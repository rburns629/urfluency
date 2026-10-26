package golang

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// commentMentions reports whether a comment on pos's line or the line above
// mentions word, which marks a written-down decision, such as a client that
// deliberately has no timeout because it streams.
func (c *checker) commentMentions(pos token.Pos, word string) bool {
	line := c.fset.Position(pos).Line
	for _, group := range c.file.Comments {
		at := c.fset.Position(group.End()).Line
		if (at == line || at == line-1) && strings.Contains(strings.ToLower(group.Text()), word) {
			return true
		}
	}
	return false
}

// httpShortcuts use http.DefaultClient, which has no timeout.
var httpShortcuts = map[string]bool{
	"net/http.Get": true, "net/http.Head": true, "net/http.Post": true, "net/http.PostForm": true,
}

// hasRequestContext reports whether a function receives a context.Context or an
// *http.Request, so a request-scoped context is already available.
func (c *checker) hasRequestContext(ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, field := range ft.Params.List {
		t := field.Type
		if star, ok := t.(*ast.StarExpr); ok {
			t = star.X
		}
		sel, ok := t.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			continue
		}
		switch c.imports[id.Name] + "." + sel.Sel.Name {
		case "context.Context", "net/http.Request":
			return true
		}
	}
	return false
}

// checkFreshContext flags context.Background and context.TODO in a function that
// already has a request context, which drops its cancellation and deadline.
// Cleanup that runs after cancellation (in a <-ctx.Done() case or an
// if ctx.Err() != nil block) needs a fresh context, so it is skipped.
func (c *checker) checkFreshContext(ft *ast.FuncType, body *ast.BlockStmt) {
	if body == nil || !c.hasRequestContext(ft) {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CommClause:
			if receivesDone(n.Comm) {
				return false
			}
		case *ast.IfStmt:
			if checksErr(n.Cond) {
				return false
			}
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if q := c.qualifiedName(call); q == "context.Background" || q == "context.TODO" {
				c.report("go.context-background-in-handler", call.Pos())
			}
		}
		return true
	})
}

// receivesDone reports whether a select case receives from a Done() channel.
func receivesDone(comm ast.Stmt) bool {
	var expr ast.Expr
	switch s := comm.(type) {
	case *ast.ExprStmt:
		expr = s.X
	case *ast.AssignStmt:
		if len(s.Rhs) == 1 {
			expr = s.Rhs[0]
		}
	}
	recv, ok := expr.(*ast.UnaryExpr)
	return ok && recv.Op == token.ARROW && isMethodCall(recv.X, "Done")
}

// checksErr reports whether a condition is ctx.Err() != nil.
func checksErr(cond ast.Expr) bool {
	bin, ok := cond.(*ast.BinaryExpr)
	return ok && bin.Op == token.NEQ && isMethodCall(bin.X, "Err") && isNil(bin.Y)
}

func isMethodCall(expr ast.Expr, method string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == method
}

// isChannel reports whether an expression's type is known to be a channel.
// Types from other packages are not loaded, so unknown types return false.
func (c *checker) isChannel(expr ast.Expr) bool {
	tv, ok := c.info.Types[expr]
	if !ok || tv.Type == nil {
		return false
	}
	_, isChan := tv.Type.Underlying().(*types.Chan)
	return isChan
}

// checkLoopBody flags defer statements that run only when the function returns,
// and, for loops over a channel, a goroutine per message without a bound. A
// slice has a known size; a channel can deliver without limit. Function literals
// are their own scope for defer, so their bodies are skipped.
func (c *checker) checkLoopBody(body *ast.BlockStmt, perMessage bool, fnLimited bool) {
	bounded := fnLimited
	var launches []*ast.GoStmt
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SendStmt:
			bounded = true
		case *ast.GoStmt:
			launches = append(launches, n)
		}
		return true
	})
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.DeferStmt:
			c.report("go.defer-in-loop", n.Pos())
		}
		return true
	})
	if perMessage && !bounded {
		for _, g := range launches {
			c.report("go.unbounded-goroutines", g.Pos())
		}
	}
}

// limitsConcurrency reports whether a function bounds goroutines with
// errgroup.SetLimit or a semaphore's Acquire.
func limitsConcurrency(body *ast.BlockStmt) bool {
	limited := false
	ast.Inspect(body, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && (sel.Sel.Name == "SetLimit" || sel.Sel.Name == "Acquire") {
			limited = true
		}
		return !limited
	})
	return limited
}

// checkHTTPTimeout flags requests through http.DefaultClient and http.Client
// values built without a Timeout, which can wait forever on a stalled server.
func (c *checker) checkHTTPTimeout(n ast.Node) {
	switch n := n.(type) {
	case *ast.CallExpr:
		if httpShortcuts[c.qualifiedName(n)] {
			c.report("go.http-without-timeout", n.Pos())
		}
	case *ast.SelectorExpr:
		if id, ok := n.X.(*ast.Ident); ok && c.imports[id.Name] == "net/http" && n.Sel.Name == "DefaultClient" {
			c.report("go.http-without-timeout", n.Pos())
		}
	case *ast.CompositeLit:
		sel, ok := n.Type.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if id, ok := sel.X.(*ast.Ident); !ok || c.imports[id.Name] != "net/http" || sel.Sel.Name != "Client" {
			return
		}
		for _, elt := range n.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Timeout" {
					return
				}
			}
		}
		if !c.commentMentions(n.Pos(), "timeout") {
			c.report("go.http-without-timeout", n.Pos())
		}
	}
}
