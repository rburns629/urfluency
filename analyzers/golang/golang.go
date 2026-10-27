// Package golang analyzes Go source with go/ast. It parses files and never builds
// or runs them.
package golang

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"regexp"
	"strconv"
	"strings"

	"github.com/rburns629/urfluency/analysis"
)

// stubImporter fails every import, so type checking never loads or builds
// dependencies. go/types still resolves local declarations and records imported
// package names.
type stubImporter struct{}

func (stubImporter) Import(string) (*types.Package, error) {
	return nil, errors.New("imports are not loaded")
}

func init() { analysis.Register(Analyzer{}) }

var generated = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

// decoders are method or function names that fill a pointer argument from input.
var decoders = map[string]bool{"Unmarshal": true, "Decode": true, "DataTo": true}

// Analyzer implements analysis.Analyzer for Go.
type Analyzer struct{}

func (Analyzer) Language() string     { return "go" }
func (Analyzer) Extensions() []string { return []string{".go"} }

func (Analyzer) Skip(f analysis.File) bool { return generated.Match(f.Content) }

// Analyze parses each file and applies every Go rule. Files that do not parse are
// skipped, since they cannot be attributed reliably.
func (Analyzer) Analyze(ctx context.Context, files []analysis.File) ([]analysis.Finding, error) {
	var findings []analysis.Finding
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f.Path, f.Content, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
		conf := types.Config{Importer: stubImporter{}, Error: func(error) {}}
		// Errors are expected (other package files and imports are absent); the
		// partial information is still accurate for local declarations.
		_, _ = conf.Check(file.Name.Name, fset, []*ast.File{file}, info)
		c := &checker{fset: fset, path: f.Path, file: file, info: info, imports: importNames(file)}
		c.run()
		c.idioms()
		findings = append(findings, c.findings...)
	}
	return findings, nil
}

type checker struct {
	fset       *token.FileSet
	path       string
	file       *ast.File
	info       *types.Info
	imports    map[string]string // local name -> import path
	waitGroups map[types.Object]bool
	findings   []analysis.Finding
	reported   map[string]bool
}

// report records one finding per rule and position; nested loops would otherwise
// report the same call more than once.
func (c *checker) report(rule string, pos token.Pos) {
	key := rule + "@" + strconv.Itoa(int(pos))
	if c.reported[key] {
		return
	}
	if c.reported == nil {
		c.reported = map[string]bool{}
	}
	c.reported[key] = true
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: c.fset.Position(pos).Line})
}

func (c *checker) run() {
	isTest := analysis.IsTestPath(c.path)
	library := c.file.Name.Name != "main"
	checkedAsserts := map[*ast.TypeAssertExpr]bool{}
	c.waitGroups = c.collectWaitGroups()

	// Two-value assertions are safe; mark them before visiting.
	ast.Inspect(c.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) == 2 && len(n.Rhs) == 1 {
				if ta, ok := n.Rhs[0].(*ast.TypeAssertExpr); ok {
					checkedAsserts[ta] = true
				}
			}
		case *ast.ValueSpec:
			if len(n.Names) == 2 && len(n.Values) == 1 {
				if ta, ok := n.Values[0].(*ast.TypeAssertExpr); ok {
					checkedAsserts[ta] = true
				}
			}
		}
		return true
	})

	for _, decl := range c.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		c.checkSignature(fn.Type)
		if fn.Body == nil {
			continue
		}
		if !isTest {
			c.checkComplexity(fn)
		}
		allowPanic := !library || isTest || fn.Name.Name == "init" || strings.HasPrefix(fn.Name.Name, "Must")
		fnLimited := limitsConcurrency(fn.Body)
		c.checkSendLeak(fn.Body)
		c.checkFreshContext(fn.Type, fn.Body)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SwitchStmt:
				// Tagless switches (switch { case cond: }) use empty cases to mean
				// "do nothing"; the fall-through confusion arises with a tag.
				if n.Tag != nil {
					c.checkEmptyCases(n.Body)
				}
			case *ast.TypeSwitchStmt:
				c.checkEmptyCases(n.Body)
			case *ast.ForStmt:
				c.checkDecodeReuse(n.Pos(), n.Body)
				c.checkIterator(n)
				c.checkLoopBody(n.Body, false, fnLimited)
			case *ast.RangeStmt:
				c.checkDecodeReuse(n.Pos(), n.Body)
				c.checkLoopBody(n.Body, c.isChannel(n.X), fnLimited)
			case *ast.BlockStmt:
				c.checkWaitGroupAdd(n)
			case *ast.GoStmt:
				c.checkGoStmt(n)
			case *ast.FuncLit:
				c.checkSendLeak(n.Body)
				c.checkFreshContext(n.Type, n.Body)
			case *ast.SelectorExpr, *ast.CompositeLit:
				if !isTest {
					c.checkHTTPTimeout(n)
				}
			case *ast.AssignStmt:
				if !isTest {
					c.checkIgnoredError(n)
				}
			case *ast.ExprStmt:
				c.checkDiscarded(n)
				if !isTest {
					c.checkIgnoredError(n)
				}
			case *ast.TypeAssertExpr:
				if n.Type != nil && !checkedAsserts[n] && !isTest {
					c.report("go.unchecked-type-assertion", n.Pos())
				}
			case *ast.KeyValueExpr:
				c.checkInsecureTLS(n)
			case *ast.CallExpr:
				c.checkSecurity(n)
				if !isTest {
					c.checkHTTPTimeout(n)
				}
				if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "panic" && !allowPanic {
					if _, builtin := c.info.Uses[id].(*types.Builtin); builtin {
						c.report("go.panic-outside-main", n.Pos())
					}
				}
			}
			return true
		})
	}
}

// checkEmptyCases flags non-final, non-default cases with no statements, which in
// Go do nothing rather than falling through to the next case.
func (c *checker) checkEmptyCases(body *ast.BlockStmt) {
	for i, stmt := range body.List {
		clause, ok := stmt.(*ast.CaseClause)
		if !ok || clause.List == nil || len(clause.Body) > 0 || i == len(body.List)-1 {
			continue
		}
		c.report("go.switch-empty-case", clause.Pos())
	}
}

// checkDecodeReuse flags decode calls inside a loop whose target variable is
// declared before the loop statement (not in its header or body) and not
// reassigned in the loop before the call.
func (c *checker) checkDecodeReuse(loop token.Pos, body *ast.BlockStmt) {
	reset := map[types.Object]token.Pos{}
	ast.Inspect(body, func(n ast.Node) bool {
		if assign, ok := n.(*ast.AssignStmt); ok && assign.Tok == token.ASSIGN {
			for _, lhs := range assign.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && c.info.Uses[id] != nil {
					if _, seen := reset[c.info.Uses[id]]; !seen {
						reset[c.info.Uses[id]] = assign.Pos()
					}
				}
			}
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !decoders[sel.Sel.Name] {
			return true
		}
		for _, arg := range call.Args {
			unary, ok := arg.(*ast.UnaryExpr)
			if !ok || unary.Op != token.AND {
				continue
			}
			id, ok := unary.X.(*ast.Ident)
			if !ok {
				continue
			}
			v, ok := c.info.Uses[id].(*types.Var)
			if !ok || v.IsField() {
				continue
			}
			// Package-level variables persist across iterations wherever declared;
			// local ones count only if declared before the loop statement.
			pkgLevel := v.Pkg() != nil && v.Parent() == v.Pkg().Scope()
			if !pkgLevel && v.Pos() >= loop {
				continue
			}
			if pos, wasReset := reset[v]; wasReset && pos < call.Pos() {
				continue
			}
			c.report("go.decode-target-reuse", call.Pos())
		}
		return true
	})
}

// checkDiscarded flags pure formatting and error constructors used as statements.
func (c *checker) checkDiscarded(stmt *ast.ExprStmt) {
	call, ok := stmt.X.(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}
	switch c.pkgPath(pkg) {
	case "fmt":
		switch sel.Sel.Name {
		case "Errorf", "Sprintf", "Sprint", "Sprintln":
			c.report("go.discarded-result", call.Pos())
		}
	case "errors":
		if sel.Sel.Name == "New" {
			c.report("go.discarded-result", call.Pos())
		}
	}
}

// checkSignature flags map[string]any or map[string]interface{} parameters and
// results.
func (c *checker) checkSignature(ft *ast.FuncType) {
	var fields []*ast.Field
	if ft.Params != nil {
		fields = append(fields, ft.Params.List...)
	}
	if ft.Results != nil {
		fields = append(fields, ft.Results.List...)
	}
	for _, field := range fields {
		if isUntypedMap(field.Type) {
			c.report("go.untyped-map-signature", field.Pos())
		}
	}
}

func isUntypedMap(expr ast.Expr) bool {
	m, ok := expr.(*ast.MapType)
	if !ok {
		return false
	}
	if key, ok := m.Key.(*ast.Ident); !ok || key.Name != "string" {
		return false
	}
	switch v := m.Value.(type) {
	case *ast.Ident:
		return v.Name == "any"
	case *ast.InterfaceType:
		return v.Methods == nil || len(v.Methods.List) == 0
	}
	return false
}

func importNames(file *ast.File) map[string]string {
	names := map[string]string{}
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		names[name] = path
	}
	return names
}
