package upload

import (
	"encoding/json"
	"errors"
	"testing"
)

func valid() Payload {
	return Payload{Format: Format, Tool: "0.1.8", Report: "0.4", Catalog: "0.10", Model: "0.2-uncalibrated", Tier: SelfReported, Since: "all",
		Summary:  json.RawMessage(`{"headline":"Advanced by code","strengths":["Go, 84"],"ratings":[{"area":"language","name":"go","score":84}]}`),
		Combined: json.RawMessage(`{"repositories":94,"languages":[{"language":"go","owned_lines":79990}]}`)}
}

func TestValidate(t *testing.T) {
	if err := Validate(valid()); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	tests := []struct {
		name        string
		edit        func(*Payload)
		identifying bool
	}{
		{"wrong format", func(p *Payload) { p.Format = "0" }, false},
		{"claims verified", func(p *Payload) { p.Tier = "verified" }, false},
		{"missing tool", func(p *Payload) { p.Tool = "" }, false},
		{"summary not an object", func(p *Payload) { p.Summary = json.RawMessage(`[]`) }, false},
		{"locations", func(p *Payload) {
			p.Combined = json.RawMessage(`{"languages":[{"weaknesses":[{"locations":["a.go:1"]}]}]}`)
		}, true},
		{"repository name", func(p *Payload) { p.Combined = json.RawMessage(`{"quality_gates":[{"repository":"acme/api"}]}`) }, true},
		{"email in text", func(p *Payload) { p.Summary = json.RawMessage(`{"strengths":["by someone@example.com"]}`) }, true},
		{"home path", func(p *Payload) { p.Summary = json.RawMessage(`{"caveats":["scanned /Users/someone/work"]}`) }, true},
		{"commit hash", func(p *Payload) {
			p.Summary = json.RawMessage(`{"headline":"at 0123456789abcdef0123456789abcdef01234567"}`)
		}, true},
	}
	for _, tc := range tests {
		p := valid()
		tc.edit(&p)
		err := Validate(p)
		if err == nil {
			t.Errorf("%s: accepted", tc.name)
			continue
		}
		if errors.Is(err, ErrIdentifying) != tc.identifying {
			t.Errorf("%s: error %v, identifying = %v", tc.name, err, tc.identifying)
		}
	}
}
