/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package interpreter

import (
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

func (inter *interpreter) giveStackFrame(size int) ([]awkvalue, int) {
	if inter.stackcount+size > cap(inter.stack) {
		return make([]awkvalue, size), 0
	}
	inter.stackcount += size
	return inter.stack[inter.stackcount-size : inter.stackcount], size
}

func (inter *interpreter) releaseStackFrame(size int) {
	inter.stackcount -= size
}

func (inter *interpreter) evalUserCall(fdef *parser.FunctionDef, args []parser.Expr) (awkvalue, error) {
	arity := len(fdef.Args)
	sublocals, size := inter.giveStackFrame(arity)

	for i := 0; i < arity; i++ {
		var arg parser.Expr
		if len(args) > 0 {
			arg, args = args[0], args[1:]
		}
		v, err := inter.evalArrayAllowed(arg)
		if err != nil {
			return null(), err
		}
		sublocals[i] = v

		// undefined values could be used as arrays
		if idexpr, ok := arg.(*parser.IdExpr); ok && v.typ == Null {
			i := i
			defer func() {
				// link back arrays
				inter.setVariable(idexpr, sublocals[i])
			}()
		}
	}

	for _, arg := range args {
		_, err := inter.eval(arg)
		if err != nil {
			return null(), err
		}
	}

	prevlocals := inter.locals
	inter.locals = sublocals

	defer func() {
		inter.locals = prevlocals
		inter.releaseStackFrame(size)
	}()

	err := inter.execute(fdef.Body)
	var retval awkvalue
	if errRet, ok := err.(errorReturn); ok {
		retval = awkvalue(errRet)
	} else if err != nil {
		return null(), err
	}

	return retval, nil
}

func (inter *interpreter) evalBuiltinCall(called lexer.Token, args []parser.Expr) (awkvalue, error) {
	switch called.Type {
	// Arithmetic functions
	case lexer.Atan2:
		if len(args) != 2 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n1, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		n2, err := inter.eval(args[1])
		if err != nil {
			return null(), err
		}
		num1 := n1.float()
		num2 := n2.float()
		return awknumber(math.Atan2(num1, num2)), nil
	case lexer.Cos:
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		return awknumber(math.Cos(num)), nil
	case lexer.Sin:
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		return awknumber(math.Sin(num)), nil
	case lexer.Exp:
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		return awknumber(math.Exp(num)), nil
	case lexer.Log:
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		if num <= 0 {
			return null(), inter.runtimeError(called, "cannot compute log of a number <= 0")
		}
		return awknumber(math.Log(num)), nil
	case lexer.Sqrt:
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		if num < 0 {
			return null(), inter.runtimeError(called, "cannot compute sqrt of a negative number")
		}
		return awknumber(math.Sqrt(num)), nil
	case lexer.Int:
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		return awknumber(float64(int(num))), nil
	case lexer.Rand:
		if len(args) > 0 {
			return null(), inter.runtimeError(called, "too may arguments")
		}
		n := inter.rng.Float64()
		return awknumber(n), nil
	case lexer.Srand:
		if len(args) > 1 {
			return null(), inter.runtimeError(called, "too many arguments")
		}
		ret := inter.rng.rngseed
		if len(args) == 0 {
			inter.rng.SetSeed(time.Now().UTC().UnixNano())
		} else {
			seed, err := inter.eval(args[0])
			if err != nil {
				return null(), err
			}
			inter.rng.SetSeed(int64(seed.float()))
		}
		return awknumber(float64(ret)), nil
	// String functions
	case lexer.Gsub:
		return generalsub(inter, called, args, true)
	case lexer.Index:
		if len(args) != 2 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		v0, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		v1, err := inter.eval(args[1])
		if err != nil {
			return null(), err
		}
		str := inter.toGoString(v0)
		substr := inter.toGoString(v1)
		return awknumber(float64(strings.Index(str, substr) + 1)), nil
	case lexer.Match:
		if len(args) != 2 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		vs, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		s := inter.toGoString(vs)
		re, err := inter.evalRegex(args[1])
		if err != nil {
			return null(), err
		}
		loc := re.FindStringIndex(s)
		if loc == nil {
			loc = []int{-1, -2}
		}
		rstart := float64(loc[0] + 1)
		rlength := float64(loc[1] - loc[0])
		inter.builtins[parser.Rstart] = awknumber(rstart)
		inter.builtins[parser.Rlength] = awknumber(rlength)
		return awknumber(rstart), nil
	case lexer.Split:
		if len(args) < 3 {
			args = append(args, nil)
		}
		if len(args) != 3 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}

		vs, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}

		s := inter.toGoString(vs)

		id, isid := args[1].(*parser.IdExpr)
		if !isid {
			return null(), inter.runtimeError(args[1].Token(), "expected array")
		}

		_, err = inter.getArrayVariable(id)
		if err != nil {
			return null(), err
		}

		newval := awkarray(map[string]awkvalue{})
		splits, err := inter.split(s, args[2])
		if err != nil {
			return null(), err
		}
		for i, split := range splits {
			newval.array[fmt.Sprintf("%d", i+1)] = awknumericstring(split)
		}

		inter.setVariable(id, newval)

		return awknumber(float64(len(newval.array))), nil
	case lexer.Sprintf:
		if len(args) == 0 {
			args = append(args, nil)
		}
		var str strings.Builder
		err := inter.fprintf(&str, called, args)
		if err != nil {
			return null(), err
		}
		return awknormalstring(str.String()), nil
	case lexer.Sub:
		return generalsub(inter, called, args, false)
	case lexer.Substr:
		if len(args) == 2 {
			args = append(args, nil)
		}
		if len(args) != 3 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		vs, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		s := []rune(inter.toGoString(vs))
		vm, err := inter.eval(args[1])
		if err != nil {
			return null(), err
		}
		m := int(vm.float()) - 1
		if m < 0 {
			m = 0
		} else if m > len(s) {
			m = len(s)
		}
		var n int
		if args[2] == nil {
			n = len(s)
		} else {
			vn, err := inter.eval(args[2])
			if err != nil {
				return null(), err
			}
			n = int(vn.float())
		}
		if n < 0 {
			n = 0
		} else if n+m > len(s) {
			n = len(s) - m
		}
		return awknormalstring(string(s[m : m+n])), nil
	case lexer.Tolower:
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		v, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		return awknormalstring(strings.ToLower(inter.toGoString(v))), nil
	case lexer.Toupper:
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		v, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		return awknormalstring(strings.ToUpper(inter.toGoString(v))), nil
	// IO Functions
	case lexer.Close:
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		file, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		str := inter.toGoString(file)
		opr := inter.outprograms.close(str)
		oprn := 0
		if opr != nil {
			oprn = 1
		}
		of := inter.outfiles.close(str)
		ofn := 0
		if of != nil {
			ofn = 1
		}
		ipr := inter.inprograms.close(str)
		iprn := 0
		if ipr != nil {
			iprn = 1
		}

		return awknumber(float64(oprn | ofn | iprn)), nil
	case lexer.System:
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		v, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		cmdstr := inter.toGoString(v)

		return awknumber(float64(System(cmdstr, inter.stdin, inter.stdout, inter.stderr))), nil
	}
	return null(), nil
}

func (inter *interpreter) evalCall(ce *parser.CallExpr) (awkvalue, error) {
	if ce.Called.Id.Type == lexer.Identifier || ce.Called.Id.Type == lexer.IdentifierParen {
		fdef := inter.ftable[ce.Called.FunctionIndex]
		return fdef(ce.Called.Id, ce.Args)
	} else {
		called := ce.Called.Id
		args := ce.Args
		return inter.evalBuiltinCall(called, args)
	}
}

func System(cmdstr string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	cmd := exec.Command("sh", "-c", cmdstr)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode()
		}
		return -1
	}
	return 0
}

func (inter *interpreter) parseFmtTypes(printtok lexer.Token, s string) (types []func(awkvalue) interface{}, err error) {
	if stored, ok := inter.fprintfcache[s]; ok {
		return stored, nil
	}

	tostr := func(v awkvalue) interface{} {
		return inter.toGoString(v)
	}
	tofloat := func(v awkvalue) interface{} {
		return v.float()
	}
	toint := func(v awkvalue) interface{} {
		return int(v.float())
	}
	tobool := func(v awkvalue) interface{} {
		return v.bool()
	}

	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		i++
		if i < len(s) && s[i] == '%' {
			continue
		}
		if i < len(s) && strings.Contains("+-# 0", s[i:i+1]) {
			i++
		}
		if i < len(s) && s[i] == '*' {
			types = append(types, toint)
			i++
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i < len(s) && s[i] == '.' {
			i++
		}
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i >= len(s) {
			return nil, inter.runtimeError(printtok, "expected format type at end of string")
		}
		switch s[i] {
		case 't': // Boolean
			types = append(types, tobool)
		case 'c', 'd', 'o', 'O', 'U': // Integer
			types = append(types, toint)
		case 'b', 'e', 'E', 'f', 'F', 'g', 'G', 'x', 'X': // Float
			types = append(types, tofloat)
		case 's', 'q', 'v': // String
			types = append(types, tostr)
		default:
			return nil, inter.runtimeError(printtok, fmt.Sprintf("unknown format %c", s[i]))
		}
	}
	if len(inter.fprintfcache) < 100 {
		inter.fprintfcache[s] = types
	}
	return types, nil
}

func (inter *interpreter) fprintf(w io.Writer, print lexer.Token, exprs []parser.Expr) error {
	format, err := inter.eval(exprs[0])
	if err != nil {
		return err
	}
	formatstr := inter.toGoString(format)
	convs, err := inter.parseFmtTypes(print, formatstr)
	if err != nil {
		return err
	}
	if len(convs) > len(exprs[1:]) {
		return inter.runtimeError(print, "run out of arguments for formatted output")
	}
	args := make([]interface{}, 0, len(convs))
	for _, expr := range exprs[1:] {
		arg, err := inter.eval(expr)
		if err != nil {
			return err
		}
		if arg.typ == Array {
			return inter.runtimeError(print, "cannot print array")
		}
		args = append(args, convs[0](arg))
		convs = convs[1:]
	}
	fmt.Fprintf(w, formatstr, args...)
	return nil
}

func (inter *interpreter) split(s string, e parser.Expr) ([]string, error) {
	fs := inter.getFs()
	if e != nil {
		vfs, err := inter.eval(e)
		if err != nil {
			return nil, err
		}
		fs = inter.toGoString(vfs)
	}
	if len(s) == 0 {
		return nil, nil
	} else if fs == " " {
		return strings.Fields(s), nil
	} else if len(fs) <= 1 {
		return strings.Split(s, fs), nil
	} else {
		re := inter.fsregex
		if e != nil {
			var err error
			re, err = inter.evalRegexFromString(e.Token(), fs)
			if err != nil {
				return nil, err
			}
		}
		return re.Split(s, -1), nil
	}
}

func generalsub(inter *interpreter, called lexer.Token, args []parser.Expr, global bool) (awkvalue, error) {
	if len(args) < 3 {
		args = append(args, nil)
	}
	if len(args) != 3 {
		return null(), inter.runtimeError(called, "incorrect number of arguments")
	}
	re, err := inter.evalRegex(args[0])
	if err != nil {
		return null(), err
	}
	vrepl, err := inter.eval(args[1])
	if err != nil {
		return null(), err
	}
	repl := inter.toGoString(vrepl)
	if args[2] == nil {
		res := sub(re, repl, inter.toGoString(inter.getField(0)), global)
		inter.setField(0, awknormalstring(res))
	} else {
		if lhs, islhs := args[2].(parser.LhsExpr); islhs {
			v, err := inter.eval(lhs)
			if err != nil {
				return null(), err
			}
			res := sub(re, repl, inter.toGoString(v), global)
			inter.evalAssignToLhs(lhs, awknormalstring(res))
		} else {
			return null(), inter.runtimeError(args[2].Token(), "expected lhs")
		}
	}
	return null(), nil
}

func sub(re *regexp.Regexp, repl string, src string, global bool) string {
	var count int
	return re.ReplaceAllStringFunc(src, func(matched string) string {
		if !global && count > 0 {
			return matched
		}
		count++
		b := make([]byte, 0, 10)
		for i := 0; i < len(repl); i++ {
			if repl[i] == '&' {
				b = append(b, matched...)
			} else if repl[i] == '\\' {
				i++
				if i >= len(repl) {
					b = append(b, '\\')
					continue
				}
				switch repl[i] {
				case '&':
					b = append(b, '&')
				case '\\':
					b = append(b, '\\')
				default:
					b = append(b, '\\', repl[i])
				}
			} else {
				b = append(b, repl[i])
			}
		}
		return string(b)
	})
}
