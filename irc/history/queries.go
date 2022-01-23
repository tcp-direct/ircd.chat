// Copyright (c) 2020 Shivaram Lingamneni <slingamn@cs.stanford.edu>
// released under the MIT license

package history

import (
	"time"
)

// Selector represents a parameter to a CHATHISTORY command
type Selector struct {
	Msgid string
	Time  time.Time
}
