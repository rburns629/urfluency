package ruby

import (
	"context"
	"testing"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/rules"
)

func TestIdioms(t *testing.T) {
	src := `# frozen_string_literal: true

Point = Struct.new(:x, :y, keyword_init: true)
Coord = Data.define(:lat, :lng)

class Version
  include Comparable
  attr_reader :parts

  def initialize(text:, separator: ".")
    @parts = text.split(separator).map(&:to_i)
  end

  def <=>(other) = parts <=> other.parts
  def major = parts.first

  def each_part
    return enum_for(:each_part) unless block_given?
    parts.each { |p| yield p }
  end
end

class Registry
  extend Enumerable
  def self.fetch(name, default: nil) = items[name] || default
end

def describe(event)
  raise ArgumentError, "no event" if event.nil?
  case event
  in {type: "click", target:} then "clicked #{target}"
  in {type: String => type} then type
  end
end

def totals(orders)
  return {} if orders.empty?
  orders.each_with_object(Hash.new(0)) { |o, h| h[o.user&.id] += o.amount }
end

def valid?(config) = config in {name: String}

def names(users)
  users.select(&:active?).map { |u| u.profile&.name }
  users.each do |u|
    next unless u
    puts u
  end
  users.first(2)
end
`
	findings, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "lib/a.rb", Content: []byte(src)}})
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
		"ruby.idiom.frozen-string-literal": 1, "ruby.idiom.value-object": 2, "ruby.idiom.comparable-enumerable": 2,
		"ruby.idiom.keyword-arguments": 2, "ruby.idiom.endless-method": 4, "ruby.idiom.yield-block": 1,
		"ruby.idiom.guard-clause": 4, "ruby.idiom.pattern-matching": 2, "ruby.idiom.safe-navigation": 2,
		"ruby.idiom.enumerable-methods": 4,
	}
	for rule, n := range want {
		if got[rule] != n {
			t.Errorf("%s = %d, want %d", rule, got[rule], n)
		}
	}
	for rule := range got {
		if _, ok := want[rule]; !ok {
			t.Errorf("unexpected idiom %s = %d", rule, got[rule])
		}
		if r, ok := rules.Lookup(rule); !ok || r.Category != rules.Idiom {
			t.Errorf("%s is not an idiom in the catalog", rule)
		}
	}
}

func TestFrozenStringLiteral(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"after other magic comments", "#!/usr/bin/env ruby\n# encoding: utf-8\n# frozen_string_literal: true\nx = 1\n", true},
		{"Emacs style", "# -*- frozen-string-literal: true -*-\nx = 1\n", true},
		{"false", "# frozen_string_literal: false\nx = 1\n", false},
		{"after code", "x = 1\n# frozen_string_literal: true\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings, err := Analyzer{}.Analyze(context.Background(), []analysis.File{{Path: "lib/a.rb", Content: []byte(tt.src)}})
			if err != nil {
				t.Fatal(err)
			}
			got := false
			for _, f := range findings {
				got = got || f.Rule == "ruby.idiom.frozen-string-literal"
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
