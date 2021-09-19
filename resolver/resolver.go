package resolver

import (
	"fmt"
	"os"
	"strconv"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

type resolver struct {
	indices         map[string]int
	localindices    map[string]int
	functionindices map[string]int
}

func newResolver() *resolver {
	return &resolver{
		indices:         map[string]int{},
		functionindices: map[string]int{},
	}
}

func ResolveVariables(items []parser.Item, builtinFunctions []string) ([]parser.Item, map[string]int, map[string]int, error) {
	resolver := newResolver()

	for _, builtin := range builtinFunctions {
		resolver.functionindices[builtin] = len(resolver.functionindices)
	}

	for _, item := range items {
		switch it := item.(type) {
		case parser.FunctionDef:
			if _, ok := resolver.functionindices[it.Name.Lexeme]; ok {
				return nil, nil, nil, resolver.resolveError(it.Name, "function already defined")
			}
			if _, ok := lexer.Builtinvars[it.Name.Lexeme]; ok {
				return nil, nil, nil, resolver.resolveError(it.Name, "cannot declare built-in variable as function")
			}
			resolver.functionindices[it.Name.Lexeme] = len(resolver.functionindices)
		}
	}

	var err error
	items, err = resolver.items(items)
	return items, resolver.indices, resolver.functionindices, err
}

func (res *resolver) items(items []parser.Item) ([]parser.Item, error) {
	var err error

	for i, item := range items {
		switch it := item.(type) {
		case parser.FunctionDef:
			items[i], err = res.functionDef(it)
			if err != nil {
				return nil, err
			}
		case parser.PatternAction:
			items[i], err = res.patternAction(it)
			if err != nil {
				return nil, err
			}
		}
	}
	return items, nil
}

func (res *resolver) functionDef(fd parser.FunctionDef) (parser.FunctionDef, error) {
	var err error
	res.localindices = map[string]int{}
	defer func() { res.localindices = nil }()
	for i, arg := range fd.Args {
		arg := arg
		res.localindices[arg.Lexeme] = i
	}

	fd.Body, err = res.blockStat(fd.Body)
	return fd, err
}

func (res *resolver) patternAction(pa parser.PatternAction) (parser.PatternAction, error) {
	var err error
	switch patt := pa.Pattern.(type) {
	case parser.ExprPattern:
		pa.Pattern, err = res.exprPattern(patt)
		if err != nil {
			return pa, err
		}
	case parser.RangePattern:
		pa.Pattern, err = res.rangePattern(patt)
		if err != nil {
			return pa, err
		}
	}
	pa.Action, err = res.blockStat(pa.Action)
	return pa, err
}

func (res *resolver) exprPattern(ep parser.ExprPattern) (parser.ExprPattern, error) {
	var err error
	ep.Expr, err = res.expr(ep.Expr)
	return ep, err
}

func (res *resolver) rangePattern(rp parser.RangePattern) (parser.RangePattern, error) {
	var err error
	rp.Expr0, err = res.expr(rp.Expr0)
	if err != nil {
		return rp, err
	}
	rp.Expr1, err = res.expr(rp.Expr1)
	if err != nil {
		return rp, err
	}
	return rp, nil
}

func (res *resolver) blockStat(bs parser.BlockStat) (parser.BlockStat, error) {
	var err error
	for i := 0; i < len(bs); i++ {
		bs[i], err = res.stat(bs[i])
		if err != nil {
			return bs, err
		}
	}
	return bs, nil
}

func (res *resolver) stat(s parser.Stat) (parser.Stat, error) {
	switch ss := s.(type) {
	case parser.IfStat:
		return res.ifStat(ss)
	case parser.ForStat:
		return res.forStat(ss)
	case parser.ForEachStat:
		return res.forEachStat(ss)
	case parser.BlockStat:
		return res.blockStat(ss)
	case parser.ReturnStat:
		return res.returnStat(ss)
	case parser.PrintStat:
		return res.printStat(ss)
	case parser.ExprStat:
		return res.exprStat(ss)
	case parser.ExitStat:
		return res.exitStat(ss)
	case parser.DeleteStat:
		return res.deleteStat(ss)
	}
	return s, nil
}
func (res *resolver) ifStat(is parser.IfStat) (parser.IfStat, error) {
	var err error
	is.Cond, err = res.expr(is.Cond)
	if err != nil {
		return is, err
	}
	is.Body, err = res.stat(is.Body)
	if err != nil {
		return is, err
	}
	is.ElseBody, err = res.stat(is.ElseBody)
	if err != nil {
		return is, err
	}
	return is, nil
}

func (res *resolver) forStat(fs parser.ForStat) (parser.ForStat, error) {
	var err error
	fs.Init, err = res.stat(fs.Init)
	if err != nil {
		return fs, err
	}
	fs.Cond, err = res.expr(fs.Cond)
	if err != nil {
		return fs, err
	}
	fs.Inc, err = res.stat(fs.Inc)
	if err != nil {
		return fs, err
	}
	fs.Body, err = res.stat(fs.Body)
	if err != nil {
		return fs, err
	}
	return fs, nil
}

func (res *resolver) forEachStat(fe parser.ForEachStat) (parser.ForEachStat, error) {
	var err error
	fe.Id, err = res.idExpr(fe.Id)
	if err != nil {
		return fe, err
	}
	fe.Array, err = res.idExpr(fe.Array)
	if err != nil {
		return fe, err
	}
	fe.Body, err = res.stat(fe.Body)
	if err != nil {
		return fe, err
	}
	return fe, nil
}

func (res *resolver) returnStat(rs parser.ReturnStat) (parser.ReturnStat, error) {
	var err error
	rs.ReturnVal, err = res.expr(rs.ReturnVal)
	return rs, err
}

func (res *resolver) printStat(ps parser.PrintStat) (parser.PrintStat, error) {
	var err error
	ps.Exprs, err = res.exprs(ps.Exprs)
	if err != nil {
		return ps, err
	}
	ps.File, err = res.expr(ps.File)
	if err != nil {
		return ps, err
	}
	return ps, nil
}

func (res *resolver) exprStat(es parser.ExprStat) (parser.ExprStat, error) {
	var err error
	es.Expr, err = res.expr(es.Expr)
	return es, err
}

func (res *resolver) exitStat(ex parser.ExitStat) (parser.ExitStat, error) {
	var err error
	ex.Status, err = res.expr(ex.Status)
	return ex, err
}

func (res *resolver) deleteStat(ds parser.DeleteStat) (parser.DeleteStat, error) {
	var err error
	ds.Lhs, err = res.lhsExpr(ds.Lhs)
	return ds, err
}

func (res *resolver) expr(ex parser.Expr) (parser.Expr, error) {
	switch e := ex.(type) {
	case parser.BinaryExpr:
		return res.binaryExpr(e)
	case parser.BinaryBoolExpr:
		return res.binaryBoolExpr(e)
	case parser.UnaryExpr:
		return res.unaryExpr(e)
	case parser.MatchExpr:
		return res.matchExpr(e)
	case parser.AssignExpr:
		return res.assignExpr(e)
	case parser.IdExpr:
		return res.idExpr(e)
	case parser.IndexingExpr:
		return res.indexingExpr(e)
	case parser.DollarExpr:
		return res.dollarExpr(e)
	case parser.IncrementExpr:
		return res.incrementExpr(e)
	case parser.PreIncrementExpr:
		return res.preIncrementExpr(e)
	case parser.PostIncrementExpr:
		return res.postIncrementExpr(e)
	case parser.TernaryExpr:
		return res.ternaryExpr(e)
	case parser.GetlineExpr:
		return res.getlineExpr(e)
	case parser.CallExpr:
		return res.callExpr(e)
	case parser.InExpr:
		return res.inExpr(e)
	case parser.ExprList:
		return res.exprList(e)
	case parser.NumberExpr:
		return res.numberExpr(e)
	}
	return ex, nil
}

func (res *resolver) binaryExpr(e parser.BinaryExpr) (parser.BinaryExpr, error) {
	var err error
	e.Left, err = res.expr(e.Left)
	if err != nil {
		return e, err
	}
	e.Right, err = res.expr(e.Right)
	if err != nil {
		return e, err
	}
	return e, nil
}

func (res *resolver) binaryBoolExpr(e parser.BinaryBoolExpr) (parser.BinaryBoolExpr, error) {
	var err error
	e.Left, err = res.expr(e.Left)
	if err != nil {
		return e, err
	}
	e.Right, err = res.expr(e.Right)
	if err != nil {
		return e, err
	}
	return e, nil
}

func (res *resolver) unaryExpr(e parser.UnaryExpr) (parser.UnaryExpr, error) {
	var err error
	e.Right, err = res.expr(e.Right)
	return e, err
}

func (res *resolver) matchExpr(e parser.MatchExpr) (parser.MatchExpr, error) {
	var err error
	e.Left, err = res.expr(e.Left)
	if err != nil {
		return e, err
	}
	e.Right, err = res.expr(e.Right)
	if err != nil {
		return e, err
	}
	return e, nil
}

func (res *resolver) assignExpr(e parser.AssignExpr) (parser.AssignExpr, error) {
	var err error
	e.Left, err = res.lhsExpr(e.Left)
	if err != nil {
		return e, err
	}
	e.Right, err = res.expr(e.Right)
	if err != nil {
		return e, err
	}
	return e, nil
}

func (res *resolver) lhsExpr(e parser.LhsExpr) (parser.LhsExpr, error) {
	switch v := e.(type) {
	case parser.DollarExpr:
		return res.dollarExpr(v)
	case parser.IdExpr:
		return res.idExpr(v)
	case parser.IndexingExpr:
		return res.indexingExpr(v)
	}
	return nil, res.resolveError(e.Token(), "undefined lhs")
}

func (res *resolver) idExpr(e parser.IdExpr) (parser.IdExpr, error) {
	li, liok := res.localindices[e.Id.Lexeme]
	if liok {
		e.LocalIndex = li
		e.Index = -1
		e.FunctionIndex = -1
		return e, nil
	}

	if _, ok := res.functionindices[e.Id.Lexeme]; ok {
		return e, res.resolveError(e.Token(), "cannot use function in variable context")
	}

	if _, ok := lexer.Builtinvars[e.Id.Lexeme]; ok {
		e.LocalIndex = -1
		e.Index = -1
		e.FunctionIndex = -1
		return e, nil
	}
	i, iok := res.indices[e.Id.Lexeme]
	if iok {
		e.LocalIndex = -1
		e.Index = i
		e.FunctionIndex = -1
		return e, nil
	}
	e.Index = len(res.indices)
	res.indices[e.Id.Lexeme] = e.Index
	return e, nil
}

func (res *resolver) indexingExpr(e parser.IndexingExpr) (parser.IndexingExpr, error) {
	var err error
	e.Id, err = res.idExpr(e.Id)
	if err != nil {
		return e, err
	}
	e.Index, err = res.exprs(e.Index)
	if err != nil {
		return e, err
	}
	return e, nil
}

func (res *resolver) dollarExpr(e parser.DollarExpr) (parser.DollarExpr, error) {
	var err error
	e.Field, err = res.expr(e.Field)
	return e, err
}

func (res *resolver) incrementExpr(e parser.IncrementExpr) (parser.IncrementExpr, error) {
	var err error
	e.Lhs, err = res.lhsExpr(e.Lhs)
	return e, err
}

func (res *resolver) preIncrementExpr(e parser.PreIncrementExpr) (parser.PreIncrementExpr, error) {
	var err error
	e.Lhs, err = res.lhsExpr(e.Lhs)
	return e, err
}

func (res *resolver) postIncrementExpr(e parser.PostIncrementExpr) (parser.PostIncrementExpr, error) {
	var err error
	e.Lhs, err = res.lhsExpr(e.Lhs)
	if err != nil {
		return e, err
	}
	return e, err
}

func (res *resolver) ternaryExpr(e parser.TernaryExpr) (parser.TernaryExpr, error) {
	var err error
	e.Cond, err = res.expr(e.Cond)
	if err != nil {
		return e, err
	}
	e.Expr0, err = res.expr(e.Expr0)
	if err != nil {
		return e, err
	}
	e.Expr1, err = res.expr(e.Expr1)
	if err != nil {
		return e, err
	}
	return e, nil
}

func (res *resolver) getlineExpr(e parser.GetlineExpr) (parser.GetlineExpr, error) {
	var err error
	e.Variable, err = res.lhsExpr(e.Variable)
	if err != nil {
		return e, err
	}
	e.File, err = res.expr(e.File)
	if err != nil {
		return e, err
	}
	return e, nil
}

func (res *resolver) callExpr(e parser.CallExpr) (parser.CallExpr, error) {
	var err error
	if i, ok := res.functionindices[e.Called.Id.Lexeme]; ok {
		e.Called.FunctionIndex = i
	} else {
		return e, res.resolveError(e.Token(), "cannot call non-callable")
	}
	e.Called.Index = -1
	e.Called.LocalIndex = -1
	e.Args, err = res.exprs(e.Args)
	return e, err
}

func (res *resolver) inExpr(e parser.InExpr) (parser.InExpr, error) {
	var err error
	e.Left, err = res.expr(e.Left)
	if err != nil {
		return e, err
	}
	e.Right, err = res.idExpr(e.Right)
	if err != nil {
		return e, err
	}
	return e, nil
}

func (res *resolver) exprList(e parser.ExprList) (parser.ExprList, error) {
	var err error
	var exprs []parser.Expr
	exprs, err = res.exprs(e)
	if err != nil {
		return parser.ExprList{}, err
	}
	e = parser.ExprList(exprs)
	return e, nil
}

func (res *resolver) numberExpr(e parser.NumberExpr) (parser.NumberExpr, error) {
	v, _ := strconv.ParseFloat(e.Num.Lexeme, 64)
	e.NumVal = v
	return e, nil
}

func (res *resolver) exprs(es []parser.Expr) ([]parser.Expr, error) {
	var err error
	for i := 0; i < len(es); i++ {
		es[i], err = res.expr(es[i])
		if err != nil {
			return es, err
		}
	}
	return es, nil
}

func (res *resolver) resolveError(tok lexer.Token, msg string) error {
	return fmt.Errorf("%s: at %d (%s): %s", os.Args[0], tok.Line, tok.Lexeme, msg)
}
