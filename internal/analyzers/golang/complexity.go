package golang

import (
	"go/ast"
	"go/token"

	"github.com/rburns629/urfluency/internal/parsing"
)

// checkComplexity reports a function whose cognitive complexity exceeds the
// threshold, following SonarSource's specification.
func (c *checker) checkComplexity(fn *ast.FuncDecl) {
	if fn.Body == nil {
		return
	}
	cc := newCognitive(fn)
	cc.block(fn.Body.List, 0)
	if cc.total > parsing.CognitiveThreshold {
		c.report("clean.cognitive-complexity", fn.Pos())
	}
}

type cognitive struct {
	name, recv string // the function and its receiver, for recursion
	total      int
}

func newCognitive(fn *ast.FuncDecl) *cognitive {
	cc := &cognitive{name: fn.Name.Name}
	if fn.Recv != nil && len(fn.Recv.List) == 1 && len(fn.Recv.List[0].Names) == 1 {
		cc.recv = fn.Recv.List[0].Names[0].Name
	}
	return cc
}

// recursive reports a direct call to the function itself, as f(...) for a
// function or recv.f(...) for a method.
func (cc *cognitive) recursive(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return cc.recv == "" && fn.Name == cc.name
	case *ast.SelectorExpr:
		x, ok := fn.X.(*ast.Ident)
		return ok && cc.recv != "" && x.Name == cc.recv && fn.Sel.Name == cc.name
	}
	return false
}

func (cc *cognitive) block(stmts []ast.Stmt, nesting int) {
	for _, s := range stmts {
		cc.stmt(s, nesting)
	}
}

func (cc *cognitive) stmt(s ast.Stmt, nesting int) {
	switch s := s.(type) {
	case *ast.IfStmt:
		cc.total += 1 + nesting
		cc.ifChain(s, nesting)
	case *ast.ForStmt:
		cc.total += 1 + nesting
		cc.expr(s.Cond)
		cc.block(s.Body.List, nesting+1)
	case *ast.RangeStmt:
		cc.total += 1 + nesting
		cc.expr(s.X)
		cc.block(s.Body.List, nesting+1)
	case *ast.SwitchStmt:
		cc.total += 1 + nesting
		cc.expr(s.Tag)
		cc.clauses(s.Body, nesting+1)
	case *ast.TypeSwitchStmt:
		cc.total += 1 + nesting
		cc.clauses(s.Body, nesting+1)
	case *ast.SelectStmt:
		cc.total += 1 + nesting
		cc.clauses(s.Body, nesting+1)
	case *ast.BranchStmt:
		if s.Label != nil || s.Tok == token.GOTO {
			cc.total++
		}
	case *ast.LabeledStmt:
		cc.stmt(s.Stmt, nesting)
	case *ast.BlockStmt:
		cc.block(s.List, nesting)
	default:
		cc.nodeExprs(s, nesting)
	}
}

// ifChain counts the condition, body, and else branches of an if. An else if
// adds 1 without a nesting penalty, as does a final else.
func (cc *cognitive) ifChain(s *ast.IfStmt, nesting int) {
	cc.expr(s.Cond)
	cc.block(s.Body.List, nesting+1)
	switch e := s.Else.(type) {
	case *ast.IfStmt:
		cc.total++
		cc.ifChain(e, nesting)
	case *ast.BlockStmt:
		cc.total++
		cc.block(e.List, nesting+1)
	}
}

func (cc *cognitive) clauses(body *ast.BlockStmt, nesting int) {
	for _, s := range body.List {
		switch cl := s.(type) {
		case *ast.CaseClause:
			for _, e := range cl.List {
				cc.expr(e)
			}
			cc.block(cl.Body, nesting)
		case *ast.CommClause:
			cc.block(cl.Body, nesting)
		}
	}
}

// nodeExprs counts logical operators in a simple statement and walks function
// literals, which deepen the nesting.
func (cc *cognitive) nodeExprs(n ast.Node, nesting int) {
	ast.Inspect(n, func(x ast.Node) bool {
		switch x := x.(type) {
		case *ast.FuncLit:
			cc.block(x.Body.List, nesting+1)
			return false
		case *ast.CallExpr:
			if cc.recursive(x) {
				cc.total++
			}
		case *ast.BinaryExpr:
			cc.logical(x, token.ILLEGAL)
			return false
		}
		return true
	})
}

func (cc *cognitive) expr(e ast.Expr) {
	if e != nil {
		cc.nodeExprs(e, 0)
	}
}

// logical adds 1 for each sequence of like && or || operators: a && b && c is
// one sequence, a && b || c is two.
func (cc *cognitive) logical(e ast.Expr, parentOp token.Token) {
	bin, ok := ast.Unparen(e).(*ast.BinaryExpr)
	if !ok {
		cc.nodeExprs(e, 0)
		return
	}
	if bin.Op != token.LAND && bin.Op != token.LOR {
		cc.nodeExprs(bin.X, 0)
		cc.nodeExprs(bin.Y, 0)
		return
	}
	if bin.Op != parentOp {
		cc.total++
	}
	cc.logical(bin.X, bin.Op)
	cc.logical(bin.Y, bin.Op)
}
