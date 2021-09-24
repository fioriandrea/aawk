/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package parser

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/fioriandrea/aawk/lexer"
)

const (
	Argc = iota
	Argv
	Convfmt
	Environ
	Filename
	Fnr
	Fs
	Nf
	Nr
	Ofmt
	Ofs
	Ors
	Rlength
	Rs
	Rstart
	Subsep
)

var Builtinvars = map[string]int{
	"ARGC":     Argc,
	"ARGV":     Argv,
	"CONVFMT":  Convfmt,
	"ENVIRON":  Environ,
	"FILENAME": Filename,
	"FNR":      Fnr,
	"FS":       Fs,
	"NF":       Nf,
	"NR":       Nr,
	"OFMT":     Ofmt,
	"OFS":      Ofs,
	"ORS":      Ors,
	"RLENGTH":  Rlength,
	"RS":       Rs,
	"RSTART":   Rstart,
	"SUBSEP":   Subsep,
}

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

// Take functions defined somewhere else (that is, not in the source code)
func resolve(items []Item, nativeFunctions map[string]interface{}) (map[string]int, map[string]int, error) {
	resolver := newResolver()

	for builtin := range nativeFunctions {
		resolver.functionindices[builtin] = len(resolver.functionindices)
	}

	for _, item := range items {
		switch it := item.(type) {
		case *FunctionDef:
			if _, ok := resolver.functionindices[it.Name.Lexeme]; ok {
				return nil, nil, resolver.resolveError(it.Name, "function already defined")
			}
			resolver.functionindices[it.Name.Lexeme] = len(resolver.functionindices)
		}
	}

	err := resolver.items(items)
	return resolver.indices, resolver.functionindices, err
}

func (res *resolver) items(items []Item) error {
	var err error

	for _, item := range items {
		switch it := item.(type) {
		case *FunctionDef:
			err = res.functionDef(it)
			if err != nil {
				return err
			}
		case *PatternAction:
			err = res.patternAction(it)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (res *resolver) functionDef(fd *FunctionDef) error {
	var err error
	res.localindices = map[string]int{}
	defer func() { res.localindices = nil }()
	for i, arg := range fd.Args {
		if _, ok := Builtinvars[arg.Lexeme]; ok {
			return res.resolveError(arg, "cannot call a function argument the same as a built-in variable")
		}
		res.localindices[arg.Lexeme] = i
	}

	err = res.blockStat(fd.Body)
	return err
}

func (res *resolver) patternAction(pa *PatternAction) error {
	var err error
	switch patt := pa.Pattern.(type) {
	case *ExprPattern:
		err = res.exprPattern(patt)
		if err != nil {
			return err
		}
	case *RangePattern:
		err = res.rangePattern(patt)
		if err != nil {
			return err
		}
	}
	err = res.blockStat(pa.Action)
	return err
}

func (res *resolver) exprPattern(ep *ExprPattern) error {
	err := res.expr(ep.Expr)
	return err
}

func (res *resolver) rangePattern(rp *RangePattern) error {
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

func (res *resolver) blockStat(bs BlockStat) error {
	var err error
	for i := 0; i < len(bs); i++ {
		err = res.stat(bs[i])
		if err != nil {
			return err
		}
	}
	return nil
}

func (res *resolver) stat(s Stat) error {
	switch ss := s.(type) {
	case *IfStat:
		return res.ifStat(ss)
	case *ForStat:
		return res.forStat(ss)
	case *ForEachStat:
		return res.forEachStat(ss)
	case BlockStat:
		return res.blockStat(ss)
	case *ReturnStat:
		return res.returnStat(ss)
	case *PrintStat:
		return res.printStat(ss)
	case *ExprStat:
		return res.exprStat(ss)
	case *ExitStat:
		return res.exitStat(ss)
	case *DeleteStat:
		return res.deleteStat(ss)
	}
	return nil
}
func (res *resolver) ifStat(is *IfStat) error {
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

func (res *resolver) forStat(fs *ForStat) error {
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

func (res *resolver) forEachStat(fe *ForEachStat) error {
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

func (res *resolver) returnStat(rs *ReturnStat) error {
	return res.expr(rs.ReturnVal)
}

func (res *resolver) printStat(ps *PrintStat) error {
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

func (res *resolver) exprStat(es *ExprStat) error {
	return res.expr(es.Expr)
}

func (res *resolver) exitStat(ex *ExitStat) error {
	err := res.expr(ex.Status)
	return err
}

func (res *resolver) deleteStat(ds *DeleteStat) error {
	err := res.lhsExpr(ds.Lhs)
	return err
}

func (res *resolver) expr(ex Expr) error {
	switch e := ex.(type) {
	case *BinaryExpr:
		return res.binaryExpr(e)
	case *BinaryBoolExpr:
		return res.binaryBoolExpr(e)
	case *UnaryExpr:
		return res.unaryExpr(e)
	case *MatchExpr:
		return res.matchExpr(e)
	case *AssignExpr:
		return res.assignExpr(e)
	case *IdExpr:
		return res.idExpr(e)
	case *IndexingExpr:
		return res.indexingExpr(e)
	case *DollarExpr:
		return res.dollarExpr(e)
	case *IncrementExpr:
		return res.incrementExpr(e)
	case *PreIncrementExpr:
		return res.preIncrementExpr(e)
	case *PostIncrementExpr:
		return res.postIncrementExpr(e)
	case *TernaryExpr:
		return res.ternaryExpr(e)
	case *GetlineExpr:
		return res.getlineExpr(e)
	case *CallExpr:
		return res.callExpr(e)
	case *InExpr:
		return res.inExpr(e)
	case ExprList:
		return res.exprList(e)
	case *NumberExpr:
		return res.numberExpr(e)
	case *LengthExpr:
		return res.lengthExpr(e)
	case *RegexExpr:
		return res.regexExpr(e)
	}
	return nil
}

func (res *resolver) binaryExpr(e *BinaryExpr) error {
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

func (res *resolver) binaryBoolExpr(e *BinaryBoolExpr) error {
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

func (res *resolver) unaryExpr(e *UnaryExpr) error {
	err := res.expr(e.Right)
	return err
}

func (res *resolver) matchExpr(e *MatchExpr) error {
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

func (res *resolver) assignExpr(e *AssignExpr) error {
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

func (res *resolver) lhsExpr(e LhsExpr) error {
	switch v := e.(type) {
	case *DollarExpr:
		return res.dollarExpr(v)
	case *IdExpr:
		return res.idExpr(v)
	case *IndexingExpr:
		return res.indexingExpr(v)
	}
	return nil
}

func (res *resolver) idExpr(e *IdExpr) error {
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

	if i, ok := Builtinvars[e.Id.Lexeme]; ok {
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

func (res *resolver) indexingExpr(e *IndexingExpr) error {
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

func (res *resolver) dollarExpr(e *DollarExpr) error {
	err := res.expr(e.Field)
	return err
}

func (res *resolver) incrementExpr(e *IncrementExpr) error {
	err := res.lhsExpr(e.Lhs)
	return err
}

func (res *resolver) preIncrementExpr(e *PreIncrementExpr) error {
	return res.incrementExpr(e.IncrementExpr)
}

func (res *resolver) postIncrementExpr(e *PostIncrementExpr) error {
	return res.incrementExpr(e.IncrementExpr)
}

func (res *resolver) ternaryExpr(e *TernaryExpr) error {
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

func (res *resolver) getlineExpr(e *GetlineExpr) error {
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

func (res *resolver) callExpr(e *CallExpr) error {
	// If it is not a built-in function (i.e. if it is user defined)
	if e.Called.Id.Type == lexer.Identifier || e.Called.Id.Type == lexer.IdentifierParen {
		if i, ok := res.functionindices[e.Called.Id.Lexeme]; ok {
			e.Called.FunctionIndex = i
		} else {
			return res.resolveError(e.Token(), "cannot call non-callable")
		}
	} else {
		e.Called.FunctionIndex = -1
	}

	e.Called.Index = -1
	e.Called.LocalIndex = -1
	return res.exprs(e.Args)
}

func (res *resolver) inExpr(e *InExpr) error {
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

func (res *resolver) exprList(e ExprList) error {
	return res.exprs(e)
}

func (res *resolver) numberExpr(e *NumberExpr) error {
	v, _ := strconv.ParseFloat(e.Num.Lexeme, 64)
	e.NumVal = v
	return nil
}

func (res *resolver) lengthExpr(e *LengthExpr) error {
	return res.expr(e.Arg)
}

func (res *resolver) regexExpr(e *RegexExpr) error {
	c := regexp.MustCompile(e.Regex.Lexeme)
	e.Compiled = c
	return nil
}

func (res *resolver) exprs(es []Expr) error {
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
	return fmt.Errorf("at line %d (%s): resolve error: %s", tok.Line, tok.Lexeme, msg)
}
