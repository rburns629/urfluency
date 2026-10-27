package python

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func TestIdioms(t *testing.T) {
	src := `from dataclasses import dataclass
from pathlib import Path
from typing import Protocol

import pytest
from pydantic import BaseModel


@dataclass
class Point:
    x: int


class User(BaseModel):
    name: str


class NotFound(Exception):
    pass


class Reader(Protocol):
    def read(self) -> str: ...


def names(users: list[User]) -> list[str]:
    return [u.name for u in users]


def lines(path: Path):
    with open(path) as f:
        for line in f:
            yield f"{line.strip()}"


async def fetch(client):
    return await client.get("/")


@pytest.mark.parametrize("n", [1, 2])
def test_n(n):
    assert n
`
	findings, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "app.py", Content: []byte(src)}})
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
		"py.idiom.structured-data": 2, "py.idiom.custom-exception": 1, "py.idiom.protocol": 1, "py.idiom.type-hints": 2,
		"py.idiom.comprehension": 1, "py.idiom.context-manager": 1, "py.idiom.generator": 1, "py.idiom.f-string": 1,
		"py.idiom.async": 1, "py.idiom.pathlib": 1, "py.idiom.pytest": 1,
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
	}
}
