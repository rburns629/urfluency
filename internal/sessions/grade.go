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

// Prompt holds the signals of one user prompt; its text is not kept.
type Prompt struct {
	Type         string // "" when the prompt is not a task, such as "yes" or a slash command
	Correction   bool
	Context      bool // names files, symbols, errors, or pastes code or logs
	Criteria     bool // states expected behavior or when the task is done
	Verification bool // asks for tests or checks
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
	t := strings.ToLower(StripTagBlocks(strings.TrimSpace(text)))
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

// StripTagBlocks removes leading blocks that editors and agents inject before the
// user's words, such as <ide_opened_file>...</ide_opened_file>.
func StripTagBlocks(t string) string {
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

// gradedRules apply per task type; acceptance criteria apply only to ambiguous
// work, where they measurably reduce rework. Content rules grade only the task
// that opens a session: later prompts inherit the conversation's context.
var gradedRules = []struct {
	id          string
	types       []string
	openingOnly bool
	met         func(p Prompt, corrected bool) bool
}{
	{"prompt.no-context", []string{Bug, Feature, Refactor, Test}, true, func(p Prompt, _ bool) bool { return p.Context }},
	{"prompt.no-acceptance-criteria", []string{Feature, Refactor}, true, func(p Prompt, _ bool) bool { return p.Criteria }},
	{"prompt.no-verification", []string{Bug, Feature, Refactor}, true, func(p Prompt, _ bool) bool { return p.Verification }},
	{"prompt.needed-correction", []string{Bug, Feature, Refactor, Test}, false, func(_ Prompt, corrected bool) bool { return !corrected }},
}

// Grade groups prompts into tasks: each task prompt starts one, and corrections
// that follow count against it.
func Grade(sessions []Session) Grades {
	g := Grades{Sessions: len(sessions), Tasks: map[string]int{}, Rules: map[string]Tally{}}
	for _, s := range sessions {
		type task struct {
			p         Prompt
			corrected bool
		}
		var tasks []task
		current := -1
		for _, p := range s.Prompts {
			switch {
			case p.Correction && current >= 0:
				tasks[current].corrected = true
			case p.Type == Other:
				current = -1
			case p.Type != "":
				tasks = append(tasks, task{p: p})
				current = len(tasks) - 1
			}
		}
		for i, t := range tasks {
			g.Tasks[t.p.Type]++
			for _, r := range gradedRules {
				if !contains(r.types, t.p.Type) || (r.openingOnly && i > 0) {
					continue
				}
				tally := g.Rules[r.id]
				tally.Applicable++
				if !r.met(t.p, t.corrected) {
					tally.Missed++
				}
				g.Rules[r.id] = tally
			}
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
