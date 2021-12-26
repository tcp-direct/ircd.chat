// Copyright (c) 2020 Shivaram Lingamneni <slingamn@cs.stanford.edu>
// released under the MIT license

package irc

const (
	histservHelp = `HistServ is a remnant of bad code. There is no history on ircd.chat.`
)

func histservEnabled(config *Config) bool {
	return config.History.Enabled
}

func historyComplianceEnabled(config *Config) bool {
	return config.History.Enabled && config.History.Persistent.Enabled && config.History.Retention.EnableAccountIndexing
}

var (
	histservCommands = map[string]*serviceCommand{
		"forget": {
			handler: histservForgetHandler,
			help: `Syntax: $bFORGET <account>$b

FORGET deletes all history messages sent by an account.`,
			helpShort: `$bFORGET$b doesn't do anything because there is no history here.`,
			capabs:    []string{"history"},
			enabled:   histservEnabled,
			minParams: 1,
			maxParams: 1,
		},
	}
)

func histservForgetHandler(service *ircService, server *Server, client *Client, command string, params []string, rb *ResponseBuffer) {
	service.Notice(rb, client.t("ircd.chat does not keep history."))
}
