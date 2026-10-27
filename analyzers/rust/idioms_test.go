package rust

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func TestIdioms(t *testing.T) {
	src := `use std::fmt;

#[derive(Debug, thiserror::Error)]
pub enum ParseError {
    #[error("empty input")]
    Empty,
}

#[derive(Debug)]
pub struct Plain;

impl std::error::Error for Plain {}

pub trait Shape {
    fn area(&self) -> f64;
}

impl fmt::Display for Plain {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "plain")
    }
}

pub struct Parser<'a> {
    input: &'a str,
}

pub fn largest<T: PartialOrd + Copy>(items: &[T]) -> Option<T> {
    items.iter().copied().fold(None, |m, x| Some(m.map_or(x, |m| if x > m { x } else { m })))
}

pub fn describe(s: impl Shape) -> String {
    format!("{}", s.area())
}

pub fn parse(input: &str) -> Result<Vec<u32>, ParseError> {
    let first = input.lines().next().ok_or(ParseError::Empty)?;
    let n: u32 = first.parse().map_err(|_| ParseError::Empty)?;
    let doubled: Vec<u32> = input.chars().filter(|c| c.is_ascii_digit()).map(|c| c as u32).collect();
    match input.len() {
        0 => {}
        _ => {}
    }
    match doubled.first() {
        Some(x) => Ok(vec![*x, n]),
        None => Err(ParseError::Empty),
    }
}

fn opt(o: Option<u8>) -> u8 { o.unwrap_or_default() }

#[cfg(test)]
mod tests {
    #[test]
    fn parses() {
        assert!(super::parse("1").is_ok());
    }
}
`
	findings, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "src/lib.rs", Content: []byte(src)}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, f := range findings {
		if f.Positive {
			got[f.Rule]++
		}
	}
	want := map[string]int{
		"rust.idiom.question-mark": 2, "rust.idiom.result-return": 1, "rust.idiom.iterator-chain": 2,
		"rust.idiom.enum-match": 1, "rust.idiom.traits": 3, "rust.idiom.trait-bounds": 2,
		"rust.idiom.combinators": 4, "rust.idiom.test-module": 1, "rust.idiom.error-type": 2,
		"rust.idiom.lifetimes": 1,
	}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf("%s = %d, want %d", rule, got[rule], n)
		}
	}
	for rule := range got {
		if r, ok := rules.Lookup(rule); !ok || r.Category != rules.Idiom {
			t.Errorf("%s is not an idiom in the catalog", rule)
		}
		if _, ok := want[rule]; !ok {
			t.Errorf("unexpected idiom %s = %d", rule, got[rule])
		}
	}
}
