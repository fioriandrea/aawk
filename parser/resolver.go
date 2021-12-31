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

func resolve(items []Item, nativeFunctions map[string]bool) (map[string]int, map[string]int, []error) {
	var errors []error

	resolver := newResolver()

	for native := range nativeFunctions {
		if _, ok := lexer.Builtinvars[native]; ok {
			errors = append(errors, fmt.Errorf("cannot call native (%s) the same as a builtin variable", native))
			continue
		} else if _, ok := lexer.Builtinfuncs[native]; ok {
			errors = append(errors, fmt.Errorf("cannot call native (%s) the same as a builtin function", native))
			continue
		} else if _, ok := lexer.Keywords[native]; ok {
			errors = append(errors, fmt.Errorf("cannot call native (%s) the same as a keyword", native))
			continue
		}
		resolver.functionindices[native] = len(resolver.functionindices)
	}

	for _, item := range items {
		switch it := item.(type) {
		case *FunctionDef:
			if _, ok := resolver.functionindices[it.Name.Lexeme]; ok {
				errors = append(errors, resolver.resolveError(it.Name, "function already defined"))
				continue
			} else if _, ok := lexer.Builtinvars[it.Name.Lexeme]; ok {
				errors = append(errors, resolver.resolveError(it.Name, "cannot call a function the same as a built-in variable"))
				continue
			} else if _, ok := lexer.Builtinfuncs[it.Name.Lexeme]; ok {
				errors = append(errors, resolver.resolveError(it.Name, "cannot call a function the same as a built-in function"))
				continue
			} else if _, ok := lexer.Keywords[it.Name.Lexeme]; ok {
				errors = append(errors, resolver.resolveError(it.Name, "cannot call a function the same as a keyword"))
				continue
			}
			resolver.functionindices[it.Name.Lexeme] = len(resolver.functionindices)
		}
	}

	errors = append(errors, resolver.items(items)...)
	return resolver.indices, resolver.functionindices, errors
}

func (res *resolver) items(items []Item) []error {
	var errors []error
	for _, item := range items {
		switch it := item.(type) {
		case *FunctionDef:
			errors = append(errors, res.functionDef(it)...)
		case *PatternAction:
			errors = append(errors, res.patternAction(it)...)
		}
	}
	return errors
}

func (res *resolver) functionDef(fd *FunctionDef) []error {
	var errors []error
	res.localindices = map[string]int{}
	defer func() { res.localindices = nil }()
	for i, arg := range fd.Args {
		if _, ok := lexer.Builtinvars[arg.Lexeme]; ok {
			errors = append(errors, res.resolveError(arg, "cannot call a function argument the same as a built-in variable"))
			continue
		} else if _, ok := res.localindices[arg.Lexeme]; ok {
			errors = append(errors, res.resolveError(arg, "cannot have duplicate parameters"))
			continue
		}
		res.localindices[arg.Lexeme] = i
	}

	errors = append(errors, res.blockStat(fd.Body)...)
	return errors
}

func (res *resolver) patternAction(pa *PatternAction) []error {
	var errors []error
	switch patt := pa.Pattern.(type) {
	case *ExprPattern:
		err := res.exprPattern(patt)
		if err != nil {
			errors = append(errors, err)
		}
	case *RangePattern:
		err := res.rangePattern(patt)
		if err != nil {
			errors = append(errors, err)
		}
	}
	errors = append(errors, res.blockStat(pa.Action)...)
	return errors
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

func (res *resolver) blockStat(bs BlockStat) []error {
	var errors []error
	for i := 0; i < len(bs); i++ {
		errors = append(errors, res.stat(bs[i])...)
	}
	return errors
}

func (res *resolver) stat(s Stat) []error {
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
func (res *resolver) ifStat(is *IfStat) []error {
	var errors []error
	err := res.expr(is.Cond)
	if err != nil {
		errors = append(errors, err)
	}
	errors = append(errors, res.stat(is.Body)...)
	errors = append(errors, res.stat(is.ElseBody)...)
	return errors
}

func (res *resolver) forStat(fs *ForStat) []error {
	var errors []error
	errors = append(errors, res.stat(fs.Init)...)
	err := res.expr(fs.Cond)
	if err != nil {
		errors = append(errors, err)
	}
	errors = append(errors, res.stat(fs.Inc)...)
	errors = append(errors, res.stat(fs.Body)...)
	return errors
}

func (res *resolver) forEachStat(fe *ForEachStat) []error {
	var errors []error
	err := res.idExpr(fe.Id)
	if err != nil {
		errors = append(errors, err)
	}
	err = res.idExpr(fe.Array)
	if err != nil {
		errors = append(errors, err)
	}
	errors = append(errors, res.stat(fe.Body)...)
	return errors
}

func (res *resolver) returnStat(rs *ReturnStat) []error {
	if err := res.expr(rs.ReturnVal); err != nil {
		return []error{err}
	}
	return nil
}

func (res *resolver) printStat(ps *PrintStat) []error {
	var errors []error
	err := res.exprs(ps.Exprs)
	if err != nil {
		errors = append(errors, err)
	}
	err = res.expr(ps.File)
	if err != nil {
		errors = append(errors, err)
	}
	return errors
}

func (res *resolver) exprStat(es *ExprStat) []error {
	if err := res.expr(es.Expr); err != nil {
		return []error{err}
	}
	return nil
}

func (res *resolver) exitStat(ex *ExitStat) []error {
	if err := res.expr(ex.Status); err != nil {
		return []error{err}
	}
	return nil
}

func (res *resolver) deleteStat(ds *DeleteStat) []error {
	if err := res.lhsExpr(ds.Lhs); err != nil {
		return []error{err}
	}
	return nil
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
