package analysis

import (
	"regexp"
	"strings"
	"time"

	"github.com/rburns629/urfluency/gates"
	"github.com/rburns629/urfluency/gitdata"
)

// MinCodeChange is the number of added code lines a commit needs before it is
// expected to come with tests; small fixes and renames are not.
const MinCodeChange = 10

// TestHabits counts the user's code-changing commits and those that also
// changed tests, split by whether the commit was marked AI-assisted.
type TestHabits struct {
	AssistedCommits, AssistedWithTests int
	OtherCommits, OtherWithTests       int
}

func testHabits(added map[string]map[string]int, commits map[string]gitdata.Commit, tools map[string][]string, since time.Time) TestHabits {
	var h TestHabits
	for hash, perPath := range added {
		c, ok := commits[hash]
		if !ok || c.Time.Before(since) {
			continue
		}
		code, tests := 0, false
		for p, n := range perPath {
			if inSkippedDir(p) || IsProtected(p) || ForPath(p) == nil || n == 0 {
				continue
			}
			if IsTestPath(p) {
				tests = true
			} else {
				code += n
			}
		}
		if code < MinCodeChange {
			continue
		}
		switch {
		case len(tools[hash]) > 0:
			h.AssistedCommits++
			if tests {
				h.AssistedWithTests++
			}
		default:
			h.OtherCommits++
			if tests {
				h.OtherWithTests++
			}
		}
	}
	return h
}

// ciFile matches CI configuration; agentInstructions matches files that give
// coding agents written standards.
var (
	ciFile            = regexp.MustCompile(`^(\.github/workflows/[^/]+\.ya?ml|\.gitlab-ci\.yml|\.circleci/config\.yml|Jenkinsfile|azure-pipelines\.yml|bitbucket-pipelines\.yml|\.pre-commit-config\.yaml)$`)
	agentInstructions = regexp.MustCompile(`(^|/)(AGENTS\.md|CLAUDE\.md|GEMINI\.md|\.cursorrules|\.windsurfrules|copilot-instructions\.md)$`)
	// instructionTopics are what agent instructions cover, by keyword.
	instructionTopics = []struct {
		name string
		re   *regexp.Regexp
	}{
		{"verification", regexp.MustCompile(`(?i)\b(tests?|verify|verification|quality gates?|lint|type.?check)\b`)},
		{"secrets", regexp.MustCompile(`(?i)(\.env\b|\bsecrets?\b|\bcredentials?\b|\bprotected\b)`)},
		{"conventions", regexp.MustCompile(`(?i)\b(conventions?|existing (code|patterns?)|architecture|style guide)\b`)},
	}
)

// InstructionTopics lists what agent instructions are checked for, in order.
func InstructionTopics() []string {
	names := make([]string, 0, len(instructionTopics))
	for _, t := range instructionTopics {
		names = append(names, t.name)
	}
	return names
}

// RepoPractices summarizes one repository's practices and its AI-assisted and
// other code, so reports can show gate coverage and compare repositories with
// and without agent instructions.
type RepoPractices struct {
	Label     string   // the repository's path under the scanned folder; "" for one repository
	Languages []string // analyzed languages with owned lines
	// CIOwned is true when the user owns lines of the CI configuration; CIGates
	// are the gates it runs, including those delegated to the Makefile or
	// package.json.
	CIOwned bool
	CIGates []string
	// InstructionsOwned is true when the user owns lines of an agent instruction
	// file; InstructionTopics are what those files cover.
	InstructionsOwned bool
	InstructionTopics []string
	AssistedLines     int
	AssistedFindings  int
	OtherLines        int
	OtherFindings     int
	Tests             TestHabits
}

// Repo returns the repository's display name.
func (r RepoPractices) Repo() string {
	if r.Label == "" {
		return "this repository"
	}
	return r.Label
}

// practicesOf builds a repository's summary. buildScripts are the Makefile and
// package.json contents, which CI commands such as "make check" delegate to.
func practicesOf(files []ownedFile, buildScripts []string, languages []LanguageResult, tests TestHabits) RepoPractices {
	r := RepoPractices{Tests: tests}
	var ci, instructions strings.Builder
	for _, f := range files {
		switch {
		case ciFile.MatchString(f.file.Path):
			ci.Write(f.file.Content)
			ci.WriteString("\n")
			r.CIOwned = r.CIOwned || len(f.owned) > 0
		case agentInstructions.MatchString(f.file.Path) && len(f.owned) > 0:
			instructions.Write(f.file.Content)
			instructions.WriteString("\n")
			r.InstructionsOwned = true
		}
	}
	if r.CIOwned {
		r.CIGates = gates.Run(ci.String() + "\n" + strings.Join(buildScripts, "\n"))
	}
	for _, topic := range instructionTopics {
		if r.InstructionsOwned && topic.re.MatchString(instructions.String()) {
			r.InstructionTopics = append(r.InstructionTopics, topic.name)
		}
	}
	for _, lr := range languages {
		if !isAnalyzerLanguage(lr.Language) {
			continue // scanners such as secrets are not languages
		}
		r.Languages = append(r.Languages, lr.Language)
		r.AssistedLines += lr.AssistedLines
		r.AssistedFindings += lr.AssistedFindings
		r.OtherLines += lr.OwnedLines - lr.AssistedLines
		r.OtherFindings += len(lr.Findings) - lr.AssistedFindings
	}
	return r
}

func isAnalyzerLanguage(name string) bool {
	for _, a := range registry {
		if a.Language() == name {
			return true
		}
	}
	return false
}

// readEvenWithoutAnalyzer reports CI configuration and agent instruction files,
// which are read so practices can be measured.
func readEvenWithoutAnalyzer(p string) bool {
	return ciFile.MatchString(p) || agentInstructions.MatchString(p)
}

// buildScriptPaths are the top-level files CI commands commonly delegate to.
var buildScriptPaths = []string{"Makefile", "package.json"}
