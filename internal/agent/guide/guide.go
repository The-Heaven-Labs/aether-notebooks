// Package guide embeds the App Builder guide served on demand by the
// dashboard_guide tool. guide.md is the single source; docs links to it. The
// tool pays no prompt cost until the agent asks for it.
package guide

import _ "embed"

//go:embed guide.md
var Markdown string
