package analysis

import (
	"regexp"
	"sort"
	"strings"

	"github.com/rburns629/urfluency/internal/gitdata"
)

// aiMarks identify coding tools in Co-authored-by trailers. They match email
// domains or tool identities, not bare names, so a person named Claude or Cursor
// is not mistaken for a tool.
var aiMarks = []struct {
	tool string
	re   *regexp.Regexp
}{
	{"claude", regexp.MustCompile(`(?i)@anthropic\.com\b`)},
	{"codex", regexp.MustCompile(`(?i)@openai\.com\b`)},
	{"cursor", regexp.MustCompile(`(?i)@cursor\.(com|sh)\b`)},
	{"copilot", regexp.MustCompile(`(?i)\bcopilot@users\.noreply\.github\.com\b`)},
	{"aider", regexp.MustCompile(`(?i)\baider\b.*@aider\.chat\b|\(aider\)`)},
}

// AITools returns the coding tools a commit is marked with, sorted.
func AITools(c gitdata.Commit) []string {
	seen := map[string]bool{}
	if strings.Contains(strings.ToLower(c.AuthorName), "(aider)") {
		seen["aider"] = true
	}
	for _, co := range c.CoAuthors {
		for _, m := range aiMarks {
			if m.re.MatchString(co) {
				seen[m.tool] = true
			}
		}
	}
	tools := make([]string, 0, len(seen))
	for t := range seen {
		tools = append(tools, t)
	}
	sort.Strings(tools)
	return tools
}
