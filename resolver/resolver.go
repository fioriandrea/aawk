package resolver

import (
	"fmt"
	"os"
	"regexp"
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

func ResolveVariables(items []parser.Item, builtinFunctions []string) (map[string]int, map[string]int, error) {
	resolver := newResolver()

	for _, builtin := range builtinFunctions {
		resolver.functionindices[builtin] = len(resolver.functionindices)
	}

	for _, item := range items {
		switch it := item.(type) {
		case *parser.FunctionDef:
			if _, ok := resolver.functionindices[it.Name.Lexeme]; ok {
				return nil, nil, resolver.resolveError(it.Name, "function already defined")
			}
			if _, ok := lexer.Builtinvars[it.Name.Lexeme]; ok {
				return nil, nil, resolver.resolveError(it.Name, "cannot declare built-in variable as function")
			}
			resolver.functionindices[it.Name.Lexeme] = len(resolver.functionindices)
		}
	}

	err := resolver.items(items)
	return resolver.indices, resolver.functionindices, err
}

func (res *resolver) items(items []parser.Item) error {
	var err error

	for _, item := range items {
		switch it := item.(type) {
		case *parser.FunctionDef:
			err = res.functionDef(it)
			if err != nil {
				return err
			}
		case *parser.PatternAction:
			err = res.patternAction(it)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (res *resolver) functionDef(fd *parser.FunctionDef) error {
	var err error
	res.localindices = map[string]int{}
	defer func() { res.localindices = nil }()
	for i, arg := range fd.Args {
		arg := arg
		res.localindices[arg.Lexeme] = i
	}

	err = res.blockStat(fd.Body)
	return err
}

func (res *resolver) patternAction(pa *parser.PatternAction) error {
	var err error
	switch patt := pa.Pattern.(type) {
	case *parser.ExprPattern:
		err = res.exprPattern(patt)
		if err != nil {
			return err
		}
	case *parser.RangePattern:
		err = res.rangePattern(patt)
		if err != nil {
			return err
		}
	}
	err = res.blockStat(pa.Action)
	return err
}

func (res *resolver) exprPattern(ep *parser.ExprPattern) error {
	err := res.expr(ep.Expr)
	return err
}

func (res *resolver) rangePattern(rp *parser.RangePattern) error {
	var err error
	err = res.expr(rp.Expr0)
	if err != nil {
		return err
	}
	err = res.expr(rp.Expr1)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) blockStat(bs parser.BlockStat) error {
	var err error
	for i := 0; i < len(bs); i++ {
		err = res.stat(bs[i])
		if err != nil {
			return err
		}
	}
	return nil
}

func (res *resolver) stat(s parser.Stat) error {
	switch ss := s.(type) {
	case *parser.IfStat:
		return res.ifStat(ss)
	case *parser.ForStat:
		return res.forStat(ss)
	case *parser.ForEachStat:
		return res.forEachStat(ss)
	case parser.BlockStat:
		return res.blockStat(ss)
	case *parser.ReturnStat:
		return res.returnStat(ss)
	case *parser.PrintStat:
		return res.printStat(ss)
	case *parser.ExprStat:
		return res.exprStat(ss)
	case *parser.ExitStat:
		return res.exitStat(ss)
	case *parser.DeleteStat:
		return res.deleteStat(ss)
	}
	return nil
}
func (res *resolver) ifStat(is *parser.IfStat) error {
	var err error
	err = res.expr(is.Cond)
	if err != nil {
		return err
	}
	err = res.stat(is.Body)
	if err != nil {
		return err
	}
	err = res.stat(is.ElseBody)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) forStat(fs *parser.ForStat) error {
	var err error
	err = res.stat(fs.Init)
	if err != nil {
		return err
	}
	err = res.expr(fs.Cond)
	if err != nil {
		return err
	}
	err = res.stat(fs.Inc)
	if err != nil {
		return err
	}
	err = res.stat(fs.Body)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) forEachStat(fe *parser.ForEachStat) error {
	var err error
	err = res.idExpr(fe.Id)
	if err != nil {
		return err
	}
	err = res.idExpr(fe.Array)
	if err != nil {
		return err
	}
	err = res.stat(fe.Body)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) returnStat(rs *parser.ReturnStat) error {
	return res.expr(rs.ReturnVal)
}

func (res *resolver) printStat(ps *parser.PrintStat) error {
	var err error
	err = res.exprs(ps.Exprs)
	if err != nil {
		return err
	}
	err = res.expr(ps.File)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) exprStat(es *parser.ExprStat) error {
	return res.expr(es.Expr)
}

func (res *resolver) exitStat(ex *parser.ExitStat) error {
	err := res.expr(ex.Status)
	return err
}

func (res *resolver) deleteStat(ds *parser.DeleteStat) error {
	err := res.lhsExpr(ds.Lhs)
	return err
}

func (res *resolver) expr(ex parser.Expr) error {
	switch e := ex.(type) {
	case *parser.BinaryExpr:
		return res.binaryExpr(e)
	case *parser.BinaryBoolExpr:
		return res.binaryBoolExpr(e)
	case *parser.UnaryExpr:
		return res.unaryExpr(e)
	case *parser.MatchExpr:
		return res.matchExpr(e)
	case *parser.AssignExpr:
		return res.assignExpr(e)
	case *parser.IdExpr:
		return res.idExpr(e)
	case *parser.IndexingExpr:
		return res.indexingExpr(e)
	case *parser.DollarExpr:
		return res.dollarExpr(e)
	case *parser.IncrementExpr:
		return res.incrementExpr(e)
	case *parser.PreIncrementExpr:
		return res.preIncrementExpr(e)
	case *parser.PostIncrementExpr:
		return res.postIncrementExpr(e)
	case *parser.TernaryExpr:
		return res.ternaryExpr(e)
	case *parser.GetlineExpr:
		return res.getlineExpr(e)
	case *parser.CallExpr:
		return res.callExpr(e)
	case *parser.InExpr:
		return res.inExpr(e)
	case parser.ExprList:
		return res.exprList(e)
	case *parser.NumberExpr:
		return res.numberExpr(e)
	case *parser.LengthExpr:
		return res.lengthExpr(e)
	case *parser.RegexExpr:
		return res.regexExpr(e)
	}
	return nil
}

func (res *resolver) binaryExpr(e *parser.BinaryExpr) error {
	var err error
	err = res.expr(e.Left)
	if err != nil {
		return err
	}
	err = res.expr(e.Right)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) binaryBoolExpr(e *parser.BinaryBoolExpr) error {
	var err error
	err = res.expr(e.Left)
	if err != nil {
		return err
	}
	err = res.expr(e.Right)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) unaryExpr(e *parser.UnaryExpr) error {
	err := res.expr(e.Right)
	return err
}

func (res *resolver) matchExpr(e *parser.MatchExpr) error {
	var err error
	err = res.expr(e.Left)
	if err != nil {
		return err
	}
	err = res.expr(e.Right)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) assignExpr(e *parser.AssignExpr) error {
	var err error
	err = res.lhsExpr(e.Left)
	if err != nil {
		return err
	}
	err = res.expr(e.Right)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) lhsExpr(e parser.LhsExpr) error {
	switch v := e.(type) {
	case *parser.DollarExpr:
		return res.dollarExpr(v)
	case *parser.IdExpr:
		return res.idExpr(v)
	case *parser.IndexingExpr:
		return res.indexingExpr(v)
	}
	return res.resolveError(e.Token(), "undefined lhs")
}

func (res *resolver) idExpr(e *parser.IdExpr) error {
	li, liok := res.localindices[e.Id.Lexeme]
	if liok {
		e.LocalIndex = li
		e.Index = -1
		e.FunctionIndex = -1
		e.BuiltinIndex = -1
		return nil
	}

	if _, ok := res.functionindices[e.Id.Lexeme]; ok {
		return res.resolveError(e.Token(), "cannot use function in variable context")
	}

	if i, ok := lexer.Builtinvars[e.Id.Lexeme]; ok {
		e.LocalIndex = -1
		e.Index = -1
		e.FunctionIndex = -1
		e.BuiltinIndex = i
		return nil
	}
	i, iok := res.indices[e.Id.Lexeme]
	if iok {
		e.LocalIndex = -1
		e.Index = i
		e.FunctionIndex = -1
		e.BuiltinIndex = -1
		return nil
	}
	e.Index = len(res.indices)
	e.LocalIndex = -1
	e.FunctionIndex = -1
	e.BuiltinIndex = -1
	res.indices[e.Id.Lexeme] = e.Index
	return nil
}

func (res *resolver) indexingExpr(e *parser.IndexingExpr) error {
	var err error
	err = res.idExpr(e.Id)
	if err != nil {
		return err
	}
	err = res.exprs(e.Index)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) dollarExpr(e *parser.DollarExpr) error {
	err := res.expr(e.Field)
	return err
}

func (res *resolver) incrementExpr(e *parser.IncrementExpr) error {
	err := res.lhsExpr(e.Lhs)
	return err
}

func (res *resolver) preIncrementExpr(e *parser.PreIncrementExpr) error {
	return res.incrementExpr(e.IncrementExpr)
}

func (res *resolver) postIncrementExpr(e *parser.PostIncrementExpr) error {
	return res.incrementExpr(e.IncrementExpr)
}

func (res *resolver) ternaryExpr(e *parser.TernaryExpr) error {
	var err error
	err = res.expr(e.Cond)
	if err != nil {
		return err
	}
	err = res.expr(e.Expr0)
	if err != nil {
		return err
	}
	err = res.expr(e.Expr1)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) getlineExpr(e *parser.GetlineExpr) error {
	var err error
	err = res.lhsExpr(e.Variable)
	if err != nil {
		return err
	}
	err = res.expr(e.File)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) callExpr(e *parser.CallExpr) error {
	var err error
	if i, ok := res.functionindices[e.Called.Id.Lexeme]; ok {
		e.Called.FunctionIndex = i
	} else {
		return res.resolveError(e.Token(), "cannot call non-callable")
	}
	e.Called.Index = -1
	e.Called.LocalIndex = -1
	err = res.exprs(e.Args)
	return err
}

func (res *resolver) inExpr(e *parser.InExpr) error {
	var err error
	err = res.expr(e.Left)
	if err != nil {
		return err
	}
	err = res.idExpr(e.Right)
	if err != nil {
		return err
	}
	return nil
}

func (res *resolver) exprList(e parser.ExprList) error {
	return res.exprs(e)
}

func (res *resolver) numberExpr(e *parser.NumberExpr) error {
	v, _ := strconv.ParseFloat(e.Num.Lexeme, 64)
	e.NumVal = v
	return nil
}

func (res *resolver) lengthExpr(e *parser.LengthExpr) error {
	return res.expr(e.Arg)
}

func (res *resolver) regexExpr(e *parser.RegexExpr) error {
	c := regexp.MustCompile(e.Regex.Lexeme)
	e.Compiled = c
	return nil
}

func (res *resolver) exprs(es []parser.Expr) error {
	var err error
	for i := 0; i < len(es); i++ {
		err = res.expr(es[i])
		if err != nil {
			return err
		}
	}
	return nil
}

func (res *resolver) resolveError(tok lexer.Token, msg string) error {
	return fmt.Errorf("%s: at %d (%s): %s", os.Args[0], tok.Line, tok.Lexeme, msg)
}
