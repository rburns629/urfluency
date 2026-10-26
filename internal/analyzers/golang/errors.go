package golang

import (
	"go/ast"
	"go/token"
	"go/types"
)

// errorOnly lists functions whose only result is an error.
var errorOnly = map[string]bool{
	"encoding/json.Unmarshal": true,
	"os.Setenv":               true,
	"os.Remove":               true,
	"os.RemoveAll":            true,
	"os.MkdirAll":             true,
	"os.WriteFile":            true,
	"os.Chdir":                true,
}

// valueAndError lists functions returning (value, error).
var valueAndError = map[string]bool{
	"encoding/json.Marshal":       true,
	"encoding/json.MarshalIndent": true,
	"strconv.Atoi":                true,
	"strconv.ParseInt":            true,
	"strconv.ParseUint":           true,
	"strconv.ParseFloat":          true,
	"strconv.ParseBool":           true,
	"time.Parse":                  true,
	"time.ParseDuration":          true,
	"time.LoadLocation":           true,
	"os.ReadFile":                 true,
	"os.Open":                     true,
	"os.Create":                   true,
	"io.ReadAll":                  true,
	"net/url.Parse":               true,
	"regexp.Compile":              true,
	"regexp.MatchString":          true,
}

// pkgPath returns the import path an identifier refers to, or "" if it is not a
// package name (for example a local variable shadowing it).
func (c *checker) pkgPath(id *ast.Ident) string {
	switch obj := c.info.Uses[id].(type) {
	case *types.PkgName:
		return obj.Imported().Path()
	case nil:
		return c.imports[id.Name]
	}
	return ""
}

// qualifiedName returns "importpath.Func" for a package-qualified call.
func (c *checker) qualifiedName(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	path := c.pkgPath(pkg)
	if path == "" {
		return ""
	}
	return path + "." + sel.Sel.Name
}

// checkIgnoredError flags known fallible calls whose error is dropped, either by
// using the call as a statement or by assigning the error to the blank identifier.
func (c *checker) checkIgnoredError(n ast.Node) {
	switch n := n.(type) {
	case *ast.ExprStmt:
		if call, ok := n.X.(*ast.CallExpr); ok {
			if name := c.qualifiedName(call); errorOnly[name] || valueAndError[name] {
				c.report("go.ignored-error", call.Pos())
			}
		}
	case *ast.AssignStmt:
		if len(n.Rhs) != 1 {
			return
		}
		call, ok := n.Rhs[0].(*ast.CallExpr)
		if !ok {
			return
		}
		name := c.qualifiedName(call)
		switch {
		case errorOnly[name] && len(n.Lhs) == 1 && isBlank(n.Lhs[0]):
			c.report("go.ignored-error", call.Pos())
		case valueAndError[name] && len(n.Lhs) == 2 && isBlank(n.Lhs[1]):
			c.report("go.ignored-error", call.Pos())
		}
	}
}

func isBlank(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "_"
}

// checkIterator inspects `for { v, err := it.Next() ... }` loops. It flags loops
// that only stop on any error (dropping real failures) and loops that only check
// the end sentinel (spinning forever when an error repeats).
func (c *checker) checkIterator(loop *ast.ForStmt) {
	if loop.Init != nil || loop.Cond != nil || loop.Post != nil {
		return
	}
	var errObj types.Object
	for _, stmt := range loop.Body.List {
		as, ok := stmt.(*ast.AssignStmt)
		if !ok || len(as.Lhs) != 2 || len(as.Rhs) != 1 {
			continue
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			continue
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Next" {
			if id, ok := as.Lhs[1].(*ast.Ident); ok {
				errObj = c.info.Defs[id]
				if errObj == nil {
					errObj = c.info.Uses[id]
				}
				break
			}
		}
	}
	if errObj == nil {
		return
	}
	checksSentinel, checksAny := false, false
	ast.Inspect(loop.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BinaryExpr:
			other := n.Y
			if c.isIdentOf(n.Y, errObj) {
				other = n.X
			} else if !c.isIdentOf(n.X, errObj) {
				return true
			}
			switch {
			case n.Op == token.NEQ && isNil(other):
				checksAny = true
			case n.Op == token.EQL && isEndSentinel(other):
				checksSentinel = true
			}
		case *ast.CallExpr:
			if c.qualifiedName(n) == "errors.Is" && len(n.Args) == 2 && c.isIdentOf(n.Args[0], errObj) && isEndSentinel(n.Args[1]) {
				checksSentinel = true
			}
		}
		return true
	})
	if checksSentinel != checksAny {
		c.report("go.iterator-error-handling", loop.Pos())
	}
}

func isNil(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "nil"
}

// isEndSentinel matches iterator.Done, io.EOF, and similar names.
func isEndSentinel(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		return e.Sel.Name == "Done" || e.Sel.Name == "EOF"
	case *ast.Ident:
		return e.Name == "Done" || e.Name == "EOF"
	}
	return false
}
