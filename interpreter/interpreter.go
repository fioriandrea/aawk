/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package interpreter

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"regexp"
	"strings"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

type CommandLine struct {
	Fs             string
	Preassignments []string
	Program        io.Reader
	Programname    string
	Arguments      []string
	Natives        map[string]NativeFunction
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
}

type RunParams struct {
	CommandLine
	parser.CompiledProgram
}

type ErrorExit struct {
	Status int
}

func (ee ErrorExit) Error() string {
	return "exit"
}

func ExecuteCL(cl CommandLine) []error {
	nativeNames := func(natives map[string]NativeFunction) map[string]bool {
		names := make(map[string]bool)
		for name := range natives {
			names[name] = true
		}
		return names
	}
	compiled, errs := parser.ParseCl(parser.CommandLine{
		Program:        cl.Program,
		Fs:             cl.Fs,
		Preassignments: cl.Preassignments,
		Natives:        nativeNames(cl.Natives),
	})
	if len(errs) > 0 {
		return errs
	}

	errs = Exec(RunParams{
		CompiledProgram: compiled,
		CommandLine:     cl,
	})
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func Exec(params RunParams) []error {
	errs := make([]error, 0)
	var inter interpreter
	inter.initialize(params)
	err := inter.run()
	if err != nil {
		errs = append(errs, err)
	}
	errs = append(errs, inter.cleanup()...)
	return errs
}

type interpreter struct {
	// Program
	items parser.ResolvedItems

	// Stacks
	ftable     []func(lexer.Token, []parser.Expr) (Awkvalue, error)
	builtins   []Awkvalue
	fields     []Awkvalue
	globals    []Awkvalue
	stack      []Awkvalue
	stackcount int
	locals     []Awkvalue

	// IO
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	outprograms closableStreams
	outfiles    closableStreams
	inprograms  closableStreams
	infiles     closableStreams
	argindex    int
	currentFile io.ByteReader
	stdinFile   io.ByteReader
	rng         rng

	// Caches
	rangematched map[int]bool
	fprintfcache map[string][]func(Awkvalue) interface{}
	fsregex      *regexp.Regexp
}

var errNext = errors.New("next")
var errBreak = errors.New("break")
var errContinue = errors.New("continue")

type errorReturn Awkvalue

func (er errorReturn) Error() string {
	return "return"
}

type rng struct {
	*rand.Rand
	rngseed int64
}

func (r *rng) setSeed(i int64) {
	r.rngseed = i
	r.Seed(i)
}

func newRNG(seed int64) rng {
	return rng{
		Rand:    rand.New(rand.NewSource(seed)),
		rngseed: seed,
	}
}

func (inter *interpreter) execute(stat parser.Stat) error {
	switch v := stat.(type) {
	case parser.BlockStat:
		return inter.executeBlock(v)
	case *parser.ExprStat:
		return inter.executeExpr(v)
	case *parser.PrintStat:
		return inter.executePrint(v)
	case *parser.IfStat:
		return inter.executeIf(v)
	case *parser.ForStat:
		return inter.executeFor(v)
	case *parser.ForEachStat:
		return inter.executeForEach(v)
	case *parser.NextStat:
		return errNext
	case *parser.BreakStat:
		return errBreak
	case *parser.ContinueStat:
		return errContinue
	case *parser.ReturnStat:
		return inter.executeReturn(v)
	case *parser.ExitStat:
		return inter.executeExit(v)
	case *parser.DeleteStat:
		return inter.executeDelete(v)
	}
	return nil
}

func (inter *interpreter) executeBlock(bs parser.BlockStat) error {
	for _, stat := range bs {
		err := inter.execute(stat)
		if err != nil {
			return err
		}
	}
	return nil
}

func (inter *interpreter) executeExpr(es *parser.ExprStat) error {
	_, err := inter.eval(es.Expr)
	return err
}

func (inter *interpreter) executePrint(ps *parser.PrintStat) error {
	w := inter.stdout
	if ps.File != nil {
		file, err := inter.eval(ps.File)
		if err != nil {
			return err
		}
		filestr := file.String(inter.getConvfmt())
		var cl io.Closer
		switch ps.RedirOp.Type {
		case lexer.Pipe:
			cl, err = inter.outprograms.get(filestr, func(name string) (io.Closer, error) {
				return spawnOutCommand(name, inter.stdout, inter.stderr)
			})
		case lexer.Greater:
			cl, err = inter.outfiles.get(filestr, func(name string) (io.Closer, error) { return spawnOutFile(name, os.O_TRUNC) })
		case lexer.DoubleGreater:
			cl, err = inter.outfiles.get(filestr, func(name string) (io.Closer, error) {
				return spawnOutFile(name, os.O_APPEND)
			})
		}
		if err != nil {
			return inter.runtimeError(ps.Token(), err.Error())
		}
		w = cl.(io.Writer)
	}
	switch ps.Print.Type {
	case lexer.Print:
		return inter.executeSimplePrint(w, ps)
	case lexer.Printf:
		return inter.executePrintf(w, ps)
	}
	return nil
}

func (inter *interpreter) executeSimplePrint(w io.Writer, ps *parser.PrintStat) error {
	if ps.Exprs == nil {
		fmt.Fprint(w, inter.toString(inter.getField(0)))
	} else {
		buff := make([]string, 0, 10)
		for _, expr := range ps.Exprs {
			v, err := inter.eval(expr)
			if err != nil {
				return err
			}
			if v.Typ == Array {
				return inter.runtimeError(ps.Token(), "cannot print array")
			}
			buff = append(buff, v.String(inter.getOfmt()))
		}
		fmt.Fprint(w, strings.Join(buff, inter.toString(inter.builtins[parser.Ofs])))
	}
	fmt.Fprint(w, inter.toString(inter.builtins[parser.Ors]))
	return nil
}

func (inter *interpreter) executePrintf(w io.Writer, ps *parser.PrintStat) error {
	return inter.fprintf(w, ps.Print, ps.Exprs)
}

func (inter *interpreter) executeIf(ifs *parser.IfStat) error {
	cond, err := inter.eval(ifs.Cond)
	if err != nil {
		return err
	}
	if cond.Bool() {
		return inter.execute(ifs.Body)
	} else {
		return inter.execute(ifs.ElseBody)
	}
}

func (inter *interpreter) executeFor(fs *parser.ForStat) error {
	err := inter.execute(fs.Init)
	if err != nil {
		return err
	}
	for {
		cond, err := inter.eval(fs.Cond)
		if err != nil {
			return err
		}
		if !cond.Bool() {
			break
		}
		err = inter.execute(fs.Body)
		if err == errBreak {
			break
		} else if err != nil && err != errContinue {
			return err
		}
		err = inter.execute(fs.Inc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (inter *interpreter) executeForEach(fes *parser.ForEachStat) error {
	arr, err := inter.getArrayVariable(fes.Array)
	if err != nil {
		return err
	}
	for k := range arr.Array {
		_, err := inter.evalAssignToLhs(fes.Id, Awknormalstring(k))
		if err != nil {
			return err
		}
		err = inter.execute(fes.Body)
		if err == errBreak {
			break
		} else if err != nil && err != errContinue {
			return err
		}
	}
	return nil
}

func (inter *interpreter) executeReturn(rs *parser.ReturnStat) error {
	v, err := inter.eval(rs.ReturnVal)
	if err != nil {
		return err
	}
	return errorReturn(v)
}

func (inter *interpreter) executeExit(es *parser.ExitStat) error {
	v, err := inter.eval(es.Status)
	if err != nil {
		return err
	}
	return ErrorExit{
		Status: int(v.Float()),
	}
}

func (inter *interpreter) executeDelete(ds *parser.DeleteStat) error {
	switch lhs := ds.Lhs.(type) {
	case *parser.IndexingExpr:
		v, err := inter.getArrayVariable(lhs.Id)
		if err != nil {
			return err
		}
		ind, err := inter.evalIndex(lhs.Index)
		if err != nil {
			return err
		}
		delete(v.Array, inter.toString(ind))
		return nil
	case *parser.IdExpr:
		_, err := inter.getArrayVariable(lhs)
		if err != nil {
			return err
		}
		return inter.setVariableArrayAllowed(lhs, Awkarray(map[string]Awkvalue{}))
	}
	return nil
}

func (inter *interpreter) eval(expr parser.Expr) (Awkvalue, error) {
	var val Awkvalue
	var err error
	switch v := expr.(type) {
	case parser.LhsExpr:
		val, _, err = inter.evalLhs(v)
	case *parser.BinaryExpr:
		val, err = inter.evalBinary(v)
	case *parser.UnaryExpr:
		val, err = inter.evalUnary(v)
	case *parser.NumberExpr:
		val = Awknumber(v.NumVal)
	case *parser.StringExpr:
		val = Awknormalstring(v.Str.Lexeme)
	case *parser.AssignExpr:
		val, err = inter.evalAssign(v)
	case *parser.PreIncrementExpr:
		val, err = inter.evalPreIncrement(v)
	case *parser.PostIncrementExpr:
		val, err = inter.evalPostIncrement(v)
	case *parser.TernaryExpr:
		val, err = inter.evalTernary(v)
	case *parser.BinaryBoolExpr:
		val, err = inter.evalBinaryBool(v)
	case *parser.GetlineExpr:
		val, err = inter.evalGetline(v)
	case *parser.CallExpr:
		val, err = inter.evalCall(v)
	case *parser.InExpr:
		val, err = inter.evalIn(v)
	case *parser.MatchExpr:
		val, err = inter.evalMatchExpr(v)
	case *parser.RegexExpr:
		val, err = inter.evalRegexExpr(v)
	}
	return val, err
}

func (inter *interpreter) evalArrayAllowed(expr parser.Expr) (Awkvalue, error) {
	if id, ok := expr.(*parser.IdExpr); ok {
		return inter.getVariable(id), nil
	}
	return inter.eval(expr)
}

func (inter *interpreter) evalBinary(b *parser.BinaryExpr) (Awkvalue, error) {
	left, err := inter.eval(b.Left)
	if err != nil {
		return Awknull, err
	}
	right, err := inter.eval(b.Right)
	if err != nil {
		return Awknull, err
	}
	return inter.computeBinary(left, b.Op, right)
}

func (inter *interpreter) computeBinary(left Awkvalue, op lexer.Token, right Awkvalue) (Awkvalue, error) {
	switch op.Type {
	case lexer.Plus:
		return Awknumber(left.Float() + right.Float()), nil
	case lexer.Minus:
		return Awknumber(left.Float() - right.Float()), nil
	case lexer.Star:
		return Awknumber(left.Float() * right.Float()), nil
	case lexer.Slash:
		if right.Float() == 0 {
			return Awknull, inter.runtimeError(op, "attempt to divide by 0")
		}
		return Awknumber(left.Float() / right.Float()), nil
	case lexer.Percent:
		if right.Float() == 0 {
			return Awknull, inter.runtimeError(op, "attempt to divide by 0")
		}
		return Awknumber(math.Mod(left.Float(), right.Float())), nil
	case lexer.Caret:
		return Awknumber(math.Pow(left.Float(), right.Float())), nil
	case lexer.Concat:
		return Awknormalstring(inter.toString(left) + inter.toString(right)), nil
	case lexer.Equal:
		c := inter.compareValues(left, right)
		if c == 0 {
			return Awknumber(1), nil
		} else {
			return Awknumber(0), nil
		}
	case lexer.NotEqual:
		c := inter.compareValues(left, right)
		if c != 0 {
			return Awknumber(1), nil
		} else {
			return Awknumber(0), nil
		}
	case lexer.Less:
		c := inter.compareValues(left, right)
		if c < 0 {
			return Awknumber(1), nil
		} else {
			return Awknumber(0), nil
		}
	case lexer.LessEqual:
		c := inter.compareValues(left, right)
		if c <= 0 {
			return Awknumber(1), nil
		} else {
			return Awknumber(0), nil
		}
	case lexer.Greater:
		c := inter.compareValues(left, right)
		if c > 0 {
			return Awknumber(1), nil
		} else {
			return Awknumber(0), nil
		}
	case lexer.GreaterEqual:
		c := inter.compareValues(left, right)
		if c >= 0 {
			return Awknumber(1), nil
		} else {
			return Awknumber(0), nil
		}
	}
	return Awknull, nil
}

func (inter *interpreter) evalBinaryBool(bb *parser.BinaryBoolExpr) (Awkvalue, error) {
	var val Awkvalue
	var err error
	switch bb.Op.Type {
	case lexer.DoubleAnd:
		val, err = inter.evalAnd(bb)
	case lexer.DoublePipe:
		val, err = inter.evalOr(bb)
	}
	return val, err
}

func (inter *interpreter) evalDollar(de *parser.DollarExpr) (Awkvalue, Awkvalue, error) {
	ind, err := inter.eval(de.Field)
	if err != nil {
		return Awknull, Awknull, err
	}
	return inter.getField(int(ind.Float())), ind, nil
}

// In case of error, always fail silently and return -1 (this is what other implementation do)
func (inter *interpreter) evalGetline(gl *parser.GetlineExpr) (Awkvalue, error) {
	var err error
	var filestr string

	if gl.File != nil {
		file, err := inter.eval(gl.File)
		if err != nil {
			return Awknull, err
		}
		filestr = file.String(inter.getConvfmt())
	}

	// Handle file
	var fetchRecord func() (string, error)
	switch gl.Op.Type {
	case lexer.Pipe:
		cl, err := inter.inprograms.get(filestr, func(name string) (io.Closer, error) {
			return spawnInCommand(name, inter.stdin, inter.stderr)
		})
		if err != nil {
			return Awknumber(-1), nil
		}
		fetchRecord = func() (string, error) {
			return inter.nextRecord(cl.(io.ByteReader))
		}
		if err != nil {
			return Awknumber(-1), nil
		}
	case lexer.Less:
		cl, err := inter.infiles.get(filestr, func(name string) (io.Closer, error) {
			return spawnInFile(name)
		})
		fetchRecord = func() (string, error) {
			return inter.nextRecord(cl.(io.ByteReader))
		}
		if err != nil {
			return Awknumber(-1), nil
		}
	default:
		fetchRecord = inter.nextRecordCurrentFile
	}

	var record string
	record, err = fetchRecord()

	// Handle return value
	retval := Awknumber(0)
	if err == nil {
		retval.N = 1
	} else if err == io.EOF {
		retval.N = 0
	} else {
		retval.N = -1
	}

	// Handle variable assignment
	recstr := Awknumericstring(record)
	if gl.Variable != nil && retval.N > 0 {
		_, err := inter.evalAssignToLhs(gl.Variable, recstr)
		if err != nil {
			return Awknull, err
		}
	} else if retval.N > 0 {
		inter.setField(0, recstr)
	}

	return retval, nil
}

func (inter *interpreter) evalIn(ine *parser.InExpr) (Awkvalue, error) {
	var elem Awkvalue
	var err error
	switch v := ine.Left.(type) {
	case parser.ExprList:
		elem, err = inter.evalIndex(v)
	default:
		elem, err = inter.eval(v)
	}
	if err != nil {
		return Awknull, err
	}
	v, err := inter.getArrayVariable(ine.Right)
	if err != nil {
		return Awknull, err
	}
	str := inter.toString(elem)
	_, ok := v.Array[str]
	if ok {
		return Awknumber(1), nil
	} else {
		return Awknumber(0), nil
	}
}

func (inter *interpreter) evalRegexExpr(re *parser.RegexExpr) (Awkvalue, error) {
	expr := &parser.MatchExpr{
		Left: &parser.DollarExpr{
			Dollar: lexer.Token{
				Lexeme: "$",
				Type:   lexer.Dollar,
				Line:   re.Regex.Line,
			},
			Field: &parser.NumberExpr{
				Num: lexer.Token{
					Lexeme: "0",
					Type:   lexer.Number,
					Line:   re.Regex.Line,
				},
			},
		},
		Op: lexer.Token{
			Lexeme: "~",
			Type:   lexer.Tilde,
			Line:   re.Regex.Line,
		},
		Right: re,
	}
	return inter.evalMatchExpr(expr)
}

func (inter *interpreter) evalMatchExpr(me *parser.MatchExpr) (Awkvalue, error) {
	left, err := inter.eval(me.Left)
	if err != nil {
		return Awknull, err
	}
	rightre, err := inter.evalRegex(me.Right)
	if err != nil {
		return Awknull, err
	}
	res := rightre.MatchString(inter.toString(left))
	if me.Op.Type == lexer.NotTilde {
		res = !res
	}
	if res {
		return Awknumber(1), nil
	} else {
		return Awknumber(0), nil
	}
}

func (inter *interpreter) evalRegex(e parser.Expr) (*regexp.Regexp, error) {
	switch v := e.(type) {
	case *parser.RegexExpr:
		return v.Compiled, nil
	default:
		rev, err := inter.eval(e)
		if err != nil {
			return nil, err
		}
		return inter.evalRegexFromString(e.Token(), inter.toString(rev))
	}
}

func (inter *interpreter) evalRegexFromString(retok lexer.Token, str string) (*regexp.Regexp, error) {
	res, err := regexp.Compile(str)
	if err != nil {
		return nil, inter.runtimeError(retok, fmt.Sprint(err))
	}
	return res, nil
}

func (inter *interpreter) evalAnd(bb *parser.BinaryBoolExpr) (Awkvalue, error) {
	left, err := inter.eval(bb.Left)
	if err != nil {
		return Awknull, err
	}
	if !left.Bool() {
		return Awknumber(0), nil
	}
	right, err := inter.eval(bb.Right)
	if err != nil {
		return Awknull, err
	}
	if right.Bool() {
		return Awknumber(1), nil
	} else {
		return Awknumber(0), nil
	}
}

func (inter *interpreter) evalOr(bb *parser.BinaryBoolExpr) (Awkvalue, error) {
	left, err := inter.eval(bb.Left)
	if err != nil {
		return Awknull, err
	}
	if left.Bool() {
		return Awknumber(1), nil
	}
	right, err := inter.eval(bb.Right)
	if err != nil {
		return Awknull, err
	}
	if right.Bool() {
		return Awknumber(1), nil
	} else {
		return Awknumber(0), nil
	}
}

func (inter *interpreter) compareValues(left, right Awkvalue) float64 {
	nusl := left.Typ == Numericstring
	nusr := right.Typ == Numericstring

	nosl := left.Typ == Normalstring
	nosr := right.Typ == Normalstring

	/* Comparisons (with the '<', "<=", "!=", "==", '>', and ">=" operators)
	shall be made numerically if both operands are numeric, if one is
	numeric and the other has a string value that is a numeric string, or
	if one is numeric and the other has the uninitialized value. */
	if nosl || nosr || (left.Typ == Null && right.Typ == Null) || (nusl && nusr) {
		strl := inter.toString(left)
		strr := inter.toString(right)
		if strl == strr {
			return 0
		} else if strl < strr {
			return -1
		} else {
			return 1
		}
	}
	return left.Float() - right.Float()
}

func (inter *interpreter) evalUnary(u *parser.UnaryExpr) (Awkvalue, error) {
	right, err := inter.eval(u.Right)
	if err != nil {
		return Awknull, err
	}
	res := Awknumber(0)
	switch u.Op.Type {
	case lexer.Minus:
		res.N = -right.Float()
	case lexer.Plus:
		res.N = right.Float()
	case lexer.Not:
		if right.Bool() {
			res = Awknumber(0)
		} else {
			res = Awknumber(1)
		}
	}
	return res, nil
}

func (inter *interpreter) evalAssign(a *parser.AssignExpr) (Awkvalue, error) {
	right, err := inter.eval(a.Right)
	if err != nil {
		return Awknull, err
	}
	res, err := inter.evalSpecialAssignToLhs(a.Left, a.Equal, right)
	return res, err
}

func (inter *interpreter) evalAssignToLhs(lhs parser.LhsExpr, val Awkvalue) (Awkvalue, error) {
	return inter.evalSpecialAssignToLhs(lhs, lexer.Token{Type: lexer.Assign}, val)
}

func (inter *interpreter) evalSpecialAssignToLhs(lhs parser.LhsExpr, op lexer.Token, val Awkvalue) (Awkvalue, error) {
	vlhs, index, err := inter.evalLhs(lhs)
	if err != nil {
		return Awknull, err
	}

	switch op.Type {
	case lexer.ExpAssign:
		op.Type = lexer.Caret
	case lexer.ModAssign:
		op.Type = lexer.Percent
	case lexer.MulAssign:
		op.Type = lexer.Star
	case lexer.DivAssign:
		op.Type = lexer.Slash
	case lexer.PlusAssign:
		op.Type = lexer.Plus
	case lexer.MinusAssign:
		op.Type = lexer.Minus
	}
	if op.Type != lexer.Assign {
		vbin, err := inter.computeBinary(vlhs, op, val)
		if err != nil {
			return Awknull, err
		}
		val = vbin
	}

	return inter.evalAssignToLhsIndex(lhs, index, val)
}

func (inter *interpreter) evalPreIncrement(pr *parser.PreIncrementExpr) (Awkvalue, error) {
	_, ival, err := inter.evalIncrement(pr.IncrementExpr)
	if err != nil {
		return Awknull, err
	}
	return ival, nil
}

func (inter *interpreter) evalPostIncrement(po *parser.PostIncrementExpr) (Awkvalue, error) {
	val, _, err := inter.evalIncrement(po.IncrementExpr)
	if err != nil {
		return Awknull, err
	}
	return val, nil
}

func (inter *interpreter) evalLhs(lhs parser.LhsExpr) (Awkvalue, Awkvalue, error) {
	switch l := lhs.(type) {
	case *parser.IdExpr:
		v, err := inter.evalId(l)
		return v, Awknull, err
	case *parser.DollarExpr:
		return inter.evalDollar(l)
	case *parser.IndexingExpr:
		return inter.evalIndexing(l)
	}
	return Awknull, Awknull, nil
}

func (inter *interpreter) evalIncrement(inc *parser.IncrementExpr) (Awkvalue, Awkvalue, error) {
	varval, index, err := inter.evalLhs(inc.Lhs)
	if err != nil {
		return Awknull, Awknull, err
	}
	val := Awknumber(varval.Float())
	ival := Awknumber(0)
	switch inc.Op.Type {
	case lexer.Increment:
		ival.N = val.N + 1
	case lexer.Decrement:
		ival.N = val.N - 1
	}
	_, err = inter.evalAssignToLhsIndex(inc.Lhs, index, ival)
	if err != nil {
		return Awknull, Awknull, err
	}
	return val, ival, nil
}

func (inter *interpreter) evalAssignToLhsIndex(lhs parser.LhsExpr, index Awkvalue, val Awkvalue) (Awkvalue, error) {
	switch left := lhs.(type) {
	case *parser.IdExpr:
		err := inter.setVariable(left, val)
		if err != nil {
			return Awknull, err
		}
	case *parser.DollarExpr:
		inter.setField(int(index.Float()), val)
	case *parser.IndexingExpr:
		arrval, err := inter.getArrayVariable(left.Id)
		if err != nil {
			return Awknull, err
		}
		arrval.Array[inter.toString(index)] = val
	}
	return val, nil
}

func (inter *interpreter) evalTernary(te *parser.TernaryExpr) (Awkvalue, error) {
	cond, err := inter.eval(te.Cond)
	if err != nil {
		return Awknull, err
	}
	if cond.Bool() {
		return inter.eval(te.Expr0)
	} else {
		return inter.eval(te.Expr1)
	}
}

func (inter *interpreter) evalId(i *parser.IdExpr) (Awkvalue, error) {
	v := inter.getVariable(i)
	if v.Typ == Array {
		return Awknull, inter.runtimeError(i.Token(), "cannot use array in scalar context")
	}
	return v, nil
}

func (inter *interpreter) evalIndexing(i *parser.IndexingExpr) (Awkvalue, Awkvalue, error) {
	v, err := inter.getArrayVariable(i.Id)
	if err != nil {
		return Awknull, Awknull, err
	}
	index, err := inter.evalIndex(i.Index)
	if err != nil {
		return Awknull, Awknull, err
	}
	res, ok := v.Array[index.Str]
	// Mentioning an index makes it part of the array keys
	if !ok {
		v.Array[index.Str] = Awknull
	}
	return res, index, nil
}

func (inter *interpreter) evalIndex(ind []parser.Expr) (Awkvalue, error) {
	indices := make([]string, 0, 5)
	for _, expr := range ind {
		res, err := inter.eval(expr)
		if err != nil {
			return Awknull, err
		}
		indices = append(indices, inter.toString(res))
	}
	return Awknormalstring(strings.Join(indices, inter.toString(inter.builtins[parser.Subsep]))), nil
}

func (inter *interpreter) getField(i int) Awkvalue {
	if i < 0 || i >= len(inter.fields) {
		return Awknormalstring("")
	}
	return inter.fields[i]
}

// Sets field at position i and recomputes NF if necessary
func (inter *interpreter) setField(i int, v Awkvalue) {
	// https://stackoverflow.com/questions/51632945/in-awk-why-does-a-nonexistent-field-like-nf1-not-equal-zero/51638902
	if i >= 1 && i < len(inter.fields) {
		inter.fields[i] = Awkstring(inter.toString(v), v.Typ)
		tojoin := make([]string, 0, len(inter.fields[1:]))
		for _, field := range inter.fields[1:] {
			tojoin = append(tojoin, inter.toString(field))
		}
		inter.fields[0] = Awknormalstring(strings.Join(tojoin, inter.getOfs()))
	} else if i >= len(inter.fields) {
		for i >= len(inter.fields) {
			inter.fields = append(inter.fields, Awknormalstring(""))
		}
		inter.setField(i, v)
	} else if i == 0 {
		str := inter.toString(v)
		splits, _ := inter.split(str, nil)
		vsplits := make([]Awkvalue, 0, len(splits))
		for _, sp := range splits {
			vsplits = append(vsplits, Awkstring(sp, v.Typ))
		}
		inter.setSplittedFields(v, vsplits)
		inter.builtins[parser.Nf] = Awknumber(float64(len(inter.fields) - 1))
	}
}

// Set fields array with given fields.
func (inter *interpreter) setSplittedFields(d0 Awkvalue, splits []Awkvalue) {
	inter.fields = inter.fields[0:0]
	inter.fields = append(inter.fields, d0)
	inter.fields = append(inter.fields, splits...)
}

func (inter *interpreter) setBuiltin(i int, v Awkvalue) error {
	switch i {
	case parser.Fs:
		re, err := parser.CompileFs(inter.toString(v))
		if err != nil {
			return err
		}
		inter.fsregex = re
		inter.builtins[parser.Fs] = v
	case parser.Nf:
		inter.builtins[parser.Nf] = v
		nf := int(v.Float())
		if nf < 0 {
			nf = 0
		}
		splits, _ := inter.split(inter.toString(inter.getField(0)), nil)
		if len(splits) > nf {
			splits = splits[:nf]
		}
		vsplits := make([]Awkvalue, 0, len(splits))
		for i, sp := range splits {
			// Keep previous type
			typ := inter.getField(i + 1).Typ
			vsplits = append(vsplits, Awkstring(sp, typ))
		}
		inter.setSplittedFields(Awknormalstring(strings.Join(splits, inter.getOfs())), vsplits)
	default:
		inter.builtins[i] = v
	}
	return nil
}

func (inter *interpreter) getVariable(id *parser.IdExpr) Awkvalue {
	if id.Index >= 0 {
		return inter.globals[id.Index]
	} else if id.Index < 0 && id.LocalIndex >= 0 {
		return inter.locals[id.LocalIndex]
	} else {
		return inter.builtins[id.BuiltinIndex]
	}
}

func (inter *interpreter) getArrayVariable(id *parser.IdExpr) (Awkvalue, error) {
	v := inter.getVariable(id)
	switch v.Typ {
	case Array:
		return v, nil
	case Null:
		err := inter.setVariable(id, nullToArray(v))
		if err != nil {
			return Awknull, err
		}
		return inter.getArrayVariable(id)
	default:
		return Awknull, inter.runtimeError(id.Token(), "cannot use scalar in array context")
	}
}

func (inter *interpreter) setVariableArrayAllowed(id *parser.IdExpr, v Awkvalue) error {
	if id.Index >= 0 {
		inter.globals[id.Index] = v
	} else if id.Index < 0 && id.LocalIndex >= 0 {
		inter.locals[id.LocalIndex] = v
	} else {
		err := inter.setBuiltin(id.BuiltinIndex, v)
		if err != nil {
			return inter.runtimeError(id.Id, err.Error())
		}
	}
	return nil
}

func (inter *interpreter) setVariable(id *parser.IdExpr, v Awkvalue) error {
	old := inter.getVariable(id)
	if old.Typ == Array {
		return inter.runtimeError(id.Token(), "cannot use array in scalar context")
	}
	return inter.setVariableArrayAllowed(id, v)
}

func (inter *interpreter) getFs() string {
	return inter.toString(inter.builtins[parser.Fs])
}

func (inter *interpreter) getOfmt() string {
	return inter.builtins[parser.Ofmt].Str
}

func (inter *interpreter) getConvfmt() string {
	return inter.builtins[parser.Convfmt].Str
}

func (inter *interpreter) getRs() string {
	return inter.toString(inter.builtins[parser.Rs])
}

func (inter *interpreter) getOfs() string {
	return inter.toString(inter.builtins[parser.Ofs])
}

func (inter *interpreter) runtimeError(tok lexer.Token, msg string) error {
	return fmt.Errorf("at line %d (%s): runtime error: %s", tok.Line, tok.Lexeme, msg)
}

func (inter *interpreter) run() error {
	var skipNormals bool
	var errexit ErrorExit

	err := inter.runBegins()
	if ee, ok := err.(ErrorExit); ok {
		errexit = ee
		skipNormals = true
	} else if err != nil {
		return err
	}

	if !skipNormals {
		err := inter.runNormals()
		if ee, ok := err.(ErrorExit); ok {
			errexit = ee
		} else if err != nil {
			return err
		}
	}

	err = inter.runEnds()
	if ee, ok := err.(ErrorExit); ok {
		errexit = ee
	} else if err != nil {
		return err
	}
	return errexit
}

func (inter *interpreter) runBegins() error {
	for _, beg := range inter.items.Begins {
		if err := inter.execute(beg.Action); err != nil {
			return err
		}
	}
	return nil
}

func (inter *interpreter) runNormals() error {
	if len(inter.items.Normals) == 0 {
		return nil
	}

	for {
		text, err := inter.nextRecordCurrentFile()
		if err != nil && err != io.EOF {
			return err
		}
		if err != nil && err == io.EOF {
			break
		}
		err = inter.processRecord(text)
		if err != nil {
			return err
		}
	}
	return nil
}

func (inter *interpreter) processRecord(record string) error {
	inter.setField(0, Awknumericstring(record))
	for i, normal := range inter.items.Normals {
		var toexecute bool
		switch pat := normal.Pattern.(type) {
		case *parser.ExprPattern:
			res, err := inter.eval(pat.Expr)
			if err != nil {
				return err
			}
			toexecute = res.Bool()
		case *parser.RangePattern:
			if inter.rangematched[i] {
				toexecute = true
			}
			res0, err := inter.eval(pat.Expr1)
			if err != nil {
				return err
			}
			if res0.Bool() {
				inter.rangematched[i] = true
				toexecute = true
			}
			res1, err := inter.eval(pat.Expr1)
			if err != nil {
				return err
			}
			if res1.Bool() {
				delete(inter.rangematched, i)
			}
		}
		if toexecute {
			if err := inter.execute(normal.Action); err != nil {
				if err == errNext {
					break
				}
				return err
			}
		}
	}
	return nil
}

func (inter *interpreter) runEnds() error {
	for _, end := range inter.items.Ends {
		if err := inter.execute(end.Action); err != nil {
			return err
		}
	}
	return nil
}

// Initialization

// Assumes params is completely correct (e.g. FS is a valid regex)
func (inter *interpreter) initialize(params RunParams) {
	inter.items = params.ResolvedItems

	// Stacks

	inter.builtins = make([]Awkvalue, len(parser.Builtinvars))
	inter.initializeBuiltinVariables(params)

	inter.globals = make([]Awkvalue, len(params.ResolvedItems.Globalindices))

	inter.stack = make([]Awkvalue, 10000)

	inter.ftable = make([]func(lexer.Token, []parser.Expr) (Awkvalue, error), len(params.ResolvedItems.Functionindices))
	inter.initializeFunctions(params)

	// Preassignment from command line
	for _, str := range params.Preassignments {
		inter.assignCommandLineString(str)
	}

	// IO structures

	inter.outprograms = closableStreams{}
	inter.outfiles = closableStreams{}
	inter.inprograms = closableStreams{}
	inter.infiles = closableStreams{}
	inter.rng = newRNG(0)
	inter.argindex = 0
	inter.currentFile = nil
	inter.stdin = params.Stdin
	inter.stdout = params.Stdout
	inter.stderr = params.Stderr
	inter.stdinFile = bufio.NewReader(inter.stdin)

	// Caches

	inter.rangematched = map[int]bool{}
	inter.fprintfcache = map[string][]func(Awkvalue) interface{}{}
}

func (inter *interpreter) initializeBuiltinVariables(params RunParams) {
	// General
	inter.setBuiltin(parser.Convfmt, Awknormalstring("%.6g"))
	inter.setBuiltin(parser.Fnr, Awknumber(0))
	inter.setBuiltin(parser.Fs, Awknumericstring(params.Fs))
	inter.setBuiltin(parser.Nr, Awknumber(0))
	inter.setBuiltin(parser.Ofmt, Awknormalstring("%.6g"))
	inter.setBuiltin(parser.Ofs, Awknormalstring(" "))
	inter.setBuiltin(parser.Ors, Awknormalstring("\n"))
	inter.setBuiltin(parser.Rs, Awknormalstring("\n"))
	inter.setBuiltin(parser.Subsep, Awknormalstring("\034"))

	// ARGC and ARGV
	argc := len(params.Arguments) + 1
	argv := map[string]Awkvalue{}
	argv["0"] = Awknumericstring(params.Programname)
	for i := 1; i <= argc-1; i++ {
		argv[fmt.Sprintf("%d", i)] = Awknumericstring(params.Arguments[i-1])
	}
	inter.setBuiltin(parser.Argc, Awknumber(float64(argc)))
	inter.setBuiltin(parser.Argv, Awkarray(argv))

	// ENVIRON
	environ := Awkarray(map[string]Awkvalue{})
	for _, envpair := range os.Environ() {
		splits := strings.Split(envpair, "=")
		environ.Array[splits[0]] = Awknumericstring(splits[1])
	}
	inter.setBuiltin(parser.Environ, environ)

}

func (inter *interpreter) assignCommandLineString(assign string) {
	splits := strings.Split(assign, "=")
	if i, ok := parser.Builtinvars[splits[0]]; ok {
		inter.setBuiltin(i, Awknumericstring(splits[1]))
	} else if i, ok := inter.items.Globalindices[splits[0]]; ok {
		inter.globals[i] = Awknumericstring(splits[1])
	}
}

func (inter *interpreter) initializeFunctions(params RunParams) {
	// Natives
	for name, nf := range params.Natives {
		nf := nf
		inter.ftable[params.ResolvedItems.Functionindices[name]] = func(fname lexer.Token, args []parser.Expr) (Awkvalue, error) {
			return inter.evalNativeFunction(fname, nf, args)
		}
	}

	// User defined
	for _, fi := range params.ResolvedItems.Functions {
		fi := fi
		inter.ftable[params.ResolvedItems.Functionindices[fi.Name.Lexeme]] = func(fname lexer.Token, args []parser.Expr) (Awkvalue, error) {
			return inter.evalUserCall(fi, args)
		}
	}
}

func (inter *interpreter) cleanup() []error {
	errors := make([]error, 0)
	errors = append(errors, inter.outprograms.closeAll()...)
	errors = append(errors, inter.outfiles.closeAll()...)
	errors = append(errors, inter.inprograms.closeAll()...)
	errors = append(errors, inter.infiles.closeAll()...)
	return errors
}
