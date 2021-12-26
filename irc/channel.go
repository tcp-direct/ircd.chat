// Copyright (c) 2012-2014 Jeremy Latt
// Copyright (c) 2014-2015 Edmund Huber
// Copyright (c) 2016-2017 Daniel Oaks <daniel@danieloaks.net>
// released under the MIT license

package irc

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.tcp.direct/ircd/irc-go/ircutils"

	"git.tcp.direct/ircd/ircd/irc/caps"
	"git.tcp.direct/ircd/ircd/irc/history"
	"git.tcp.direct/ircd/ircd/irc/modes"
	"git.tcp.direct/ircd/ircd/irc/utils"
)

type ChannelSettings struct {
	History     HistoryStatus
	QueryCutoff HistoryCutoff
}

// Channel represents a channel that clients can join.
type Channel struct {
	flags             modes.ModeSet
	lists             map[modes.Mode]*UserMaskSet
	key               string
	forward           string
	members           MemberSet
	membersCache      []*Client // allow iteration over channel members without holding the lock
	name              string
	nameCasefolded    string
	server            *Server
	createdTime       time.Time
	registeredFounder string
	registeredTime    time.Time
	transferPendingTo string
	topic             string
	topicSetBy        string
	topicSetTime      time.Time
	userLimit         int
	accountToUMode    map[string]modes.Mode
	history           history.Buffer
	stateMutex        sync.RWMutex    // tier 1
	writerSemaphore   utils.Semaphore // tier 1.5
	joinPartMutex     sync.Mutex      // tier 3
	ensureLoaded      utils.Once      // manages loading stored registration info from the database
	dirtyBits         uint
	settings          ChannelSettings
}

// NewChannel creates a new channel from a `Server` and a `name`
// string, which must be unique on the server.
func NewChannel(s *Server, name, casefoldedName string, registered bool) *Channel {
	config := s.Config()

	channel := &Channel{
		createdTime:     time.Now().UTC(), // may be overwritten by applyRegInfo
		members:         make(MemberSet),
		name:            name,
		nameCasefolded:  casefoldedName,
		server:          s,
		writerSemaphore: utils.NewSemaphore(1),
	}

	channel.initializeLists()
	channel.history.Initialize(0, 0)

	if !registered {
		channel.resizeHistory(config)
		for _, mode := range config.Channels.defaultModes {
			channel.flags.SetMode(mode, true)
		}
		// no loading to do, so "mark" the load operation as "done":
		channel.ensureLoaded.Do(func() {})
	} // else: modes will be loaded before first join

	return channel
}

func (channel *Channel) initializeLists() {
	channel.lists = map[modes.Mode]*UserMaskSet{
		modes.BanMask:    NewUserMaskSet(),
		modes.ExceptMask: NewUserMaskSet(),
		modes.InviteMask: NewUserMaskSet(),
	}
	channel.accountToUMode = make(map[string]modes.Mode)
}

// EnsureLoaded blocks until the channel's registration info has been loaded
// from the database.
func (channel *Channel) EnsureLoaded() {
	channel.ensureLoaded.Do(func() {
		nmc := channel.NameCasefolded()
		info, err := channel.server.channelRegistry.LoadChannel(nmc)
		if err == nil {
			channel.applyRegInfo(info)
		} else {
			channel.server.logger.Error("internal", "couldn't load channel", nmc, err.Error())
		}
	})
}

func (channel *Channel) IsLoaded() bool {
	return channel.ensureLoaded.Done()
}

func (channel *Channel) resizeHistory(config *Config) {
	status, _, _ := channel.historyStatus(config)
	if status == HistoryEphemeral {
		channel.history.Resize(config.History.ChannelLength, time.Duration(config.History.AutoresizeWindow))
	} else {
		channel.history.Resize(0, 0)
	}
}

// read in channel state that was persisted in the DB
func (channel *Channel) applyRegInfo(chanReg RegisteredChannel) {
	defer channel.resizeHistory(channel.server.Config())

	channel.stateMutex.Lock()
	defer channel.stateMutex.Unlock()

	channel.registeredFounder = chanReg.Founder
	channel.registeredTime = chanReg.RegisteredAt
	channel.topic = chanReg.Topic
	channel.topicSetBy = chanReg.TopicSetBy
	channel.topicSetTime = chanReg.TopicSetTime
	channel.name = chanReg.Name
	channel.createdTime = chanReg.RegisteredAt
	channel.key = chanReg.Key
	channel.userLimit = chanReg.UserLimit
	channel.settings = chanReg.Settings
	channel.forward = chanReg.Forward

	for _, mode := range chanReg.Modes {
		channel.flags.SetMode(mode, true)
	}
	for account, mode := range chanReg.AccountToUMode {
		channel.accountToUMode[account] = mode
	}
	channel.lists[modes.BanMask].SetMasks(chanReg.Bans)
	channel.lists[modes.InviteMask].SetMasks(chanReg.Invites)
	channel.lists[modes.ExceptMask].SetMasks(chanReg.Excepts)
}

// ExportRegistration obtain a consistent snapshot of the channel state that can be persisted to the DB
func (channel *Channel) ExportRegistration(includeFlags uint) (info RegisteredChannel) {
	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()

	info.Name = channel.name
	info.NameCasefolded = channel.nameCasefolded
	info.Founder = channel.registeredFounder
	info.RegisteredAt = channel.registeredTime

	if includeFlags&IncludeTopic != 0 {
		info.Topic = channel.topic
		info.TopicSetBy = channel.topicSetBy
		info.TopicSetTime = channel.topicSetTime
	}

	if includeFlags&IncludeModes != 0 {
		info.Key = channel.key
		info.Forward = channel.forward
		info.Modes = channel.flags.AllModes()
		info.UserLimit = channel.userLimit
	}

	if includeFlags&IncludeLists != 0 {
		info.Bans = channel.lists[modes.BanMask].Masks()
		info.Invites = channel.lists[modes.InviteMask].Masks()
		info.Excepts = channel.lists[modes.ExceptMask].Masks()
		info.AccountToUMode = make(map[string]modes.Mode)
		for account, mode := range channel.accountToUMode {
			info.AccountToUMode[account] = mode
		}
	}

	if includeFlags&IncludeSettings != 0 {
		info.Settings = channel.settings
	}

	return
}

// begin: asynchronous database writeback implementation, modeled on irc/socket.go

// MarkDirty marks part (or all) of a channel's data as needing to be written back
// to the database, then starts a writer goroutine if necessary.
// This is the equivalent of Socket.Write().
func (channel *Channel) MarkDirty(dirtyBits uint) {
	channel.stateMutex.Lock()
	isRegistered := channel.registeredFounder != ""
	channel.dirtyBits = channel.dirtyBits | dirtyBits
	channel.stateMutex.Unlock()
	if !isRegistered {
		return
	}

	channel.wakeWriter()
}

// IsClean returns whether a channel can be safely removed from the server.
// To avoid the obvious TOCTOU race condition, it must be called while holding
// ChannelManager's lock (that way, no one can join and make the channel dirty again
// between this method exiting and the actual deletion).
func (channel *Channel) IsClean() bool {
	if !channel.writerSemaphore.TryAcquire() {
		// a database write (which may fail) is in progress, the channel cannot be cleaned up
		return false
	}
	defer channel.writerSemaphore.Release()

	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()
	if len(channel.members) != 0 {
		return false
	}
	// see #1507 and #704 among others; registered channels should never be removed
	return channel.registeredFounder == ""
}

func (channel *Channel) wakeWriter() {
	if channel.writerSemaphore.TryAcquire() {
		go channel.writeLoop()
	}
}

// equivalent of Socket.send()
func (channel *Channel) writeLoop() {
	for {
		// TODO(#357) check the error value of this and implement timed backoff
		channel.performWrite(0)
		channel.writerSemaphore.Release()

		channel.stateMutex.RLock()
		isDirty := channel.dirtyBits != 0
		isEmpty := len(channel.members) == 0
		channel.stateMutex.RUnlock()

		if !isDirty {
			if isEmpty {
				channel.server.channels.Cleanup(channel)
			}
			return // nothing to do
		} // else: isDirty, so we need to write again

		if !channel.writerSemaphore.TryAcquire() {
			return
		}
	}
}

// Store writes part (or all) of the channel's data back to the database,
// blocking until the write is complete. This is the equivalent of
// Socket.BlockingWrite.
func (channel *Channel) Store(dirtyBits uint) (err error) {
	defer func() {
		channel.stateMutex.Lock()
		isDirty := channel.dirtyBits != 0
		isEmpty := len(channel.members) == 0
		channel.stateMutex.Unlock()

		if isDirty {
			channel.wakeWriter()
		} else if isEmpty {
			channel.server.channels.Cleanup(channel)
		}
	}()

	channel.writerSemaphore.Acquire()
	defer channel.writerSemaphore.Release()
	return channel.performWrite(dirtyBits)
}

// do an individual write; equivalent of Socket.send()
func (channel *Channel) performWrite(additionalDirtyBits uint) (err error) {
	channel.stateMutex.Lock()
	dirtyBits := channel.dirtyBits | additionalDirtyBits
	channel.dirtyBits = 0
	isRegistered := channel.registeredFounder != ""
	channel.stateMutex.Unlock()

	if !isRegistered || dirtyBits == 0 {
		return
	}

	info := channel.ExportRegistration(dirtyBits)
	err = channel.server.channelRegistry.StoreChannel(info, dirtyBits)
	if err != nil {
		channel.stateMutex.Lock()
		channel.dirtyBits = channel.dirtyBits | dirtyBits
		channel.stateMutex.Unlock()
	}
	return
}

// SetRegistered registers the channel, returning an error if it was already registered.
func (channel *Channel) SetRegistered(founder string) error {
	channel.stateMutex.Lock()
	defer channel.stateMutex.Unlock()

	if channel.registeredFounder != "" {
		return errChannelAlreadyRegistered
	}
	channel.registeredFounder = founder
	channel.registeredTime = time.Now().UTC()
	channel.accountToUMode[founder] = modes.ChannelFounder
	return nil
}

// SetUnregistered deletes the channel's registration information.
func (channel *Channel) SetUnregistered(expectedFounder string) {
	channel.stateMutex.Lock()
	defer channel.stateMutex.Unlock()

	if channel.registeredFounder != expectedFounder {
		return
	}
	channel.registeredFounder = ""
	var zeroTime time.Time
	channel.registeredTime = zeroTime
	channel.accountToUMode = make(map[string]modes.Mode)
}

// implements `CHANSERV CLEAR #chan ACCESS` (resets bans, invites, excepts, and amodes)
func (channel *Channel) resetAccess() {
	defer channel.MarkDirty(IncludeLists)

	channel.stateMutex.Lock()
	defer channel.stateMutex.Unlock()
	channel.initializeLists()
	if channel.registeredFounder != "" {
		channel.accountToUMode[channel.registeredFounder] = modes.ChannelFounder
	}
}

// IsRegistered returns whether the channel is registered.
func (channel *Channel) IsRegistered() bool {
	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()
	return channel.registeredFounder != ""
}

type channelTransferStatus uint

const (
	channelTransferComplete channelTransferStatus = iota
	channelTransferPending
	channelTransferCancelled
	channelTransferFailed
)

// Transfer transfers ownership of a registered channel to a different account
func (channel *Channel) Transfer(client *Client, target string, hasPrivs bool) (status channelTransferStatus, err error) {
	status = channelTransferFailed
	defer func() {
		if status == channelTransferComplete && err == nil {
			channel.Store(IncludeAllAttrs)
		}
	}()

	cftarget, err := CasefoldName(target)
	if err != nil {
		err = errAccountDoesNotExist
		return
	}
	channel.stateMutex.Lock()
	defer channel.stateMutex.Unlock()
	if channel.registeredFounder == "" {
		err = errChannelNotOwnedByAccount
		return
	}
	if hasPrivs {
		channel.transferOwnership(cftarget)
		return channelTransferComplete, nil
	} else {
		if channel.registeredFounder == cftarget {
			// transferring back to yourself cancels a pending transfer
			channel.transferPendingTo = ""
			return channelTransferCancelled, nil
		} else {
			channel.transferPendingTo = cftarget
			return channelTransferPending, nil
		}
	}
}

func (channel *Channel) transferOwnership(newOwner string) {
	delete(channel.accountToUMode, channel.registeredFounder)
	channel.registeredFounder = newOwner
	channel.accountToUMode[channel.registeredFounder] = modes.ChannelFounder
	channel.transferPendingTo = ""
}

// AcceptTransfer implements `CS TRANSFER #chan ACCEPT`
func (channel *Channel) AcceptTransfer(client *Client) (err error) {
	defer func() {
		if err == nil {
			channel.Store(IncludeAllAttrs)
		}
	}()

	account := client.Account()
	if account == "" {
		return errAccountNotLoggedIn
	}
	channel.stateMutex.Lock()
	defer channel.stateMutex.Unlock()
	if account != channel.transferPendingTo {
		return errChannelTransferNotOffered
	}
	channel.transferOwnership(account)
	return nil
}

func (channel *Channel) regenerateMembersCache() {
	channel.stateMutex.RLock()
	result := make([]*Client, len(channel.members))
	i := 0
	for client := range channel.members {
		result[i] = client
		i++
	}
	channel.stateMutex.RUnlock()

	channel.stateMutex.Lock()
	channel.membersCache = result
	channel.stateMutex.Unlock()
}

// Names sends the list of users joined to the channel to the given client.
func (channel *Channel) Names(client *Client, rb *ResponseBuffer) {
	channel.stateMutex.RLock()
	clientData, isJoined := channel.members[client]
	channel.stateMutex.RUnlock()
	isOper := client.HasRoleCapabs("sajoin")
	respectAuditorium := channel.flags.HasMode(modes.Auditorium) && !isOper &&
		(!isJoined || clientData.modes.HighestChannelUserMode() == modes.Mode(0))
	isMultiPrefix := rb.session.capabilities.Has(caps.MultiPrefix)
	isUserhostInNames := rb.session.capabilities.Has(caps.UserhostInNames)

	maxNamLen := 480 - len(client.server.name) - len(client.Nick())
	var namesLines []string
	var buffer strings.Builder
	if isJoined || !channel.flags.HasMode(modes.Secret) || isOper {
		for _, target := range channel.Members() {
			var nick string
			if isUserhostInNames {
				nick = target.NickMaskString()
			} else {
				nick = target.Nick()
			}
			channel.stateMutex.RLock()
			memberData, _ := channel.members[target]
			channel.stateMutex.RUnlock()
			modeSet := memberData.modes
			if modeSet == nil {
				continue
			}
			if !isJoined && target.HasMode(modes.Invisible) && !isOper {
				continue
			}
			if respectAuditorium && modeSet.HighestChannelUserMode() == modes.Mode(0) {
				continue
			}
			prefix := modeSet.Prefixes(isMultiPrefix)
			if buffer.Len()+len(nick)+len(prefix)+1 > maxNamLen {
				namesLines = append(namesLines, buffer.String())
				buffer.Reset()
			}
			if buffer.Len() > 0 {
				buffer.WriteString(" ")
			}
			buffer.WriteString(prefix)
			buffer.WriteString(nick)
		}
		if buffer.Len() > 0 {
			namesLines = append(namesLines, buffer.String())
		}
	}

	for _, line := range namesLines {
		if buffer.Len() > 0 {
			rb.Add(nil, client.server.name, RPL_NAMREPLY, client.nick, "=", channel.name, line)
		}
	}
	rb.Add(nil, client.server.name, RPL_ENDOFNAMES, client.nick, channel.name, client.t("End of NAMES list"))
}

// does `clientMode` give you privileges to grant/remove `targetMode` to/from people,
// or to kick them?
func channelUserModeHasPrivsOver(clientMode modes.Mode, targetMode modes.Mode) bool {
	switch clientMode {
	case modes.ChannelFounder:
		return true
	case modes.ChannelAdmin, modes.ChannelOperator:
		// admins cannot kick other admins, operators *can* kick other operators
		return targetMode != modes.ChannelFounder && targetMode != modes.ChannelAdmin
	case modes.Halfop:
		// halfops cannot kick other halfops
		return targetMode == modes.Voice || targetMode == modes.Mode(0)
	default:
		// voice and unprivileged cannot kick anyone
		return false
	}
}

// ClientIsAtLeast returns whether the client has at least the given channel privilege.
func (channel *Channel) ClientIsAtLeast(client *Client, permission modes.Mode) bool {
	channel.stateMutex.RLock()
	memberData := channel.members[client]
	founder := channel.registeredFounder
	channel.stateMutex.RUnlock()

	if founder != "" && founder == client.Account() {
		return true
	}

	for _, mode := range modes.ChannelUserModes {
		if memberData.modes.HasMode(mode) {
			return true
		}
		if mode == permission {
			break
		}
	}
	return false
}

func (channel *Channel) ClientPrefixes(client *Client, isMultiPrefix bool) string {
	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()
	memberData, present := channel.members[client]
	if !present {
		return ""
	} else {
		return memberData.modes.Prefixes(isMultiPrefix)
	}
}

func (channel *Channel) ClientStatus(client *Client) (present bool, joinTimeSecs int64, cModes modes.Modes) {
	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()
	memberData, present := channel.members[client]
	return present, time.Unix(0, memberData.joinTime).Unix(), memberData.modes.AllModes()
}

// helper for persisting channel-user modes for always-on clients;
// return the channel name and all channel-user modes for a client
func (channel *Channel) alwaysOnStatus(client *Client) (chname string, status alwaysOnChannelStatus) {
	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()
	chname = channel.name
	data := channel.members[client]
	status.Modes = data.modes.String()
	status.JoinTime = data.joinTime
	return
}

// overwrite any existing channel-user modes with the stored ones
func (channel *Channel) setMemberStatus(client *Client, status alwaysOnChannelStatus) {
	newModes := modes.NewModeSet()
	for _, mode := range status.Modes {
		newModes.SetMode(modes.Mode(mode), true)
	}
	channel.stateMutex.Lock()
	defer channel.stateMutex.Unlock()
	if _, ok := channel.members[client]; !ok {
		return
	}
	memberData := channel.members[client]
	memberData.modes = newModes
	memberData.joinTime = status.JoinTime
	channel.members[client] = memberData
}

func (channel *Channel) ClientHasPrivsOver(client *Client, target *Client) bool {
	channel.stateMutex.RLock()
	founder := channel.registeredFounder
	clientModes := channel.members[client].modes
	targetModes := channel.members[target].modes
	channel.stateMutex.RUnlock()

	if founder != "" {
		if founder == client.Account() {
			return true // #950: founder can take any privileged action without actually having +q
		} else if founder == target.Account() {
			return false // conversely, only the founder can kick the founder
		}
	}

	return channelUserModeHasPrivsOver(clientModes.HighestChannelUserMode(), targetModes.HighestChannelUserMode())
}

func (channel *Channel) hasClient(client *Client) bool {
	channel.stateMutex.RLock()
	_, present := channel.members[client]
	channel.stateMutex.RUnlock()
	return present
}

// <mode> <mode params>
func (channel *Channel) modeStrings(client *Client) (result []string) {
	hasPrivs := client.HasRoleCapabs("sajoin")

	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()

	isMember := hasPrivs || channel.members.Has(client)
	showKey := isMember && (channel.key != "")
	showUserLimit := channel.userLimit > 0
	showForward := channel.forward != ""

	var mods strings.Builder
	mods.WriteRune('+')

	// flags with args
	if showKey {
		mods.WriteRune(rune(modes.Key))
	}
	if showUserLimit {
		mods.WriteRune(rune(modes.UserLimit))
	}
	if showForward {
		mods.WriteRune(rune(modes.Forward))
	}

	for _, m := range channel.flags.AllModes() {
		mods.WriteRune(rune(m))
	}

	result = []string{mods.String()}

	// args for flags with args: The order must match above to keep
	// positional arguments in place.
	if showKey {
		result = append(result, channel.key)
	}
	if showUserLimit {
		result = append(result, strconv.Itoa(channel.userLimit))
	}
	if showForward {
		result = append(result, channel.forward)
	}

	return
}

func (channel *Channel) IsEmpty() bool {
	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()
	return len(channel.members) == 0
}

// figure out where history is being stored: persistent, ephemeral, or neither
// target is only needed if we're doing persistent history
func (channel *Channel) historyStatus(config *Config) (status HistoryStatus, target string, restrictions HistoryCutoff) {
	if !config.History.Enabled {
		return HistoryDisabled, "", HistoryCutoffNone
	}

	channel.stateMutex.RLock()
	target = channel.nameCasefolded
	settings := channel.settings
	registered := channel.registeredFounder != ""
	channel.stateMutex.RUnlock()

	restrictions = settings.QueryCutoff
	if restrictions == HistoryCutoffDefault {
		restrictions = config.History.Restrictions.queryCutoff
	}

	return channelHistoryStatus(config, registered, settings.History), target, restrictions
}

func (channel *Channel) joinTimeCutoff(client *Client) (present bool, cutoff time.Time) {
	account := client.Account()

	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()
	if data, ok := channel.members[client]; ok {
		present = true
		// report a cutoff of zero, i.e., no restriction, if the user is privileged
		if !((account != "" && account == channel.registeredFounder) || data.modes.HasMode(modes.ChannelFounder) || data.modes.HasMode(modes.ChannelAdmin) || data.modes.HasMode(modes.ChannelOperator)) {
			cutoff = time.Unix(0, data.joinTime)
		}
	}
	return
}

func channelHistoryStatus(config *Config, registered bool, storedStatus HistoryStatus) (result HistoryStatus) {
	return HistoryDisabled
}

func (channel *Channel) AddHistoryItem(item history.Item, account string) (err error) {
	if !itemIsStorable(&item, channel.server.Config()) {
		return
	}

	status, _, _ := channel.historyStatus(channel.server.Config())
	if status == HistoryEphemeral {
		channel.history.Add(item)
	}
	return
}

// Join joins the given client to this channel (if they can be joined).
func (channel *Channel) Join(client *Client, key string, isSajoin bool, rb *ResponseBuffer) (joinErr error, forward string) {
	details := client.Details()
	isBot := client.HasMode(modes.Bot)

	channel.stateMutex.RLock()
	chname := channel.name
	chcfname := channel.nameCasefolded
	founder := channel.registeredFounder
	createdAt := channel.createdTime
	chkey := channel.key
	limit := channel.userLimit
	chcount := len(channel.members)
	_, alreadyJoined := channel.members[client]
	persistentMode := channel.accountToUMode[details.account]
	forward = channel.forward
	channel.stateMutex.RUnlock()

	if alreadyJoined {
		// no message needs to be sent
		return nil, ""
	}

	switch {
	case
		// 0. SAJOIN always succeeds
		isSajoin,
		// 1. the founder can always join (even if they disabled auto +q on join)
		founder == details.account && details.account != "",
		// 2. anyone who automatically receives halfop or higher can always join
		persistentMode != 0 && persistentMode != modes.Voice,
		// 3. people invited with INVITE can join
		client.CheckInvited(chcfname, createdAt):

		break

	// If the channel has limited capacity and they are over said capacity, don't join.
	case limit != 0 && chcount >= limit:
		return errLimitExceeded, forward

	// If they channel is +k (keyed), and the joinee does not have the correct key, don't join.
	case chkey != "" && !utils.SecretTokensMatch(chkey, key):
		return errWrongChannelKey, forward

	// If the channel is invite only and they joinee does not have an invite exception, don't join.
	case channel.flags.HasMode(modes.InviteOnly):
		if channel.lists[modes.InviteMask].Match(details.nickMaskCasefolded) {
			break
		}
		return errInviteOnly, forward

	// If the channel has banned the joinee and there is no ban exception, don't join.
	case channel.lists[modes.BanMask].Match(details.nickMaskCasefolded):
		if channel.lists[modes.ExceptMask].Match(details.nickMaskCasefolded) {
			// do not forward people who are banned:
			return errBanned, ""
		}

	// If the channel is set to registered users only and there is no invite exception for the joinee, don't join.
	case details.account == "" &&
		(channel.flags.HasMode(modes.RegisteredOnly) || channel.server.Defcon() <= 2) &&
		!channel.lists[modes.InviteMask].Match(details.nickMaskCasefolded):
		return errRegisteredOnly, forward

	default:
		//
	}

	if joinErr := client.addChannel(channel, rb == nil); joinErr != nil {
		return joinErr, ""
	}

	client.server.logger.Debug("channels", fmt.Sprintf("%s joined channel %s", details.nick, chname))

	givenMode := func() (givenMode modes.Mode) {
		channel.joinPartMutex.Lock()
		defer channel.joinPartMutex.Unlock()

		func() {
			channel.stateMutex.Lock()
			defer channel.stateMutex.Unlock()

			channel.members.Add(client)
			firstJoin := len(channel.members) == 1
			newChannel := firstJoin && channel.registeredFounder == ""
			if newChannel {
				givenMode = modes.ChannelOperator
			} else {
				givenMode = persistentMode
			}
			if givenMode != 0 {
				channel.members[client].modes.SetMode(givenMode, true)
			}
		}()

		channel.regenerateMembersCache()

		return
	}()

	var message utils.SplitMessage
	respectAuditorium := givenMode == modes.Mode(0) && channel.flags.HasMode(modes.Auditorium)
	message = utils.MakeMessage("")
	// no history item for fake persistent joins
	if rb != nil && !respectAuditorium {
		histItem := history.Item{
			Type:        history.Join,
			Nick:        details.nickMask,
			AccountName: details.accountName,
			Message:     message,
			IsBot:       isBot,
		}
		histItem.Params[0] = details.realname
		channel.AddHistoryItem(histItem, details.account)
	}

	if rb == nil {
		return nil, ""
	}

	var modestr string
	if givenMode != 0 {
		modestr = fmt.Sprintf("+%v", givenMode)
	}

	// cache the most common case (JOIN without extended-join)
	var cache MessageCache
	cache.Initialize(channel.server, message.Time, message.Msgid, details.nickMask, details.accountName, isBot, nil, "JOIN", chname)
	isAway, awayMessage := client.Away()
	for _, member := range channel.Members() {
		if respectAuditorium {
			channel.stateMutex.RLock()
			memberData, ok := channel.members[member]
			channel.stateMutex.RUnlock()
			if !ok || memberData.modes.HighestChannelUserMode() == modes.Mode(0) {
				continue
			}
		}
		for _, session := range member.Sessions() {
			if session == rb.session {
				continue
			} else if client == session.client {
				channel.playJoinForSession(session)
				continue
			}
			if session.capabilities.Has(caps.ExtendedJoin) {
				session.sendFromClientInternal(false, message.Time, message.Msgid, details.nickMask, details.accountName, isBot, nil, "JOIN", chname, details.accountName, details.realname)
			} else {
				cache.Send(session)
			}
			if givenMode != 0 {
				session.Send(nil, client.server.name, "MODE", chname, modestr, details.nick)
			}
			if isAway && session.capabilities.Has(caps.AwayNotify) {
				session.sendFromClientInternal(false, time.Time{}, "", details.nickMask, details.accountName, isBot, nil, "AWAY", awayMessage)
			}
		}
	}

	if rb.session.capabilities.Has(caps.ExtendedJoin) {
		rb.AddFromClient(message.Time, message.Msgid, details.nickMask, details.accountName, isBot, nil, "JOIN", chname, details.accountName, details.realname)
	} else {
		rb.AddFromClient(message.Time, message.Msgid, details.nickMask, details.accountName, isBot, nil, "JOIN", chname)
	}

	if rb.session.client == client {
		// don't send topic and names for a SAJOIN of a different client
		channel.SendTopic(client, rb, false)
		channel.Names(client, rb)
	} else {
		// ensure that SAJOIN sends a MODE line to the originating client, if applicable
		if givenMode != 0 {
			rb.Add(nil, client.server.name, "MODE", chname, modestr, details.nick)
		}
	}

	// TODO #259 can be implemented as Flush(false) (i.e., nonblocking) while holding joinPartMutex
	rb.Flush(true)

	return nil, ""
}

// plays channel join messages (the JOIN line, topic, and names) to a session.
// this is used when attaching a new session to an existing client that already has
// channels, and also when one session of a client initiates a JOIN and the other
// sessions need to receive the state change
func (channel *Channel) playJoinForSession(session *Session) {
	client := session.client
	sessionRb := NewResponseBuffer(session)
	details := client.Details()
	if session.capabilities.Has(caps.ExtendedJoin) {
		sessionRb.Add(nil, details.nickMask, "JOIN", channel.Name(), details.accountName, details.realname)
	} else {
		sessionRb.Add(nil, details.nickMask, "JOIN", channel.Name())
	}
	channel.SendTopic(client, sessionRb, false)
	channel.Names(client, sessionRb)
	sessionRb.Send(false)
}

// Part parts the given client from this channel, with the given message.
func (channel *Channel) Part(client *Client, message string, rb *ResponseBuffer) {
	channel.stateMutex.RLock()
	chname := channel.name
	clientData, ok := channel.members[client]
	channel.stateMutex.RUnlock()

	if !ok {
		rb.Add(nil, client.server.name, ERR_NOTONCHANNEL, client.Nick(), chname, client.t("You're not on that channel"))
		return
	}

	channel.Quit(client)

	splitMessage := utils.MakeMessage(message)

	details := client.Details()
	isBot := client.HasMode(modes.Bot)
	params := make([]string, 1, 2)
	params[0] = chname
	if message != "" {
		params = append(params, message)
	}
	respectAuditorium := channel.flags.HasMode(modes.Auditorium) &&
		clientData.modes.HighestChannelUserMode() == modes.Mode(0)
	var cache MessageCache
	cache.Initialize(channel.server, splitMessage.Time, splitMessage.Msgid, details.nickMask, details.accountName, isBot, nil, "PART", params...)
	for _, member := range channel.Members() {
		if respectAuditorium {
			channel.stateMutex.RLock()
			memberData, ok := channel.members[member]
			channel.stateMutex.RUnlock()
			if !ok || memberData.modes.HighestChannelUserMode() == modes.Mode(0) {
				continue
			}
		}
		for _, session := range member.Sessions() {
			cache.Send(session)
		}
	}
	rb.AddFromClient(splitMessage.Time, splitMessage.Msgid, details.nickMask, details.accountName, isBot, nil, "PART", params...)
	for _, session := range client.Sessions() {
		if session != rb.session {
			session.sendFromClientInternal(false, splitMessage.Time, splitMessage.Msgid, details.nickMask, details.accountName, isBot, nil, "PART", params...)
		}
	}

	if !respectAuditorium {
		channel.AddHistoryItem(history.Item{
			Type:        history.Part,
			Nick:        details.nickMask,
			AccountName: details.accountName,
			Message:     splitMessage,
			IsBot:       isBot,
		}, details.account)
	}

	client.server.logger.Debug("channels", fmt.Sprintf("%s left channel %s", details.nick, chname))
}

// SendTopic sends the channel topic to the given client.
// `sendNoTopic` controls whether RPL_NOTOPIC is sent when the topic is unset
func (channel *Channel) SendTopic(client *Client, rb *ResponseBuffer, sendNoTopic bool) {
	channel.stateMutex.RLock()
	name := channel.name
	topic := channel.topic
	topicSetBy := channel.topicSetBy
	topicSetTime := channel.topicSetTime
	_, hasClient := channel.members[client]
	channel.stateMutex.RUnlock()

	if !hasClient {
		rb.Add(nil, client.server.name, ERR_NOTONCHANNEL, client.Nick(), channel.name, client.t("You're not on that channel"))
		return
	}

	if topic == "" {
		if sendNoTopic {
			rb.Add(nil, client.server.name, RPL_NOTOPIC, client.nick, name, client.t("No topic is set"))
		}
		return
	}

	rb.Add(nil, client.server.name, RPL_TOPIC, client.nick, name, topic)
	rb.Add(nil, client.server.name, RPL_TOPICTIME, client.nick, name, topicSetBy, strconv.FormatInt(topicSetTime.Unix(), 10))
}

// SetTopic sets the topic of this channel, if the client is allowed to do so.
func (channel *Channel) SetTopic(client *Client, topic string, rb *ResponseBuffer) {
	if !channel.hasClient(client) {
		rb.Add(nil, client.server.name, ERR_NOTONCHANNEL, client.Nick(), channel.Name(), client.t("You're not on that channel"))
		return
	}

	if channel.flags.HasMode(modes.OpOnlyTopic) && !(channel.ClientIsAtLeast(client, modes.Halfop) || client.HasRoleCapabs("samode")) {
		rb.Add(nil, client.server.name, ERR_CHANOPRIVSNEEDED, client.Nick(), channel.Name(), client.t("You're not a channel operator"))
		return
	}

	topic = ircutils.TruncateUTF8Safe(topic, client.server.Config().Limits.TopicLen)

	channel.stateMutex.Lock()
	chname := channel.name
	channel.topic = topic
	channel.topicSetBy = client.nickMaskString
	channel.topicSetTime = time.Now().UTC()
	channel.stateMutex.Unlock()

	details := client.Details()
	isBot := client.HasMode(modes.Bot)
	message := utils.MakeMessage(topic)
	rb.AddFromClient(message.Time, message.Msgid, details.nickMask, details.accountName, isBot, nil, "TOPIC", chname, topic)
	for _, member := range channel.Members() {
		for _, session := range member.Sessions() {
			if session != rb.session {
				session.sendFromClientInternal(false, message.Time, message.Msgid, details.nickMask, details.accountName, isBot, nil, "TOPIC", chname, topic)
			}
		}
	}

	channel.AddHistoryItem(history.Item{
		Type:        history.Topic,
		Nick:        details.nickMask,
		AccountName: details.accountName,
		Message:     message,
		IsBot:       isBot,
	}, details.account)

	channel.MarkDirty(IncludeTopic)
}

// CanSpeak returns true if the client can speak on this channel, otherwise it returns false along with the channel mode preventing the client from speaking.
func (channel *Channel) CanSpeak(client *Client) (bool, modes.Mode) {
	channel.stateMutex.RLock()
	memberData, hasClient := channel.members[client]
	channel.stateMutex.RUnlock()
	clientModes := memberData.modes

	if !hasClient && channel.flags.HasMode(modes.NoOutside) {
		// TODO: enforce regular +b bans on -n channels?
		return false, modes.NoOutside
	}
	if channel.isMuted(client) && clientModes.HighestChannelUserMode() == modes.Mode(0) {
		return false, modes.BanMask
	}
	if channel.flags.HasMode(modes.Moderated) && clientModes.HighestChannelUserMode() == modes.Mode(0) {
		return false, modes.Moderated
	}
	if channel.flags.HasMode(modes.RegisteredOnlySpeak) && client.Account() == "" &&
		clientModes.HighestChannelUserMode() == modes.Mode(0) {
		return false, modes.RegisteredOnlySpeak
	}
	return true, modes.Mode('?')
}

func (channel *Channel) isMuted(client *Client) bool {
	muteRe := channel.lists[modes.BanMask].MuteRegexp()
	if muteRe == nil {
		return false
	}
	nuh := client.NickMaskCasefolded()
	return muteRe.MatchString(nuh) && !channel.lists[modes.ExceptMask].MatchMute(nuh)
}

func (channel *Channel) relayNickMuted(relayNick string) bool {
	relayNUH := fmt.Sprintf("%s!*@*", relayNick)
	return channel.lists[modes.BanMask].MatchMute(relayNUH) &&
		!channel.lists[modes.ExceptMask].MatchMute(relayNUH)
}

func msgCommandToHistType(command string) (history.ItemType, error) {
	switch command {
	case "PRIVMSG":
		return history.Privmsg, nil
	case "NOTICE":
		return history.Notice, nil
	case "TAGMSG":
		return history.Tagmsg, nil
	default:
		return history.ItemType(0), errInvalidParams
	}
}

func (channel *Channel) SendSplitMessage(command string, minPrefixMode modes.Mode, clientOnlyTags map[string]string, client *Client, message utils.SplitMessage, rb *ResponseBuffer) {
	histType, err := msgCommandToHistType(command)
	if err != nil {
		return
	}

	if canSpeak, mode := channel.CanSpeak(client); !canSpeak {
		if histType != history.Notice {
			rb.Add(nil, client.server.name, ERR_CANNOTSENDTOCHAN, client.Nick(), channel.Name(), fmt.Sprintf(client.t("Cannot send to channel (+%s)"), mode))
		}
		return
	}

	isCTCP := message.IsRestrictedCTCPMessage()
	if isCTCP && channel.flags.HasMode(modes.NoCTCP) {
		if histType != history.Notice {
			rb.Add(nil, client.server.name, ERR_CANNOTSENDTOCHAN, client.Nick(), channel.Name(), fmt.Sprintf(client.t("Cannot send to channel (+%s)"), "C"))
		}
		return
	}

	details := client.Details()
	isBot := client.HasMode(modes.Bot)
	chname := channel.Name()

	if !client.server.Config().Server.Compatibility.allowTruncation {
		if !validateSplitMessageLen(histType, details.nickMask, chname, message) {
			rb.Add(nil, client.server.name, ERR_INPUTTOOLONG, details.nick, client.t("Line too long to be relayed without truncation"))
			return
		}
	}

	// STATUSMSG targets are prefixed with the supplied min-prefix, e.g., @#channel
	if minPrefixMode != modes.Mode(0) {
		chname = fmt.Sprintf("%s%s", modes.ChannelModePrefixes[minPrefixMode], chname)
	}

	if channel.flags.HasMode(modes.OpModerated) {
		channel.stateMutex.RLock()
		cuData := channel.members[client]
		channel.stateMutex.RUnlock()
		if cuData.modes.HighestChannelUserMode() == modes.Mode(0) {
			// max(statusmsg_minmode, halfop)
			if minPrefixMode == modes.Mode(0) || minPrefixMode == modes.Voice {
				minPrefixMode = modes.Halfop
			}
		}
	}

	// send echo-message
	rb.addEchoMessage(clientOnlyTags, details.nickMask, details.accountName, command, chname, message)

	var cache MessageCache
	cache.InitializeSplitMessage(channel.server, details.nickMask, details.accountName, isBot, clientOnlyTags, command, chname, message)
	for _, member := range channel.Members() {
		if minPrefixMode != modes.Mode(0) && !channel.ClientIsAtLeast(member, minPrefixMode) {
			// STATUSMSG or OpModerated
			continue
		}

		for _, session := range member.Sessions() {
			if session == rb.session {
				continue // we already sent echo-message, if applicable
			}

			if isCTCP && session.isTor {
				continue // #753
			}

			cache.Send(session)
		}
	}

	// #959: don't save STATUSMSG (or OpModerated)
	if minPrefixMode == modes.Mode(0) {
		channel.AddHistoryItem(history.Item{
			Type:        histType,
			Message:     message,
			Nick:        details.nickMask,
			AccountName: details.accountName,
			Tags:        clientOnlyTags,
			IsBot:       isBot,
		}, details.account)
	}
}

func (channel *Channel) applyModeToMember(client *Client, change modes.ModeChange, rb *ResponseBuffer) (applied bool, result modes.ModeChange) {
	target := channel.server.clients.Get(change.Arg)
	if target == nil {
		rb.Add(nil, client.server.name, ERR_NOSUCHNICK, client.Nick(), utils.SafeErrorParam(change.Arg), client.t("No such nick"))
		return
	}
	change.Arg = target.Nick()

	channel.stateMutex.Lock()
	memberData, exists := channel.members[target]
	if exists {
		if memberData.modes.SetMode(change.Mode, change.Op == modes.Add) {
			applied = true
			result = change
		}
	}
	channel.stateMutex.Unlock()

	if !exists {
		rb.Add(nil, client.server.name, ERR_USERNOTINCHANNEL, client.Nick(), channel.Name(), client.t("They aren't on that channel"))
	}
	if applied {
		target.markDirty(IncludeChannels)
	}
	return
}

// ShowMaskList shows the given list to the client.
func (channel *Channel) ShowMaskList(client *Client, mode modes.Mode, rb *ResponseBuffer) {
	// choose appropriate modes
	var rpllist, rplendoflist string
	if mode == modes.BanMask {
		rpllist = RPL_BANLIST
		rplendoflist = RPL_ENDOFBANLIST
	} else if mode == modes.ExceptMask {
		rpllist = RPL_EXCEPTLIST
		rplendoflist = RPL_ENDOFEXCEPTLIST
	} else if mode == modes.InviteMask {
		rpllist = RPL_INVITELIST
		rplendoflist = RPL_ENDOFINVITELIST
	}

	nick := client.Nick()
	chname := channel.Name()
	for mask, info := range channel.lists[mode].Masks() {
		rb.Add(nil, client.server.name, rpllist, nick, chname, mask, info.CreatorNickmask, strconv.FormatInt(info.TimeCreated.Unix(), 10))
	}

	rb.Add(nil, client.server.name, rplendoflist, nick, chname, client.t("End of list"))
}

// Quit removes the given client from the channel
func (channel *Channel) Quit(client *Client) {
	channelEmpty := func() bool {
		channel.joinPartMutex.Lock()
		defer channel.joinPartMutex.Unlock()

		channel.stateMutex.Lock()
		channel.members.Remove(client)
		channelEmpty := len(channel.members) == 0
		channel.stateMutex.Unlock()
		channel.regenerateMembersCache()
		return channelEmpty
	}()

	if channelEmpty {
		client.server.channels.Cleanup(channel)
	}
	client.removeChannel(channel)
}

func (channel *Channel) Kick(client *Client, target *Client, comment string, rb *ResponseBuffer, hasPrivs bool) {
	if !hasPrivs {
		if !channel.ClientHasPrivsOver(client, target) {
			rb.Add(nil, client.server.name, ERR_CHANOPRIVSNEEDED, client.Nick(), channel.Name(), client.t("You don't have enough channel privileges"))
			return
		}
	}
	if !channel.hasClient(target) {
		rb.Add(nil, client.server.name, ERR_USERNOTINCHANNEL, client.Nick(), channel.Name(), client.t("They aren't on that channel"))
		return
	}

	comment = ircutils.TruncateUTF8Safe(comment, channel.server.Config().Limits.KickLen)

	message := utils.MakeMessage(comment)
	details := client.Details()
	isBot := client.HasMode(modes.Bot)

	targetNick := target.Nick()
	chname := channel.Name()
	for _, member := range channel.Members() {
		for _, session := range member.Sessions() {
			if session != rb.session {
				session.sendFromClientInternal(false, message.Time, message.Msgid, details.nickMask, details.accountName, isBot, nil, "KICK", chname, targetNick, comment)
			}
		}
	}
	rb.AddFromClient(message.Time, message.Msgid, details.nickMask, details.accountName, isBot, nil, "KICK", chname, targetNick, comment)

	histItem := history.Item{
		Type:        history.Kick,
		Nick:        details.nickMask,
		AccountName: details.accountName,
		Message:     message,
		IsBot:       isBot,
	}
	histItem.Params[0] = targetNick
	channel.AddHistoryItem(histItem, details.account)

	channel.Quit(target)
}

// Purge handle a purge: kick everyone off the channel, clean up all the pointers between
// *Channel and *Client
func (channel *Channel) Purge(source string) {
	if source == "" {
		source = channel.server.name
	}

	channel.stateMutex.Lock()
	chname := channel.name
	members := channel.membersCache
	channel.membersCache = nil
	channel.members = make(MemberSet)
	// TODO try to prevent Purge racing against (pending) Join?
	channel.stateMutex.Unlock()

	now := time.Now().UTC()
	for _, member := range members {
		tnick := member.Nick()
		msgid := utils.GenerateSecretToken()
		for _, session := range member.Sessions() {
			session.sendFromClientInternal(false, now, msgid, source, "*", false, nil, "KICK", chname, tnick, member.t("This channel has been purged by the server administrators and cannot be used"))
		}
		member.removeChannel(channel)
	}
}

// Invite invites the given client to the channel, if the inviter can do so.
func (channel *Channel) Invite(invitee *Client, inviter *Client, rb *ResponseBuffer) {
	channel.stateMutex.RLock()
	chname := channel.name
	chcfname := channel.nameCasefolded
	createdAt := channel.createdTime
	_, inviterPresent := channel.members[inviter]
	_, inviteePresent := channel.members[invitee]
	channel.stateMutex.RUnlock()

	if !inviterPresent {
		rb.Add(nil, inviter.server.name, ERR_NOTONCHANNEL, inviter.Nick(), chname, inviter.t("You're not on that channel"))
		return
	}

	inviteOnly := channel.flags.HasMode(modes.InviteOnly)
	if inviteOnly && !channel.ClientIsAtLeast(inviter, modes.ChannelOperator) {
		rb.Add(nil, inviter.server.name, ERR_CHANOPRIVSNEEDED, inviter.Nick(), chname, inviter.t("You're not a channel operator"))
		return
	}

	if inviteePresent {
		rb.Add(nil, inviter.server.name, ERR_USERONCHANNEL, inviter.Nick(), invitee.Nick(), chname, inviter.t("User is already on that channel"))
		return
	}

	if inviteOnly {
		invitee.Invite(chcfname, createdAt)
	}

	details := inviter.Details()
	isBot := inviter.HasMode(modes.Bot)
	tDetails := invitee.Details()
	tnick := invitee.Nick()
	message := utils.MakeMessage(chname)
	item := history.Item{
		Type:    history.Invite,
		Message: message,
	}

	for _, member := range channel.Members() {
		if member == inviter || member == invitee || !channel.ClientIsAtLeast(member, modes.Halfop) {
			continue
		}
		for _, session := range member.Sessions() {
			if session.capabilities.Has(caps.InviteNotify) {
				session.sendFromClientInternal(false, message.Time, message.Msgid, details.nickMask, details.accountName, isBot, nil, "INVITE", tnick, chname)
			}
		}
	}

	rb.Add(nil, inviter.server.name, RPL_INVITING, details.nick, tnick, chname)
	for _, iSession := range invitee.Sessions() {
		iSession.sendFromClientInternal(false, message.Time, message.Msgid, details.nickMask, details.accountName, isBot, nil, "INVITE", tnick, chname)
	}
	if away, awayMessage := invitee.Away(); away {
		rb.Add(nil, inviter.server.name, RPL_AWAY, details.nick, tnick, awayMessage)
	}
	inviter.addHistoryItem(invitee, item, &details, &tDetails, channel.server.Config())
}

// Uninvite rescinds a channel invitation, if the inviter can do so.
func (channel *Channel) Uninvite(invitee *Client, inviter *Client, rb *ResponseBuffer) {
	if !channel.flags.HasMode(modes.InviteOnly) {
		rb.Add(nil, channel.server.name, "FAIL", "UNINVITE", "NOT_INVITE_ONLY", channel.Name(), inviter.t("Channel is not invite-only"))
		return
	}

	if !channel.ClientIsAtLeast(inviter, modes.ChannelOperator) {
		rb.Add(nil, channel.server.name, "FAIL", "UNINVITE", "PRIVS_NEEDED", channel.Name(), inviter.t("You're not a channel operator"))
		return
	}

	invitee.Uninvite(channel.NameCasefolded())
	rb.Add(nil, channel.server.name, "UNINVITE", invitee.Nick(), channel.Name())
}

// returns who the client can "see" in the channel, respecting the auditorium mode
func (channel *Channel) auditoriumFriends(client *Client) (friends []*Client) {
	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()

	clientData, found := channel.members[client]
	if !found {
		return // non-members have no friends
	}
	if !channel.flags.HasMode(modes.Auditorium) {
		return channel.membersCache // default behavior for members
	}
	if clientData.modes.HighestChannelUserMode() != modes.Mode(0) {
		return channel.membersCache // +v and up can see everyone in the auditorium
	}
	// without +v, your friends are those with +v and up
	for member, memberData := range channel.members {
		if memberData.modes.HighestChannelUserMode() != modes.Mode(0) {
			friends = append(friends, member)
		}
	}
	return
}

// data for RPL_LIST
func (channel *Channel) listData() (memberCount int, name, topic string) {
	channel.stateMutex.RLock()
	defer channel.stateMutex.RUnlock()
	return len(channel.members), channel.name, channel.topic
}
