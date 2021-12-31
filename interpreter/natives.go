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

type NativeFunction func(...Awkvalue) (Awkvalue, error)

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
	res, err := nf(args...)
	if err != nil {
		return Awknull, inter.runtimeError(called, err.Error())
	}
	return res, nil
}
