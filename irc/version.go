// Copyright (c) 2020 Shivaram Lingamneni
// Released under the MIT license

package irc

import "fmt"

const (
	// SemVer is the semantic version of ircd.
	SemVer = "0.4.6"
)

var (
	// Ver is the full version of ircd, used in responses to clients.
	Ver = fmt.Sprintf("ircd-%s", SemVer)
	// Commit is the full git hash, if available
	Commit string
)

// SetVersionString initialize version strings (these are set in package main via linker flags)
func SetVersionString(version, commit string) {
	Commit = commit
	if version != "" {
		Ver = fmt.Sprintf("ircd-%s", version)
	} else if len(Commit) == 40 {
		Ver = fmt.Sprintf("ircd-%s-%s", SemVer, Commit[:16])
	}
}
