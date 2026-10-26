package sessions

import (
	"regexp"
	"strings"
)

// Task types. Prompts are graded by what they ask for: a bug with an evident
// symptom needs less specification than a feature.
const (
	Bug      = "bug"
	Feature  = "feature"
	Refactor = "refactor"
	Test     = "test"
	Other    = "other" // questions and requests that are not code changes
)

// Prompt holds the signals of one user prompt, and what the agent did before the
// next prompt; its text is not kept.
type Prompt struct {
	Type         string // "" when the prompt is not a task, such as "yes" or a slash command
	Correction   bool
	Context      bool // names files, symbols, errors, or pastes code or logs
	Criteria     bool // states expected behavior or when the task is done
	Verification bool // asks for tests or checks

	Explores           int // file reads and searches
	ExploresBeforeEdit int
	Edits              int
	Checks             int // tests, linters, builds, and type checks run
}

var (
	correctionRe = regexp.MustCompile(`^(no[,.!]|no$|nope\b|wrong\b|still\b|it still\b|that'?s not\b|that is not\b|this is not\b|not what\b|(it )?(doesn'?t|does not|didn'?t|did not) work|you broke\b|(that|this) broke\b|revert\b|undo\b|why did you\b)`)
	politeRe     = regexp.MustCompile(`^(can|could|would|will) you\b|^please\b`)
	bugRe        = regexp.MustCompile(`\b(fix|bug|broken|crash(es|ing)?|error|fails?|failing|regression|exception|traceback|panic)\b`)
	testRe       = regexp.MustCompile(`^(please )?(add|write)( a| the| more)?( unit| integration| e2e)? tests?\b`)
	refactorRe   = regexp.MustCompile(`\b(refactor|rename|clean ?up|simplify|extract|restructure|reorganize|deduplicate|move)\b`)
	featureRe    = regexp.MustCompile(`\b(add|implement|create|build|support|introduce|make|update|change|integrate|generate|write)\b`)
	contextRe    = regexp.MustCompile("`|\\b[\\w./-]+\\.(go|py|ts|tsx|js|jsx|java|kt|rb|rs|cs|php|swift|sql|md|json|ya?ml|toml|html|css|sh)\\b|\\w/\\w|\\w\\(\\)|\\bline \\d+|:\\d+\\b|\\b(stack trace|traceback|error:|exception:)")
	criteriaRe   = regexp.MustCompile(`\b(should|must|expect(ed|s)?|so that|done when|acceptance|criteria|returns?|until|ensure|make sure|without (breaking|changing)|only (if|when)|instead of|requirements?)\b`)
	verifyRe     = regexp.MustCompile(`\b(tests?|testing|verify|verified|check|lint|build|compile|type-?check|ci|benchmark|run (it|the))\b`)
)

// Classify reduces a prompt to signals. Slash commands, tool output, and replies
// shorter than four words are not tasks.
func Classify(text string) Prompt {
	t := strings.ToLower(stripTagBlocks(strings.TrimSpace(text)))
	if t == "" || strings.HasPrefix(t, "<") || strings.HasPrefix(t, "/") || strings.HasPrefix(t, "[") {
		return Prompt{}
	}
	if correctionRe.MatchString(t) {
		return Prompt{Correction: true}
	}
	if len(strings.Fields(t)) < 4 {
		return Prompt{}
	}
	p := Prompt{
		Context:      contextRe.MatchString(t) || strings.Count(t, "\n") >= 3,
		Criteria:     criteriaRe.MatchString(t),
		Verification: verifyRe.MatchString(t),
	}
	switch {
	case strings.HasSuffix(t, "?") && !politeRe.MatchString(t):
		p.Type = Other
	case bugRe.MatchString(t):
		p.Type = Bug
	case testRe.MatchString(t):
		p.Type = Test
	case refactorRe.MatchString(t):
		p.Type = Refactor
	case featureRe.MatchString(t):
		p.Type = Feature
	default:
		p.Type = Other
	}
	return p
}

// stripTagBlocks removes leading blocks that editors and agents inject before the
// user's words, such as <ide_opened_file>...</ide_opened_file>.
func stripTagBlocks(t string) string {
	for strings.HasPrefix(t, "<") {
		end := strings.IndexAny(t, " >")
		if end < 2 {
			return t
		}
		closing := "</" + t[1:end] + ">"
		i := strings.Index(t, closing)
		if i < 0 {
			return t
		}
		t = strings.TrimSpace(t[i+len(closing):])
	}
	return t
}

// Tally counts the tasks a rule applies to and those that missed it.
type Tally struct {
	Applicable int
	Missed     int
}

// Grades summarizes prompting across sessions.
type Grades struct {
	Sessions int
	Tasks    map[string]int
	Rules    map[string]Tally
}

// signalTypes lists the task types each prompt signal applies to; acceptance
// criteria apply only to ambiguous work.
var signalTypes = map[string][]string{
	"context":      {Bug, Feature, Refactor, Test},
	"criteria":     {Feature, Refactor},
	"verification": {Bug, Feature, Refactor},
}

// SignalApplies reports whether a signal ("context", "criteria", or
// "verification") applies to a task type.
func SignalApplies(signal, taskType string) bool { return contains(signalTypes[signal], taskType) }

// Task is one task prompt and what followed it until the next task.
type Task struct {
	Prompt             Prompt // the prompt that started the task
	Corrected          bool
	FollowUps          int // later prompts within the task, including corrections
	ExploresBeforeEdit int
	Edits              int
	Checks             int
}

// Tasks groups each session's prompts into tasks: a task prompt starts one, and
// corrections and short replies that follow belong to it, with the agent's
// activity after them. A question or other non-code prompt ends the task.
func Tasks(sessions []Session) []Task {
	var out []Task
	for _, s := range sessions {
		current := -1
		for _, p := range s.Prompts {
			switch {
			case p.Type == Other:
				current = -1
				continue
			case p.Type != "":
				out = append(out, Task{Prompt: p})
				current = len(out) - 1
			case current < 0:
				continue
			default:
				out[current].FollowUps++
				out[current].Corrected = out[current].Corrected || p.Correction
			}
			t := &out[current]
			if t.Edits == 0 {
				t.ExploresBeforeEdit += p.ExploresBeforeEdit
			}
			t.Edits += p.Edits
			t.Checks += p.Checks
		}
	}
	return out
}

// codeTypes are the task types that ask for code changes.
var codeTypes = []string{Bug, Feature, Refactor, Test}

// gradedRules apply to code tasks where applies is true. Only rules grounded in
// what happened ship: the prompt-text signals (context, criteria, verification)
// did not predict outcomes in validation (see DESIGN.md), so they are reported
// by "urfluency validate" but not graded.
var gradedRules = []struct {
	id      string
	applies func(t Task) bool
	met     func(t Task) bool
}{
	{"prompt.unverified-change", func(t Task) bool { return t.Edits > 0 }, func(t Task) bool { return t.Checks > 0 }},
	{"prompt.needed-correction", func(Task) bool { return true }, func(t Task) bool { return !t.Corrected }},
}

// Grade tallies each rule over the sessions' tasks.
func Grade(sessions []Session) Grades {
	g := Grades{Sessions: len(sessions), Tasks: map[string]int{}, Rules: map[string]Tally{}}
	for _, t := range Tasks(sessions) {
		g.Tasks[t.Prompt.Type]++
		for _, r := range gradedRules {
			if !contains(codeTypes, t.Prompt.Type) || !r.applies(t) {
				continue
			}
			tally := g.Rules[r.id]
			tally.Applicable++
			if !r.met(t) {
				tally.Missed++
			}
			g.Rules[r.id] = tally
		}
	}
	return g
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
