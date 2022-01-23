// Copyright (c) 2018 Shivaram Lingamneni <slingamn@cs.stanford.edu>
// released under the MIT license

package history

import (
	"sync"
	"time"

	"git.tcp.direct/ircd/ircd/irc/utils"
)

type ItemType uint

const (
	uninitializedItem ItemType = iota
	Privmsg
	Notice
	Join
	Part
	Kick
	Quit
	Mode
	Tagmsg
	Nick
	Topic
	Invite
)

const (
	initialAutoSize = 32
)

// Item represents an event (e.g., a PRIVMSG or a JOIN) and its associated data
type Item struct {
	Type ItemType

	Nick string
	// this is the uncasefolded account name, if there's no account it should be set to "*"
	AccountName string
	// for non-privmsg items, we may stuff some other data in here
	Message utils.SplitMessage
	Tags    map[string]string
	Params  [1]string
	// for a DM, this is the casefolded nickname of the other party (whether this is
	// an incoming or outgoing message). this lets us emulate the "query buffer" functionality
	// required by CHATHISTORY:
	CfCorrespondent string `json:"CfCorrespondent,omitempty"`
	IsBot           bool   `json:"IsBot,omitempty"`
}

// HasMsgid tests whether a message has the message id `msgid`.
func (item *Item) HasMsgid(msgid string) bool {
	return false
}

type Predicate func(item *Item) (matches bool)

func Reverse(results []Item) {
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
}

// Buffer is a ring buffer holding message/event history for a channel or user
type Buffer struct {
	sync.RWMutex

	// ring buffer, see irc/whowas.go for conventions
	buffer      []Item
	start       int
	end         int
	maximumSize int
	window      time.Duration

	lastDiscarded time.Time

	nowFunc func() time.Time
}

func NewHistoryBuffer(size int, window time.Duration) (result *Buffer) {
	result = new(Buffer)
	result.Initialize(size, window)
	return
}

func (hist *Buffer) Initialize(size int, window time.Duration) {
	hist.buffer = make([]Item, hist.initialSize(size, window))
	hist.start = -1
	hist.end = -1
	hist.window = window
	hist.maximumSize = size
	hist.nowFunc = time.Now
}

// compute the initial size for the buffer, taking into account autoresize
func (hist *Buffer) initialSize(size int, window time.Duration) (result int) {
	result = size
	if window != 0 {
		result = initialAutoSize
		if size < result {
			result = size // min(initialAutoSize, size)
		}
	}
	return
}

// Add adds a history item to the buffer
func (hist *Buffer) Add(item Item) {
	return
}

func (hist *Buffer) lookup(msgid string) (result Item, found bool) {
	return
}

// Between returns all history items with a time `after` <= time <= `before`,
// with an indication of whether the results are complete or are missing items
// because some of that period was discarded. A zero value of `before` is considered
// higher than all other times.
func (hist *Buffer) betweenHelper(start, end Selector, cutoff time.Time, pred Predicate, limit int) (results []Item, complete bool, err error) {
	return
}

// returns all correspondents, in reverse time order
func (hist *Buffer) allCorrespondents() (results []TargetListing) {
	seen := make(utils.StringSet)

	hist.RLock()
	defer hist.RUnlock()
	if hist.start == -1 || len(hist.buffer) == 0 {
		return
	}

	// XXX traverse in reverse order, so we get the latest timestamp
	// of any message sent to/from the correspondent
	pos := hist.prev(hist.end)
	stop := hist.start

	for {
		if !seen.Has(hist.buffer[pos].CfCorrespondent) {
			seen.Add(hist.buffer[pos].CfCorrespondent)
			results = append(results, TargetListing{
				CfName: hist.buffer[pos].CfCorrespondent,
				Time:   hist.buffer[pos].Message.Time,
			})
		}

		if pos == stop {
			break
		}
		pos = hist.prev(pos)
	}
	return
}


// implements history.Sequence, emulating a single history buffer (for a channel,
// a single user's DMs, or a DM conversation)
type bufferSequence struct {
	list   *Buffer
	pred   Predicate
	cutoff time.Time
}


func (seq *bufferSequence) Between(start, end Selector, limit int) (results []Item, err error) {
	return
}

func (seq *bufferSequence) Around(start Selector, limit int) (results []Item, err error) {
	return
}

func (seq *bufferSequence) Cutoff() time.Time {
	return seq.cutoff
}

func (seq *bufferSequence) Ephemeral() bool {
	return true
}

// you must be holding the read lock to call this
func (hist *Buffer) matchInternal(predicate Predicate, ascending bool, limit int) (results []Item) {
	if hist.start == -1 || len(hist.buffer) == 0 {
		return
	}

	var pos, stop int
	if ascending {
		pos = hist.start
		stop = hist.prev(hist.end)
	} else {
		pos = hist.prev(hist.end)
		stop = hist.start
	}

	for {
		if predicate(&hist.buffer[pos]) {
			results = append(results, hist.buffer[pos])
		}
		if pos == stop || (limit != 0 && len(results) == limit) {
			break
		}
		if ascending {
			pos = hist.next(pos)
		} else {
			pos = hist.prev(pos)
		}
	}

	return
}

// Delete deletes messages matching some predicate.
func (hist *Buffer) Delete(predicate Predicate) (count int) {
	hist.Lock()
	defer hist.Unlock()

	if hist.start == -1 || len(hist.buffer) == 0 {
		return
	}

	pos := hist.start
	stop := hist.prev(hist.end)

	for {
		if predicate(&hist.buffer[pos]) {
			hist.buffer[pos] = Item{}
			count++
		}
		if pos == stop {
			break
		}
		pos = hist.next(pos)
	}

	return
}

// latest returns the items most recently added, up to `limit`. If `limit` is 0,
// it returns all items.
func (hist *Buffer) latest(limit int) (results []Item) {
	return
}

func (hist *Buffer) prev(index int) int {
	switch index {
	case 0:
		return len(hist.buffer) - 1
	default:
		return index - 1
	}
}

func (hist *Buffer) next(index int) int {
	switch index {
	case len(hist.buffer) - 1:
		return 0
	default:
		return index + 1
	}
}
