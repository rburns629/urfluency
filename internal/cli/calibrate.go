package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rburns629/urfluency/internal/sessions"
)

// maxShown bounds how much of a long prompt, such as a pasted log, is displayed.
const maxShown = 1200

const rule = "──────────────────────────────────────────────────────────"

var typeChoices = []struct{ key, typ, text string }{
	{"1", sessions.Bug, "Fix a bug"},
	{"2", sessions.Feature, "New feature or change"},
	{"3", sessions.Refactor, "Refactor or cleanup"},
	{"4", sessions.Test, "Add tests"},
	{"5", sessions.Other, "Not a code change (question, chat, git or housekeeping)"},
}

// signalQuestions are asked in order, only when the signal is graded for the
// chosen task type.
var signalQuestions = []struct{ signal, question, hint string }{
	{"context", "Does it point to specific code?", "(file, function, error message, pasted code or logs)"},
	{"criteria", `Does it say what "done" looks like?`, "(expected behavior, edge cases, what must not change)"},
	{"verification", "Does it ask the agent to test or check its work?", ""},
}

// answerHelp explains the flow; it is shown at the start and in help.
const answerHelp = `Each prompt is shown with a few short questions:
  1. What is it asking for? Pick 1-5: bug fix, feature or change, refactor or
     cleanup, tests, or not a code change.
  2. Then y or n: does it point to specific code, does it say what "done" looks
     like, and does it ask the agent to test its work? Questions that do not
     apply to the type you picked are skipped.
  Press Enter after each answer. u goes back one step, s skips a prompt, and
  q finishes and shows the results.
`

// label is the author's judgment of one prompt: its task type and the answers to
// the questions asked for that type.
type label struct {
	typ     string
	answers map[string]bool
}

type action int

const (
	answered action = iota
	back
	skip
	quit
)

// calibrate shows a sample of the user's prompts in this terminal, asks the user
// to label each one, and prints how often the rules agreed. Nothing is saved, and
// the summary contains no prompt text. The rules' answers are not shown until the
// end, so they cannot sway the labels.
func calibrate(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("calibrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	size := fs.Int("sample", 40, "")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 1 || *size <= 0 {
		return fmt.Errorf("%w: calibrate takes at most one path and a positive --sample", errUsage)
	}
	root, err := filepath.Abs(firstOr(fs.Args(), "."))
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	repos, err := findRepos(root)
	if err != nil {
		return err
	}
	found, _, err := loadSessions(root, repos, time.Now().AddDate(-1, 0, 0), true)
	if err != nil {
		return err
	}
	type sample struct {
		text string
		rule sessions.Prompt
	}
	var samples []sample
	for _, s := range found {
		for i, p := range s.Prompts {
			if p.Type != "" && i < len(s.Texts) {
				samples = append(samples, sample{sessions.StripTagBlocks(strings.TrimSpace(s.Texts[i])), p})
			}
		}
	}
	rand.Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })
	if len(samples) > *size {
		samples = samples[:*size]
	}
	if len(samples) == 0 {
		_, err := fmt.Fprintln(stdout, "No prompts found in agent sessions for this path.")
		return err
	}

	fmt.Fprintf(stdout, "You will label %d of your prompts. They are shown only in this terminal, and\n"+
		"nothing is saved.\n\n%s", len(samples), answerHelp)
	in := bufio.NewScanner(stdin)
	labels := make([]*label, len(samples))
	for i := 0; i < len(samples); {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		text := samples[i].text
		if len(text) > maxShown {
			text = text[:maxShown] + " …(truncated)"
		}
		fmt.Fprintf(stdout, "\nPrompt %d of %d\n%s\n%s\n%s\n", i+1, len(samples), rule, text, rule)
		l, act := askPrompt(in, stdout)
		if act == quit {
			break
		}
		if act == back {
			if i > 0 {
				i--
				labels[i] = nil
			}
			continue
		}
		labels[i] = l // nil when skipped
		i++
	}
	var rules []sessions.Prompt
	var given []label
	for i, l := range labels {
		if l != nil {
			rules = append(rules, samples[i].rule)
			given = append(given, *l)
		}
	}
	return writeAgreement(stdout, rules, given)
}

// askPrompt asks the type and then each applicable question. Going back from the
// first question returns back, so the caller reopens the previous prompt.
func askPrompt(in *bufio.Scanner, out io.Writer) (*label, action) {
	l := &label{answers: map[string]bool{}}
	var questions []int // indexes into signalQuestions for the chosen type
	for step := 0; ; {
		if step == 0 {
			fmt.Fprintln(out, "What is this prompt asking for?")
			valid := ""
			for _, c := range typeChoices {
				fmt.Fprintf(out, "  %s %s\n", c.key, c.text)
				valid += c.key
			}
			key, act := ask(in, out, "1-5", valid)
			if act != answered {
				return nil, act
			}
			questions = questions[:0]
			for _, c := range typeChoices {
				if c.key == key {
					l.typ = c.typ
				}
			}
			for qi, q := range signalQuestions {
				if sessions.SignalApplies(q.signal, l.typ) {
					questions = append(questions, qi)
				}
			}
			clear(l.answers)
			step = 1
			continue
		}
		if step > len(questions) {
			return l, answered
		}
		q := signalQuestions[questions[step-1]]
		fmt.Fprintf(out, "\n%s\n", q.question)
		if q.hint != "" {
			fmt.Fprintln(out, q.hint)
		}
		key, act := ask(in, out, "y/n", "yn")
		switch act {
		case answered:
			l.answers[q.signal] = key == "y"
			step++
		case back:
			step--
		default:
			return nil, act
		}
	}
}

// ask reads one answer: a key from valid, or u, s, or q. End of input finishes.
func ask(in *bufio.Scanner, out io.Writer, choices, valid string) (string, action) {
	for {
		fmt.Fprintf(out, "[%s, u back, s skip, q finish] > ", choices)
		if !in.Scan() {
			return "", quit
		}
		key := strings.ToLower(strings.TrimSpace(in.Text()))
		switch {
		case key == "u":
			return "", back
		case key == "s":
			return "", skip
		case key == "q":
			return "", quit
		case len(key) == 1 && strings.Contains(valid, key):
			return key, answered
		}
		fmt.Fprintf(out, "Please enter %s.\n", choices)
	}
}

// writeAgreement prints, for each question, how often the rules matched the
// author's answers and which way they differed.
func writeAgreement(w io.Writer, rules []sessions.Prompt, labels []label) error {
	var b strings.Builder
	fmt.Fprintf(&b, "\nResults: %d prompts labeled (this summary contains no prompt text)\n\n", len(labels))
	if len(labels) == 0 {
		_, err := io.WriteString(w, b.String())
		return err
	}
	signal := map[string]func(sessions.Prompt) bool{
		"context":      func(p sessions.Prompt) bool { return p.Context },
		"criteria":     func(p sessions.Prompt) bool { return p.Criteria },
		"verification": func(p sessions.Prompt) bool { return p.Verification },
	}
	fmt.Fprintf(&b, "  %-13s %-15s %-28s %s\n", "question", "rules agreed", "rules yes, you said no", "rules no, you said yes")
	for _, q := range signalQuestions {
		asked, agree, falseYes, falseNo := 0, 0, 0, 0
		for i, l := range labels {
			h, ok := l.answers[q.signal]
			if !ok {
				continue
			}
			asked++
			switch r := signal[q.signal](rules[i]); {
			case r == h:
				agree++
			case r:
				falseYes++
			default:
				falseNo++
			}
		}
		if asked == 0 {
			fmt.Fprintf(&b, "  %-13s not asked\n", q.signal)
			continue
		}
		fmt.Fprintf(&b, "  %-13s %-15s %-28d %d\n", q.signal, ratio(agree, asked), falseYes, falseNo)
	}
	agree := 0
	mismatches := map[string]int{}
	for i, l := range labels {
		if rules[i].Type == l.typ {
			agree++
		} else {
			mismatches["you said "+l.typ+", rules said "+rules[i].Type]++
		}
	}
	fmt.Fprintf(&b, "  %-13s %s\n", "task type", ratio(agree, len(labels)))
	keys := make([]string, 0, len(mismatches))
	for k := range mismatches {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if mismatches[keys[i]] != mismatches[keys[j]] {
			return mismatches[keys[i]] > mismatches[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		fmt.Fprintf(&b, "    %s: %d\n", k, mismatches[k])
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func ratio(n, total int) string {
	return fmt.Sprintf("%d/%d %.0f%%", n, total, float64(n)*100/float64(total))
}

func firstOr(args []string, fallback string) string {
	if len(args) > 0 {
		return args[0]
	}
	return fallback
}
