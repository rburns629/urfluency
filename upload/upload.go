// Package upload defines what `urfluency upload` sends to urfluency.com: a
// report's summary and combined result, with version information. It is the
// contract between the CLI and the service, so both validate with it.
//
// A payload carries ratings, counts, rule and library names, and the summary's
// sentences. It never carries source code, prompt text, repository names, file
// paths, commit hashes, email addresses, or secret values; Validate rejects
// payloads that look like they do.
package upload

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Format versions the payload.
const Format = "1"

// MaxBytes bounds an encoded payload.
const MaxBytes = 1 << 20

// Tiers a payload can claim. The CLI only sends self-reported results; the
// service assigns the tiers it can verify itself.
const (
	SelfReported = "self-reported"
	Declared     = "declared"
)

// Payload is one upload.
type Payload struct {
	Format  string `json:"format"`
	Tool    string `json:"tool"`          // CLI version
	Report  string `json:"report_format"` // report schema the summary follows
	Catalog string `json:"catalog"`       // rule catalog version
	Model   string `json:"model"`         // proficiency model version
	Tier    string `json:"tier"`
	Since   string `json:"since"` // "all" or YYYY-MM-DD
	// Summary and Combined are the report's "summary" and "combined" objects.
	Summary  json.RawMessage `json:"summary"`
	Combined json.RawMessage `json:"combined"`
}

// forbiddenKeys name fields that would identify code or people. A report's
// summary and combined result never have them; their presence means the
// payload was not built from those parts.
var forbiddenKeys = map[string]bool{
	"locations": true, "location": true, "path": true, "paths": true, "file": true,
	"repository": true, "repo": true, "label": true, "commit": true, "head": true,
	"email": true, "emails": true, "author": true, "authors": true, "source": true,
	"code": true, "prompt": true, "secret": true, "value": true,
}

var (
	emailRe    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	homePathRe = regexp.MustCompile(`(^|[\s"'(])(/Users/|/home/|/root/|~/|[A-Za-z]:\\)`)
	hashRe     = regexp.MustCompile(`\b[0-9a-f]{40}\b`)
)

// Validate checks a payload's versions, size, and shape, and rejects anything
// that looks identifying: forbidden field names, email addresses, local paths,
// or commit hashes in any string.
func Validate(p Payload) error {
	if p.Format != Format {
		return fmt.Errorf("upload format %q, want %q", p.Format, Format)
	}
	if p.Tier != SelfReported && p.Tier != Declared {
		return fmt.Errorf("tier %q cannot be claimed by the CLI", p.Tier)
	}
	for name, v := range map[string]string{"tool": p.Tool, "report_format": p.Report, "catalog": p.Catalog, "model": p.Model, "since": p.Since} {
		if v == "" || len(v) > 64 {
			return fmt.Errorf("%s must be 1 to 64 characters", name)
		}
	}
	if len(p.Summary)+len(p.Combined) > MaxBytes {
		return fmt.Errorf("payload over %d bytes", MaxBytes)
	}
	for name, raw := range map[string]json.RawMessage{"summary": p.Summary, "combined": p.Combined} {
		var v any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, ok := v.(map[string]any); !ok {
			return fmt.Errorf("%s must be an object", name)
		}
		if err := check(name, v); err != nil {
			return err
		}
	}
	return nil
}

// ErrIdentifying marks a payload rejected for carrying identifying data.
var ErrIdentifying = errors.New("payload carries identifying data")

func check(at string, v any) error {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if forbiddenKeys[strings.ToLower(k)] {
				return fmt.Errorf("%w: field %s.%s", ErrIdentifying, at, k)
			}
			if err := check(at+"."+k, child); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range t {
			if err := check(fmt.Sprintf("%s[%d]", at, i), child); err != nil {
				return err
			}
		}
	case string:
		switch {
		case emailRe.MatchString(t):
			return fmt.Errorf("%w: email address in %s", ErrIdentifying, at)
		case homePathRe.MatchString(t):
			return fmt.Errorf("%w: local path in %s", ErrIdentifying, at)
		case hashRe.MatchString(t):
			return fmt.Errorf("%w: commit hash in %s", ErrIdentifying, at)
		}
	}
	return nil
}
