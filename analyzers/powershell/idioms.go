package powershell

import (
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"

	"github.com/rburns629/urfluency/analysis"
	"github.com/rburns629/urfluency/parsing"
)

func (c *checker) idiom(rule string, n *ts.Node) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: parsing.Line(n), Positive: true})
}

// visitIdioms finds the PowerShell idioms that show fluency: each is a
// deliberate choice that makes scripts behave like well-built cmdlets.
func (c *checker) visitIdioms(n *ts.Node) bool {
	switch n.Kind() {
	case "attribute":
		c.attributeIdioms(n)
	case "variable":
		if p := n.Parent(); strings.HasPrefix(c.tree.Text(n), "@") && p != nil && p.Kind() == "unary_expression" {
			c.idiom("powershell.idiom.splatting", n)
		}
	case "comment":
		t := strings.ToUpper(c.tree.Text(n))
		if strings.HasPrefix(t, "<#") && (strings.Contains(t, ".SYNOPSIS") || strings.Contains(t, ".DESCRIPTION")) {
			c.idiom("powershell.idiom.comment-based-help", n)
		}
	case "command":
		if strings.EqualFold(c.commandName(n), "Set-StrictMode") {
			c.idiom("powershell.idiom.strict-mode", n)
		}
	case "assignment_expression":
		t := strings.ToLower(c.tree.Text(n))
		if strings.HasPrefix(t, "$erroractionpreference") && strings.Contains(t, "stop") {
			c.idiom("powershell.idiom.strict-mode", n)
		}
	}
	return true
}

func (c *checker) attributeIdioms(n *ts.Node) {
	name := n.ChildByFieldName("attribute_name")
	if name == nil {
		name = n.NamedChild(0)
	}
	if name == nil {
		return
	}
	if name.Kind() == "type_literal" {
		if p := n.Parent(); p != nil && p.Parent() != nil && p.Parent().Kind() == "script_parameter" {
			c.idiom("powershell.idiom.typed-parameters", n)
		}
		return
	}
	attr := strings.ToLower(c.tree.Text(name))
	args := strings.ToLower(c.tree.Text(n))
	switch {
	case attr == "cmdletbinding":
		c.idiom("powershell.idiom.advanced-function", n)
		if strings.Contains(args, "supportsshouldprocess") {
			c.idiom("powershell.idiom.should-process", n)
		}
	case attr == "parameter":
		if strings.Contains(args, "mandatory") {
			c.idiom("powershell.idiom.parameter-validation", n)
		}
		if strings.Contains(args, "valuefrompipeline") {
			c.idiom("powershell.idiom.pipeline-input", n)
		}
	case strings.HasPrefix(attr, "validate"):
		c.idiom("powershell.idiom.parameter-validation", n)
	}
}
