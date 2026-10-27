package golang

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/rburns629/urfluency/analysis"
)

// idiom records a positive finding: an idiom the code uses well.
func (c *checker) idiom(rule string, pos token.Pos) {
	c.findings = append(c.findings, analysis.Finding{Rule: rule, Path: c.path, Line: c.fset.Position(pos).Line, Positive: true})
}

// idioms finds the Go idioms that show fluency: they are each a deliberate
// choice the language does not force.
func (c *checker) idioms() {
	isTest := analysis.IsTestPath(c.path)
	ast.Inspect(c.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			if n.Type.TypeParams != nil {
				c.idiom("go.idiom.generics", n.Pos())
			}
			if params := n.Type.Params.List; len(params) > 0 && c.isContext(params[0].Type) {
				c.idiom("go.idiom.context-first", n.Pos())
			}
			if n.Recv != nil && n.Name.Name == "Error" && len(n.Type.Params.List) == 0 &&
				n.Type.Results != nil && len(n.Type.Results.List) == 1 && isIdent(n.Type.Results.List[0].Type, "string") {
				c.idiom("go.idiom.custom-error-type", n.Pos())
			}
		case *ast.TypeSpec:
			if n.TypeParams != nil {
				c.idiom("go.idiom.generics", n.Pos())
			}
			if it, ok := n.Type.(*ast.InterfaceType); ok && len(it.Methods.List) >= 1 && len(it.Methods.List) <= 3 {
				c.idiom("go.idiom.small-interface", n.Pos())
			}
		case *ast.CallExpr:
			switch c.callee(n) {
			case "fmt.Errorf":
				if len(n.Args) > 0 && strings.Contains(stringValue(n.Args[0]), "%w") {
					c.idiom("go.idiom.error-wrapping", n.Pos())
				}
			case "errors.Is", "errors.As":
				c.idiom("go.idiom.error-inspection", n.Pos())
			case "errgroup.WithContext", "semaphore.NewWeighted":
				c.idiom("go.idiom.structured-concurrency", n.Pos())
			}
		case *ast.DeferStmt:
			if sel, ok := n.Call.Fun.(*ast.SelectorExpr); ok {
				switch sel.Sel.Name {
				case "Close", "Unlock", "RUnlock", "Done", "Stop":
					c.idiom("go.idiom.defer-cleanup", n.Pos())
				}
			} else if isIdent(n.Call.Fun, "cancel") {
				c.idiom("go.idiom.defer-cleanup", n.Pos())
			}
		case *ast.SelectStmt:
			for _, s := range n.Body.List {
				if cc, ok := s.(*ast.CommClause); ok && receivesDone(cc.Comm) {
					c.idiom("go.idiom.cancellation", n.Pos())
					break
				}
			}
		case *ast.RangeStmt:
			if isTest && containsSubtest(n.Body) {
				c.idiom("go.idiom.table-driven-test", n.Pos())
			}
		}
		return true
	})
}

// callee returns "pkg.Func" for a call to an imported package's function, using
// the import path's last element so renamed imports still match.
func (c *checker) callee(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	path, ok := c.imports[pkg.Name]
	if !ok {
		return ""
	}
	return path[strings.LastIndex(path, "/")+1:] + "." + sel.Sel.Name
}

func (c *checker) isContext(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Context" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && c.imports[pkg.Name] == "context"
}

func isIdent(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}

func stringValue(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return s
}

// containsSubtest reports whether a loop body calls t.Run.
func containsSubtest(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Run" {
				if id, ok := sel.X.(*ast.Ident); ok && (id.Name == "t" || id.Name == "b") {
					found = true
				}
			}
		}
		return !found
	})
	return found
}
