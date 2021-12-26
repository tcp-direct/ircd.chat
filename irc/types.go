// Copyright (c) 2012-2014 Jeremy Latt
// Copyright (c) 2014-2015 Edmund Huber
// Copyright (c) 2016 Daniel Oaks <daniel@danieloaks.net>
// released under the MIT license

package irc

import (
	"time"

	"git.tcp.direct/ircd/ircd/irc/modes"
)

type empty struct{}

// ClientSet is a set of clients.
type ClientSet map[*Client]empty

// Add adds the given client to this set.
func (clients ClientSet) Add(client *Client) {
	clients[client] = empty{}
}

// Remove removes the given client from this set.
func (clients ClientSet) Remove(client *Client) {
	delete(clients, client)
}

// Has returns true if the given client is in this set.
func (clients ClientSet) Has(client *Client) bool {
	_, ok := clients[client]
	return ok
}

type memberData struct {
	modes    *modes.ModeSet
	joinTime int64
}

// MemberSet is a set of members with modes.
type MemberSet map[*Client]memberData

// Add adds the given client to this set.
func (members MemberSet) Add(member *Client) {
	members[member] = memberData{
		modes:    modes.NewModeSet(),
		joinTime: time.Now().UnixNano(),
	}
}

// Remove removes the given client from this set.
func (members MemberSet) Remove(member *Client) {
	delete(members, member)
}

// Has returns true if the given client is in this set.
func (members MemberSet) Has(member *Client) bool {
	_, ok := members[member]
	return ok
}

// ChannelSet is a set of channels.
type ChannelSet map[*Channel]empty
