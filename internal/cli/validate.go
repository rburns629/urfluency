package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rburns629/urfluency/internal/sessions"
)

// validate prints, for each prompt signal, what happened in tasks whose prompt
// had the signal and in tasks whose prompt did not. Maintainers run it to decide
// which prompting rules ship; the output has counts only.
func validate(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("%w: validate takes at most one path", errUsage)
	}
	path := "."
	if fs.NArg() == 1 {
		path = fs.Arg(0)
	}
	root, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}
	repos, err := findRepos(root)
	if err != nil {
		return err
	}
	found, _, err := loadSessions(root, repos, time.Now().AddDate(-1, 0, 0))
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return writeValidation(stdout, found)
}

// outcome sums what happened across a group of tasks.
type outcome struct {
	tasks, corrected, followUps int
	edited, exploresBeforeEdit  int // over tasks with edits
	checked                     int // tasks with edits where a check ran
}

func (o *outcome) add(t sessions.Task) {
	o.tasks++
	o.followUps += t.FollowUps
	if t.Corrected {
		o.corrected++
	}
	if t.Edits > 0 {
		o.edited++
		o.exploresBeforeEdit += t.ExploresBeforeEdit
		if t.Checks > 0 {
			o.checked++
		}
	}
}

var validatedSignals = []struct {
	name string
	has  func(sessions.Prompt) bool
}{
	{"context", func(p sessions.Prompt) bool { return p.Context }},
	{"criteria", func(p sessions.Prompt) bool { return p.Criteria }},
	{"verification", func(p sessions.Prompt) bool { return p.Verification }},
}

func writeValidation(w io.Writer, found []sessions.Session) error {
	tasks := sessions.Tasks(found)
	var b strings.Builder
	fmt.Fprintf(&b, "Prompt signals vs. what happened next: %d tasks in %d sessions (counts only)\n", len(tasks), len(found))
	type totals struct{ sessions, prompts, edits, checks int }
	byAgent := map[string]*totals{}
	var agents []string
	for _, s := range found {
		t := byAgent[s.Agent]
		if t == nil {
			t = &totals{}
			byAgent[s.Agent] = t
			agents = append(agents, s.Agent)
		}
		t.sessions++
		t.prompts += len(s.Prompts)
		for _, p := range s.Prompts {
			t.edits += p.Edits
			t.checks += p.Checks
		}
	}
	sort.Strings(agents)
	for _, a := range agents {
		t := byAgent[a]
		fmt.Fprintf(&b, "  %-15s %d sessions, %d user messages, %d edits, %d checks\n", a, t.sessions, t.prompts, t.edits, t.checks)
	}
	for _, s := range validatedSignals {
		var with, without outcome
		for _, t := range tasks {
			if !sessions.SignalApplies(s.name, t.Prompt.Type) {
				continue
			}
			if s.has(t.Prompt) {
				with.add(t)
			} else {
				without.add(t)
			}
		}
		fmt.Fprintf(&b, "\n%s: %d tasks with the signal, %d without\n", s.name, with.tasks, without.tasks)
		fmt.Fprintf(&b, "  %-36s %10s %10s\n", "", "with", "without")
		fmt.Fprintf(&b, "  %-36s %10s %10s\n", "needed a correction", share(with.corrected, with.tasks), share(without.corrected, without.tasks))
		fmt.Fprintf(&b, "  %-36s %10s %10s\n", "follow-up prompts per task", mean(with.followUps, with.tasks), mean(without.followUps, without.tasks))
		fmt.Fprintf(&b, "  %-36s %10s %10s\n", "reads and searches before first edit", mean(with.exploresBeforeEdit, with.edited), mean(without.exploresBeforeEdit, without.edited))
		fmt.Fprintf(&b, "  %-36s %10s %10s\n", "code changes checked or tested", share(with.checked, with.edited), share(without.checked, without.edited))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func share(n, total int) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", float64(n)*100/float64(total))
}

func mean(sum, n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(sum)/float64(n))
}
