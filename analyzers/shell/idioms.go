package shell

import (
	"regexp"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

var (
	defaultsExpansion = regexp.MustCompile(`^\$\{[^}]*:[-=?]`)
	trapSignals       = map[string]bool{"EXIT": true, "ERR": true, "INT": true, "TERM": true, "HUP": true}
)

// visitIdioms finds the shell idioms that show fluency: each is a deliberate
// choice that makes a script safer or clearer.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "command":
		c.commandIdioms(n)
	case "declaration_command":
		if n.ChildCount() > 0 && n.Child(0).Kind() == "local" {
			c.idiom("shell.idiom.local-variables", n)
		}
	case "test_command":
		if n.ChildCount() > 0 && n.Child(0).Kind() == "[[" {
			c.idiom("shell.idiom.double-brackets", n)
		}
	case "expansion":
		t := c.tree.Text(n)
		if defaultsExpansion.MatchString(t) {
			c.idiom("shell.idiom.parameter-defaults", n)
		}
		if strings.HasSuffix(t, "[@]}") && n.Parent() != nil && n.Parent().Kind() == "string" {
			c.idiom("shell.idiom.arrays", n)
		}
	}
	return true
}

func (c *checker) commandIdioms(n *ts.Node) {
	args := arguments(n)
	text := func(i int) string { return c.tree.Text(args[i]) }
	switch commandName(c.tree, n) {
	case "set":
		for i := range args {
			if s := text(i); s == "pipefail" || (strings.HasPrefix(s, "-") && strings.Contains(s, "e") && strings.Contains(s, "u")) {
				c.idiom("shell.idiom.strict-mode", n)
				return
			}
		}
	case "trap":
		for i := range args {
			if trapSignals[strings.TrimPrefix(text(i), "SIG")] {
				c.idiom("shell.idiom.trap-cleanup", n)
				return
			}
		}
	case "getopts":
		c.idiom("shell.idiom.getopts", n)
	case "read":
		for i := range args {
			if s := text(i); strings.HasPrefix(s, "-") && strings.Contains(s, "r") {
				c.idiom("shell.idiom.read-raw", n)
				return
			}
		}
	case "main":
		if len(args) == 1 && text(0) == `"$@"` {
			c.idiom("shell.idiom.main-function", n)
		}
	}
}
