package golang

import (
	"go/ast"
	"go/token"
	"go/types"
)

// collectWaitGroups finds variables and fields declared as sync.WaitGroup or
// *sync.WaitGroup, including params and values built with &sync.WaitGroup{}.
func (c *checker) collectWaitGroups() map[types.Object]bool {
	wgs := map[types.Object]bool{}
	mark := func(id *ast.Ident) {
		if obj := c.info.Defs[id]; obj != nil {
			wgs[obj] = true
		}
	}
	ast.Inspect(c.file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if (n.Type != nil && c.isWaitGroupType(n.Type)) || (i < len(n.Values) && c.isWaitGroupValue(n.Values[i])) {
					mark(name)
				}
			}
		case *ast.Field:
			if c.isWaitGroupType(n.Type) {
				for _, name := range n.Names {
					mark(name)
				}
			}
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for i, rhs := range n.Rhs {
					if i >= len(n.Lhs) {
						break
					}
					if id, ok := n.Lhs[i].(*ast.Ident); ok && c.isWaitGroupValue(rhs) {
						mark(id)
					}
				}
			}
		}
		return true
	})
	return wgs
}

func (c *checker) isWaitGroupType(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "WaitGroup" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && c.pkgPath(pkg) == "sync"
}

func (c *checker) isWaitGroupValue(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		return e.Op == token.AND && c.isWaitGroupValue(e.X)
	case *ast.CompositeLit:
		return e.Type != nil && c.isWaitGroupType(e.Type)
	case *ast.CallExpr:
		id, ok := e.Fun.(*ast.Ident)
		return ok && id.Name == "new" && len(e.Args) == 1 && c.isWaitGroupType(e.Args[0])
	}
	return false
}

// isWaitGroupCall reports whether call is wg.<method>() on a known WaitGroup.
func (c *checker) isWaitGroupCall(call *ast.CallExpr, method string) bool {
	return c.waitGroupOf(call, method) != nil
}

// waitGroupOf returns the WaitGroup a wg.<method>() call is made on, or nil.
func (c *checker) waitGroupOf(call *ast.CallExpr, method string) types.Object {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != method {
		return nil
	}
	var obj types.Object
	switch x := sel.X.(type) {
	case *ast.Ident:
		obj = c.info.Uses[x]
	case *ast.SelectorExpr:
		obj = c.info.Uses[x.Sel]
	}
	if !c.waitGroups[obj] {
		return nil
	}
	return obj
}

// refersTo reports whether n mentions obj, as an identifier or a field selector.
func (c *checker) refersTo(n ast.Node, obj types.Object) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Ident:
			found = found || c.info.Uses[x] == obj
		case *ast.SelectorExpr:
			found = found || c.info.Uses[x.Sel] == obj
		}
		return !found
	})
	return found
}

// checkGoStmt applies the WaitGroup rules to a goroutine started from a func
// literal.
func (c *checker) checkGoStmt(g *ast.GoStmt) {
	fl, ok := g.Call.Fun.(*ast.FuncLit)
	if !ok {
		return
	}
	var dones []*ast.CallExpr
	hasReturn, deferredDone := false, false
	ast.Inspect(fl.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			hasReturn = true
		case *ast.DeferStmt:
			if c.isWaitGroupCall(n.Call, "Done") {
				deferredDone = true
				return false
			}
		case *ast.CallExpr:
			if c.isWaitGroupCall(n, "Add") {
				c.report("go.waitgroup-add-in-goroutine", n.Pos())
			}
			if c.isWaitGroupCall(n, "Done") {
				dones = append(dones, n)
			}
		}
		return true
	})
	if hasReturn && !deferredDone {
		for _, d := range dones {
			c.report("go.waitgroup-done-not-deferred", d.Pos())
		}
	}
}

// checkWaitGroupAdd flags wg.Add(n) with a non-constant n when a later loop in the
// same block launches goroutines that use the same WaitGroup but can exit or skip
// before launching them all.
func (c *checker) checkWaitGroupAdd(block *ast.BlockStmt) {
	for i, stmt := range block.List {
		es, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			continue
		}
		wg := c.waitGroupOf(call, "Add")
		if wg == nil {
			continue
		}
		if _, constant := call.Args[0].(*ast.BasicLit); constant {
			continue
		}
		for _, later := range block.List[i+1:] {
			var body *ast.BlockStmt
			switch loop := later.(type) {
			case *ast.ForStmt:
				body = loop.Body
			case *ast.RangeStmt:
				body = loop.Body
			}
			if body != nil && c.launchesWith(body, wg) && exitsEarly(body.List, true, true) {
				c.report("go.waitgroup-add-before-conditional-launch", call.Pos())
				break
			}
		}
	}
}

// launchesWith reports whether a loop body starts a goroutine that uses wg,
// either passed as an argument or referenced inside a func literal.
func (c *checker) launchesWith(body *ast.BlockStmt, wg types.Object) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch g := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.GoStmt:
			found = found || c.refersTo(g.Call, wg)
			return false
		}
		return !found
	})
	return found
}

// exitsEarly reports whether statements can leave or skip the rest of the
// enclosing loop iteration. ownBreak and ownContinue say whether an unlabeled
// break or continue at this level applies to that loop.
func exitsEarly(stmts []ast.Stmt, ownBreak, ownContinue bool) bool {
	for _, s := range stmts {
		if exitStmt(s, ownBreak, ownContinue) {
			return true
		}
	}
	return false
}

func exitStmt(s ast.Stmt, ownBreak, ownContinue bool) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		switch s.Tok {
		case token.BREAK:
			return ownBreak || s.Label != nil
		case token.CONTINUE:
			return ownContinue || s.Label != nil
		case token.GOTO:
			return true
		}
	case *ast.BlockStmt:
		return exitsEarly(s.List, ownBreak, ownContinue)
	case *ast.IfStmt:
		if exitsEarly(s.Body.List, ownBreak, ownContinue) {
			return true
		}
		return s.Else != nil && exitStmt(s.Else, ownBreak, ownContinue)
	case *ast.LabeledStmt:
		return exitStmt(s.Stmt, ownBreak, ownContinue)
	case *ast.SwitchStmt:
		return clausesExit(s.Body, ownContinue)
	case *ast.TypeSwitchStmt:
		return clausesExit(s.Body, ownContinue)
	case *ast.SelectStmt:
		return clausesExit(s.Body, ownContinue)
	case *ast.ForStmt:
		return exitsEarly(s.Body.List, false, false)
	case *ast.RangeStmt:
		return exitsEarly(s.Body.List, false, false)
	}
	return false
}

// clausesExit checks switch and select clauses, where an unlabeled break ends the
// switch rather than the loop but continue still applies to the loop.
func clausesExit(body *ast.BlockStmt, ownContinue bool) bool {
	for _, clause := range body.List {
		switch cl := clause.(type) {
		case *ast.CaseClause:
			if exitsEarly(cl.Body, false, ownContinue) {
				return true
			}
		case *ast.CommClause:
			if exitsEarly(cl.Body, false, ownContinue) {
				return true
			}
		}
	}
	return false
}

// checkSendLeak flags returns that happen after a goroutine is started with an
// unbuffered channel it sends on, but before the function first receives from it.
func (c *checker) checkSendLeak(body *ast.BlockStmt) {
	var channels []types.Object
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for i, rhs := range n.Rhs {
					if i >= len(n.Lhs) {
						break
					}
					if id, ok := n.Lhs[i].(*ast.Ident); ok && isUnbufferedMake(rhs) && c.info.Defs[id] != nil {
						channels = append(channels, c.info.Defs[id])
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if i < len(n.Values) && isUnbufferedMake(n.Values[i]) && c.info.Defs[name] != nil {
					channels = append(channels, c.info.Defs[name])
				}
			}
		}
		return true
	})
	for _, ch := range channels {
		goPos, firstRecv := token.NoPos, token.NoPos
		earliest := func(cur *token.Pos, p token.Pos) {
			if *cur == token.NoPos || p < *cur {
				*cur = p
			}
		}
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.GoStmt:
				if c.goSendsOn(n, ch) {
					earliest(&goPos, n.Pos())
				}
				return false
			case *ast.FuncLit:
				return false
			case *ast.UnaryExpr:
				if n.Op == token.ARROW && c.isIdentOf(n.X, ch) {
					earliest(&firstRecv, n.Pos())
				}
			case *ast.RangeStmt:
				if c.isIdentOf(n.X, ch) {
					earliest(&firstRecv, n.Pos())
				}
			}
			return true
		})
		if goPos == token.NoPos || firstRecv == token.NoPos {
			continue
		}
		ast.Inspect(body, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit, *ast.GoStmt:
				return false
			case *ast.ReturnStmt:
				// A return whose results contain the receive (return <-ch) ends after it.
				if n.Pos() > goPos && n.End() <= firstRecv {
					c.report("go.goroutine-send-leak", n.Pos())
				}
			}
			return true
		})
	}
}

// goSendsOn reports whether a go statement may send on ch: a func literal that
// sends on it, or a function called with it as an argument.
func (c *checker) goSendsOn(g *ast.GoStmt, ch types.Object) bool {
	if fl, ok := g.Call.Fun.(*ast.FuncLit); ok {
		sends := false
		ast.Inspect(fl.Body, func(n ast.Node) bool {
			if s, ok := n.(*ast.SendStmt); ok && c.isIdentOf(s.Chan, ch) {
				sends = true
			}
			return !sends
		})
		return sends
	}
	for _, arg := range g.Call.Args {
		if c.isIdentOf(arg, ch) {
			return true
		}
	}
	return false
}

func isUnbufferedMake(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != "make" {
		return false
	}
	_, isChan := call.Args[0].(*ast.ChanType)
	return isChan
}

func (c *checker) isIdentOf(expr ast.Expr, obj types.Object) bool {
	id, ok := expr.(*ast.Ident)
	return ok && obj != nil && (c.info.Uses[id] == obj || c.info.Defs[id] == obj)
}
