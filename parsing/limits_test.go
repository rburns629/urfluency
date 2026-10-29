package parsing

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDeepNestingIsRefused(t *testing.T) {
	deep := "x = " + strings.Repeat("[", 5000) + strings.Repeat("]", 5000) + ";\n"
	if _, err := Parse(JavaScript, []byte(deep)); !errors.Is(err, ErrTooComplex) {
		t.Errorf("5,000 levels of nesting: err %v, want ErrTooComplex", err)
	}
	normal := "x = " + strings.Repeat("[", 50) + strings.Repeat("]", 50) + ";\n"
	tree, err := Parse(JavaScript, []byte(normal))
	if err != nil {
		t.Fatalf("50 levels of nesting: %v", err)
	}
	tree.Close()
}

func TestSlowParseIsCancelled(t *testing.T) {
	defer func(b time.Duration) { ParseBudget = b }(ParseBudget)
	ParseBudget = 0
	src := strings.Repeat("function f(a, b) { return a + b * (a - b); }\n", 20000)
	if _, err := Parse(JavaScript, []byte(src)); !errors.Is(err, ErrTooComplex) {
		t.Errorf("parse past its budget: err %v, want ErrTooComplex", err)
	}
}
