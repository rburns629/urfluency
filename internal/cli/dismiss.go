package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

// dismissal is a finding the user said is not a real problem, such as a
// placeholder flagged as a secret. Only the rule and a hash of the line are
// kept: never the path, the line, or any value in it.
type dismissal struct {
	Key    string `json:"key"` // analysis.DismissalKey
	Rule   string `json:"rule"`
	Reason string `json:"reason,omitempty"`
	Added  string `json:"added"` // YYYY-MM-DD
}

// dismissedKeys returns the saved dismissals as a set of keys for scans.
func (s settings) dismissedKeys() map[string]bool {
	keys := map[string]bool{}
	for _, d := range s.Dismissed {
		keys[d.Key] = true
	}
	return keys
}

// dismiss records, lists, or undoes dismissals:
//
//	urfluency dismiss RULE FILE:LINE [--reason TEXT]
//	urfluency dismiss --undo RULE FILE:LINE
//	urfluency dismiss --list
func dismiss(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("dismiss", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	reason := fs.String("reason", "", "")
	undo := fs.Bool("undo", false, "")
	list := fs.Bool("list", false, "")
	// Flags may come before or after the rule and FILE:LINE.
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return fmt.Errorf("%w: %v", errUsage, err)
		}
		if fs.NArg() == 0 {
			break
		}
		pos, args = append(pos, fs.Arg(0)), fs.Args()[1:]
	}
	s, err := loadSettings()
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}
	if *list {
		if len(s.Dismissed) == 0 {
			_, err := fmt.Fprintln(stdout, "No dismissed findings.")
			return err
		}
		for _, d := range s.Dismissed {
			line := fmt.Sprintf("%s  %s", d.Added, d.Rule)
			if d.Reason != "" {
				line += "  " + d.Reason
			}
			if _, err := fmt.Fprintln(stdout, line); err != nil {
				return err
			}
		}
		return nil
	}
	if len(pos) != 2 {
		return fmt.Errorf("%w: dismiss takes a rule id and FILE:LINE, as scan --json --locations shows them", errUsage)
	}
	rule, at := pos[0], pos[1]
	if _, ok := rules.Lookup(rule); !ok {
		return fmt.Errorf("%w: unknown rule %q; use the rule id a scan shows, such as sec.hardcoded-password", errUsage, rule)
	}
	path, n, err := fileLine(at)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	lines := bytes.Split(content, []byte("\n"))
	if n > len(lines) || len(bytes.TrimSpace(lines[n-1])) == 0 {
		return fmt.Errorf("%s has no code on line %d", path, n)
	}
	key := analysis.DismissalKey(rule, lines[n-1])
	i := slices.IndexFunc(s.Dismissed, func(d dismissal) bool { return d.Key == key })
	switch {
	case *undo && i < 0:
		return fmt.Errorf("%s at %s is not dismissed", rule, at)
	case *undo:
		s.Dismissed = slices.Delete(s.Dismissed, i, i+1)
	case i >= 0:
		_, err := fmt.Fprintf(stdout, "%s at %s is already dismissed.\n", rule, at)
		return err
	default:
		s.Dismissed = append(s.Dismissed, dismissal{Key: key, Rule: rule, Reason: strings.TrimSpace(*reason), Added: time.Now().Format("2006-01-02")})
	}
	if _, err := s.save(); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	if *undo {
		_, err = fmt.Fprintf(stdout, "%s at %s counts again from the next scan.\n", rule, at)
		return err
	}
	_, err = fmt.Fprintf(stdout, "Dismissed %s at %s. Scans and uploads leave it out while that line stays the same;\n"+
		"reports say how many findings you dismissed, never which. Undo with: urfluency dismiss --undo %s %s\n", rule, at, rule, shellArgs([]string{at}))
	return err
}

// fileLine splits FILE:LINE, as scan --locations prints it.
func fileLine(at string) (string, int, error) {
	i := strings.LastIndex(at, ":")
	if i <= 0 {
		return "", 0, fmt.Errorf("%w: expected FILE:LINE, got %q", errUsage, at)
	}
	n, err := strconv.Atoi(at[i+1:])
	if err != nil || n < 1 {
		return "", 0, errors.New("the line in FILE:LINE must be a number from 1")
	}
	return at[:i], n, nil
}
