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

var typeKeys = map[byte]string{'b': sessions.Bug, 'f': sessions.Feature, 'r': sessions.Refactor, 't': sessions.Test, 'o': sessions.Other}

// label is the author's judgment of one prompt.
type label struct {
	typ                             string
	context, criteria, verification bool
}

// calibrate shows a sample of the user's prompts in this terminal, asks the user
// to label each one, and prints how often the rules agreed. Nothing is saved, and
// the summary contains no prompt text.
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

	fmt.Fprintf(stdout, "Calibrating with %d of your prompts. They are shown only in this terminal and\n"+
		"nothing is saved. For each, enter the task type and three yes/no answers:\n"+
		"  type: b bug, f feature, r refactor, t test, o other (question, not a code change)\n"+
		"  then y/n for: names concrete context (files, symbols, errors), states acceptance\n"+
		"  criteria or a definition of done, asks for tests or checks.\n"+
		"  Example: \"f yny\". Enter s to skip, q to finish early.\n", len(samples))
	in := bufio.NewScanner(stdin)
	var rules, labels []sessions.Prompt
	for i, s := range samples {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		text := s.text
		if len(text) > maxShown {
			text = text[:maxShown] + " …(truncated)"
		}
		fmt.Fprintf(stdout, "\n── %d/%d ──\n%s\n", i+1, len(samples), text)
		l, ok := askLabel(in, stdout)
		if !ok {
			break
		}
		if l == nil {
			continue
		}
		rules = append(rules, s.rule)
		labels = append(labels, sessions.Prompt{Type: l.typ, Context: l.context, Criteria: l.criteria, Verification: l.verification})
	}
	return writeAgreement(stdout, rules, labels)
}

// askLabel reads one answer. It returns ok=false to finish and a nil label to skip.
func askLabel(in *bufio.Scanner, out io.Writer) (*label, bool) {
	for {
		fmt.Fprint(out, "> ")
		if !in.Scan() {
			return nil, false
		}
		answer := strings.ToLower(strings.Join(strings.Fields(in.Text()), ""))
		switch {
		case answer == "q":
			return nil, false
		case answer == "s":
			return nil, true
		case len(answer) == 4 && typeKeys[answer[0]] != "" && strings.Trim(answer[1:], "yn") == "":
			return &label{typeKeys[answer[0]], answer[1] == 'y', answer[2] == 'y', answer[3] == 'y'}, true
		}
		fmt.Fprintln(out, `Enter a type and three y/n answers, such as "f yny", or s or q.`)
	}
}

// writeAgreement prints per-signal agreement and disagreement directions.
func writeAgreement(w io.Writer, rules, labels []sessions.Prompt) error {
	var b strings.Builder
	fmt.Fprintf(&b, "\nCalibration summary: %d prompts labeled (contains no prompt text)\n", len(labels))
	if len(labels) == 0 {
		_, err := io.WriteString(w, b.String())
		return err
	}
	signals := []struct {
		name string
		get  func(sessions.Prompt) bool
	}{
		{"context", func(p sessions.Prompt) bool { return p.Context }},
		{"criteria", func(p sessions.Prompt) bool { return p.Criteria }},
		{"verification", func(p sessions.Prompt) bool { return p.Verification }},
	}
	fmt.Fprintf(&b, "  %-13s %9s  %22s  %22s\n", "signal", "agreement", "rule yes, you said no", "rule no, you said yes")
	for _, s := range signals {
		agree, falseYes, falseNo := 0, 0, 0
		for i := range labels {
			r, h := s.get(rules[i]), s.get(labels[i])
			switch {
			case r == h:
				agree++
			case r:
				falseYes++
			default:
				falseNo++
			}
		}
		fmt.Fprintf(&b, "  %-13s %8.0f%%  %22d  %22d\n", s.name, percent(agree, len(labels)), falseYes, falseNo)
	}
	agree := 0
	mismatches := map[string]int{}
	for i := range labels {
		if rules[i].Type == labels[i].Type {
			agree++
		} else {
			mismatches["you "+labels[i].Type+", rule "+rules[i].Type]++
		}
	}
	fmt.Fprintf(&b, "  %-13s %8.0f%%\n", "task type", percent(agree, len(labels)))
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

func percent(n, total int) float64 { return float64(n) * 100 / float64(total) }

func firstOr(args []string, fallback string) string {
	if len(args) > 0 {
		return args[0]
	}
	return fallback
}
