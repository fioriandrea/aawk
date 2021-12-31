/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package interpreter

import (
	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

type NativeVal interface {
	String() string
	Float() float64
	Bool() bool
	Int() int
}

type NativeStr string

func (s NativeStr) String() string {
	return string(s)
}

func (s NativeStr) Float() float64 {
	return Awknormalstring(s.String()).Float()
}

func (s NativeStr) Bool() bool {
	return Awknormalstring(s.String()).Bool()
}

func (s NativeStr) Int() int {
	return int(Awknormalstring(s.String()).Float())
}

type NativeNum float64

func (n NativeNum) String() string {
	return Awknumber(n.Float()).String("%g")
}

func (n NativeNum) Float() float64 {
	return float64(n)
}

func (n NativeNum) Bool() bool {
	return Awknumber(n.Float()).Bool()
}

func (n NativeNum) Int() int {
	return int(n.Float())
}

type NativeFunction func(...NativeVal) (NativeVal, error)

func (inter *interpreter) evalNativeFunction(called lexer.Token, nf NativeFunction, exprargs []parser.Expr) (Awkvalue, error) {
	// Collect arguments
	args := make([]Awkvalue, 0)
	for i := 0; i < len(exprargs); i++ {
		expr := exprargs[i]
		awkarg, err := inter.evalArrayAllowed(expr)
		if err != nil {
			return Awknull, err
		}
		args = append(args, awkarg)
	}
	nativeargs := make([]NativeVal, 0, len(args))
	for _, arg := range args {
		nativeargs = append(nativeargs, awkValToNativeVal(arg))
	}
	res, err := nf(nativeargs...)
	if err != nil {
		return Awknull, inter.runtimeError(called, err.Error())
	}
	return nativeValToAwkVal(res), nil
}

func awkValToNativeVal(v Awkvalue) NativeVal {
	switch v.Typ {
	case Normalstring:
		return NativeStr(v.Str)
	case Numericstring:
		return NativeNum(v.N)
	case Null:
		return nil
	default:
		panic("unreachable")
	}
}

func nativeValToAwkVal(nv NativeVal) Awkvalue {
	switch vv := nv.(type) {
	case NativeStr:
		return Awknormalstring(vv.String())
	case NativeNum:
		return Awknumber(vv.Float())
	case nil:
		return Awknull
	default:
		panic("unreachable")
	}
}
