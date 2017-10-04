// Copyright (c) 2017 Shivaram Lingamneni <slingamn@cs.stanford.edu>
// released under the MIT license

package irc

func (server *Server) getISupport() *ISupportList {
	server.configurableStateMutex.RLock()
	defer server.configurableStateMutex.RUnlock()
	return server.isupport
}

func (server *Server) getLimits() Limits {
	server.configurableStateMutex.RLock()
	defer server.configurableStateMutex.RUnlock()
	return server.limits
}

func (server *Server) getPassword() []byte {
	server.configurableStateMutex.RLock()
	defer server.configurableStateMutex.RUnlock()
	return server.password
}

func (client *Client) getNick() string {
	client.stateMutex.RLock()
	defer client.stateMutex.RUnlock()
	return client.nick
}

func (client *Client) getNickMaskString() string {
	client.stateMutex.RLock()
	defer client.stateMutex.RUnlock()
	return client.nickMaskString
}

func (client *Client) getNickCasefolded() string {
	client.stateMutex.RLock()
	defer client.stateMutex.RUnlock()
	return client.nickCasefolded
}
