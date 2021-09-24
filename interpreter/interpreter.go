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
	Program        io.RuneReader
	Programname    string
	Arguments      []string
	Natives        map[string]interface{}
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
	compiled, errs := parser.ParseCl(parser.CommandLine{
		Program:        cl.Program,
		Fs:             cl.Fs,
		Preassignments: cl.Preassignments,
		Natives:        cl.Natives,
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
	// Check natives
	nativeserrors := make([]error, 0)
	for name, native := range params.Natives {
		if err := validateNative(name, native); err != nil {
			nativeserrors = append(nativeserrors, err)
		}
	}
	if len(nativeserrors) > 0 {
		return nativeserrors
	}

	var inter interpreter
	inter.initialize(params)
	defer inter.cleanup()
	return []error{inter.run()}
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
	outprograms resources
	outfiles    resources
	inprograms  resources
	infiles     resources
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
		switch ps.RedirOp.Type {
		case lexer.Pipe:
			w = inter.outprograms.get(filestr, func(name string) io.Closer { return spawnOutCommand(name, inter.stdout, inter.stderr) }).(io.Writer)
		case lexer.Greater:
			w = inter.outfiles.get(filestr, func(name string) io.Closer { return spawnOutFile(name, 0) }).(io.Writer)
		case lexer.DoubleGreater:
			w = inter.outfiles.get(filestr, func(name string) io.Closer { return spawnOutFile(name, os.O_APPEND) }).(io.Writer)
		}
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
		fmt.Fprint(w, inter.toGoString(inter.getField(0)))
	} else {
		buff := make([]string, 0, 10)
		for _, expr := range ps.Exprs {
			v, err := inter.eval(expr)
			if err != nil {
				return err
			}
			if v.typ == Array {
				return inter.runtimeError(ps.Token(), "cannot print array")
			}
			buff = append(buff, v.String(inter.getOfmt()))
		}
		fmt.Fprint(w, strings.Join(buff, inter.toGoString(inter.builtins[parser.Ofs])))
	}
	fmt.Fprint(w, inter.toGoString(inter.builtins[parser.Ors]))
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
	arr := inter.getVariable(fes.Array)
	if arr.typ != Array && arr.typ != Null {
		return inter.runtimeError(fes.Array.Id, "cannot do for each on a non array")
	}
	for k := range arr.array {
		err := inter.setVariable(fes.Id, Awknormalstring(k))
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
		delete(v.array, inter.toGoString(ind))
		return nil
	case *parser.IdExpr:
		_, err := inter.getArrayVariable(lhs)
		if err != nil {
			return err
		}
		return inter.setVariable(lhs, Awkarray(map[string]Awkvalue{}))
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
	case *parser.LengthExpr:
		val, err = inter.evalLength(v)
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
		return Awknil(), err
	}
	right, err := inter.eval(b.Right)
	if err != nil {
		return Awknil(), err
	}
	switch b.Op.Type {
	case lexer.Plus:
		return Awknumber(left.Float() + right.Float()), nil
	case lexer.Minus:
		return Awknumber(left.Float() - right.Float()), nil
	case lexer.Star:
		return Awknumber(left.Float() * right.Float()), nil
	case lexer.Slash:
		if right.Float() == 0 {
			return Awknil(), inter.runtimeError(b.Right.Token(), "attempt to divide by 0")
		}
		return Awknumber(left.Float() / right.Float()), nil
	case lexer.Percent:
		if right.Float() == 0 {
			return Awknil(), inter.runtimeError(b.Right.Token(), "attempt to divide by 0")
		}
		return Awknumber(math.Mod(left.Float(), right.Float())), nil
	case lexer.Caret:
		return Awknumber(math.Pow(left.Float(), right.Float())), nil
	case lexer.Concat:
		return Awknormalstring(inter.toGoString(left) + inter.toGoString(right)), nil
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
	return Awknil(), nil
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
		return Awknil(), Awknil(), err
	}
	return inter.getField(int(ind.Float())), ind, nil
}

func (inter *interpreter) evalGetline(gl *parser.GetlineExpr) (Awkvalue, error) {
	var err error
	var filestr string
	if gl.File != nil {
		file, err := inter.eval(gl.File)
		if err != nil {
			return Awknil(), err
		}
		filestr = file.String(inter.getConvfmt())
	}
	var fetchRecord func() (string, error)
	switch gl.Op.Type {
	case lexer.Pipe:
		reader := inter.inprograms.get(filestr, func(name string) io.Closer { return spawnInCommand(name, inter.stdin) }).(io.ByteReader)
		fetchRecord = func() (string, error) { return inter.nextRecord(reader) }
	case lexer.Less:
		reader := inter.infiles.get(filestr, func(name string) io.Closer { return spawnInFile(name) }).(io.ByteReader)
		fetchRecord = func() (string, error) { return inter.nextRecord(reader) }
	default:
		fetchRecord = inter.nextRecordCurrentFile
	}
	var record string
	record, err = fetchRecord()
	retval := Awknumber(0)
	if err == nil {
		retval.n = 1
	} else if err == io.EOF {
		retval.n = 0
	} else {
		retval.n = -1
	}
	recstr := Awknumericstring(record)
	if gl.Variable != nil && retval.n > 0 {
		_, err := inter.evalAssignToLhs(gl.Variable, recstr)
		if err != nil {
			return Awknil(), err
		}
	} else if retval.n > 0 {
		inter.setField(0, recstr)
	}
	return retval, nil
}

func (inter *interpreter) evalLength(le *parser.LengthExpr) (Awkvalue, error) {
	var str string
	if le.Arg == nil {
		str = inter.toGoString(inter.getField(0))
	} else {
		v, err := inter.evalArrayAllowed(le.Arg)
		if err != nil {
			return Awknil(), err
		}
		if v.typ == Array {
			return Awknumber(float64(len(v.array))), nil
		}
		str = inter.toGoString(v)
	}
	return Awknumber(float64(len([]rune(str)))), nil
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
		return Awknil(), err
	}
	v := inter.getVariable(ine.Right)
	if v.typ != Array {
		return Awknumber(0), nil
	}
	str := inter.toGoString(elem)
	_, ok := v.array[str]
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
		return Awknil(), err
	}
	rightre, err := inter.evalRegex(me.Right)
	if err != nil {
		return Awknil(), err
	}
	res := rightre.MatchString(inter.toGoString(left))
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
		return inter.evalRegexFromString(e.Token(), inter.toGoString(rev))
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
		return Awknil(), err
	}
	if !left.Bool() {
		return Awknumber(0), nil
	}
	right, err := inter.eval(bb.Right)
	if err != nil {
		return Awknil(), err
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
		return Awknil(), err
	}
	if left.Bool() {
		return Awknumber(1), nil
	}
	right, err := inter.eval(bb.Right)
	if err != nil {
		return Awknil(), err
	}
	if right.Bool() {
		return Awknumber(1), nil
	} else {
		return Awknumber(0), nil
	}
}

func (inter *interpreter) compareValues(left, right Awkvalue) float64 {
	nusl := left.typ == Numericstring
	nusr := right.typ == Numericstring

	nosl := left.typ == Normalstring
	nosr := right.typ == Normalstring

	/* Comparisons (with the '<', "<=", "!=", "==", '>', and ">=" operators)
	shall be made numerically if both operands are numeric, if one is
	numeric and the other has a string value that is a numeric string, or
	if one is numeric and the other has the uninitialized value. */
	if nosl || nosr || (left.typ == Null && right.typ == Null) || (nusl && nusr) {
		strl := inter.toGoString(left)
		strr := inter.toGoString(right)
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
		return Awknil(), err
	}
	res := Awknumber(0)
	switch u.Op.Type {
	case lexer.Minus:
		res.n = -right.Float()
	case lexer.Plus:
		res.n = right.Float()
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
		return Awknil(), err
	}
	res, err := inter.evalAssignToLhs(a.Left, right)
	return res, err
}

func (inter *interpreter) evalPreIncrement(pr *parser.PreIncrementExpr) (Awkvalue, error) {
	_, ival, err := inter.evalIncrement(pr.IncrementExpr)
	if err != nil {
		return Awknil(), err
	}
	return ival, nil
}

func (inter *interpreter) evalPostIncrement(po *parser.PostIncrementExpr) (Awkvalue, error) {
	val, _, err := inter.evalIncrement(po.IncrementExpr)
	if err != nil {
		return Awknil(), err
	}
	return val, nil
}

func (inter *interpreter) evalLhs(lhs parser.LhsExpr) (Awkvalue, Awkvalue, error) {
	switch l := lhs.(type) {
	case *parser.IdExpr:
		v, err := inter.evalId(l)
		return v, Awknil(), err
	case *parser.DollarExpr:
		return inter.evalDollar(l)
	case *parser.IndexingExpr:
		return inter.evalIndexing(l)
	}
	return Awknil(), Awknil(), nil
}

func (inter *interpreter) evalIncrement(inc *parser.IncrementExpr) (Awkvalue, Awkvalue, error) {
	varval, index, err := inter.evalLhs(inc.Lhs)
	if err != nil {
		return Awknil(), Awknil(), err
	}
	val := Awknumber(varval.Float())
	ival := Awknumber(0)
	switch inc.Op.Type {
	case lexer.Increment:
		ival.n = val.n + 1
	case lexer.Decrement:
		ival.n = val.n - 1
	}
	_, err = inter.evalAssignToLhsIndex(inc.Lhs, index, ival)
	if err != nil {
		return Awknil(), Awknil(), err
	}
	return val, ival, nil
}

func (inter *interpreter) evalAssignToLhs(lhs parser.LhsExpr, val Awkvalue) (Awkvalue, error) {
	_, index, err := inter.evalLhs(lhs)
	if err != nil {
		return Awknil(), err
	}
	return inter.evalAssignToLhsIndex(lhs, index, val)
}

func (inter *interpreter) evalAssignToLhsIndex(lhs parser.LhsExpr, index Awkvalue, val Awkvalue) (Awkvalue, error) {
	switch left := lhs.(type) {
	case *parser.IdExpr:
		if inter.getVariable(left).typ == Array {
			return Awknil(), inter.runtimeError(left.Token(), "cannot use array in scalar context")
		}
		err := inter.setVariable(left, val)
		if err != nil {
			return Awknil(), err
		}
	case *parser.DollarExpr:
		inter.setField(int(index.Float()), val)
	case *parser.IndexingExpr:
		arrval := inter.getVariable(left.Id)
		if arrval.typ == Null {
			arrval = Awkarray(map[string]Awkvalue{})
			inter.setVariable(left.Id, arrval)
		}
		if arrval.typ != Array {
			return Awknil(), inter.runtimeError(left.Token(), "cannot index scalar variable")
		}
		arrval.array[inter.toGoString(index)] = val
	}
	return val, nil
}

func (inter *interpreter) evalTernary(te *parser.TernaryExpr) (Awkvalue, error) {
	cond, err := inter.eval(te.Cond)
	if err != nil {
		return Awknil(), err
	}
	if cond.Bool() {
		return inter.eval(te.Expr0)
	} else {
		return inter.eval(te.Expr1)
	}
}

func (inter *interpreter) evalId(i *parser.IdExpr) (Awkvalue, error) {
	v := inter.getVariable(i)
	if v.typ == Array {
		return Awknil(), inter.runtimeError(i.Token(), "cannot use array in scalar context")
	}
	return v, nil
}

func (inter *interpreter) evalIndexing(i *parser.IndexingExpr) (Awkvalue, Awkvalue, error) {
	v, err := inter.getArrayVariable(i.Id)
	if err != nil {
		return Awknil(), Awknil(), err
	}
	index, err := inter.evalIndex(i.Index)
	if err != nil {
		return Awknil(), Awknil(), err
	}
	res, ok := v.array[index.str]
	// Mentioning an index makes it part of the array keys
	if !ok {
		v.array[index.str] = Awknil()
	}
	return res, index, nil
}

func (inter *interpreter) evalIndex(ind []parser.Expr) (Awkvalue, error) {
	indices := make([]string, 0, 5)
	for _, expr := range ind {
		res, err := inter.eval(expr)
		if err != nil {
			return Awknil(), err
		}
		indices = append(indices, inter.toGoString(res))
	}
	return Awknormalstring(strings.Join(indices, inter.toGoString(inter.builtins[parser.Subsep]))), nil
}

func (inter *interpreter) getField(i int) Awkvalue {
	if i < 0 || i >= len(inter.fields) {
		return Awknil()
	}
	return inter.fields[i]
}

func (inter *interpreter) setField(i int, v Awkvalue) {
	if i >= 1 && i < len(inter.fields) {
		inter.fields[i] = Awknumericstring(inter.toGoString(v))
		ofs := inter.toGoString(inter.builtins[parser.Ofs])
		sep := ""
		var res strings.Builder
		for _, field := range inter.fields[1:] {
			fmt.Fprintf(&res, "%s%s", sep, inter.toGoString(field))
			sep = ofs
		}
		inter.fields[0] = Awknumericstring(res.String())
	} else if i >= len(inter.fields) {
		for i >= len(inter.fields) {
			inter.fields = append(inter.fields, Awknil())
		}
		inter.setField(i, v)
	} else if i == 0 {
		str := inter.toGoString(v)
		inter.fields[0] = Awknumericstring(str)
		inter.fields = inter.fields[0:1]
		splits, _ := inter.split(str, nil)
		for _, split := range splits {
			inter.fields = append(inter.fields, Awknumericstring(split))
		}
		inter.builtins[parser.Nf] = Awknumber(float64(len(inter.fields) - 1))
	}
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
	switch v.typ {
	case Array:
		return v, nil
	case Null:
		err := inter.setVariable(id, Awkarray(map[string]Awkvalue{}))
		if err != nil {
			return Awknil(), err
		}
		return inter.getArrayVariable(id)
	default:
		return Awknil(), inter.runtimeError(id.Token(), "cannot use scalar in array context")
	}
}

func (inter *interpreter) setVariable(id *parser.IdExpr, v Awkvalue) error {
	if id.Index >= 0 {
		inter.globals[id.Index] = v
	} else if id.Index < 0 && id.LocalIndex >= 0 {
		inter.locals[id.LocalIndex] = v
	} else {
		err := inter.setBuiltin(id.BuiltinIndex, v)
		if err != nil {
			return inter.runtimeError(id.Id, err.Error())
		}
		inter.builtins[id.BuiltinIndex] = v
	}
	return nil
}

func (inter *interpreter) setBuiltin(i int, v Awkvalue) error {
	if i == parser.Fs {
		re, err := parser.CompileFs(inter.toGoString(v))
		if err != nil {
			return err
		}
		inter.fsregex = re
	}
	inter.builtins[i] = v
	return nil
}

func (inter *interpreter) getFs() string {
	return inter.toGoString(inter.builtins[parser.Fs])
}

func (inter *interpreter) getOfmt() string {
	return inter.builtins[parser.Ofmt].str
}

func (inter *interpreter) getConvfmt() string {
	return inter.builtins[parser.Convfmt].str
}

func (inter *interpreter) getRs() string {
	return inter.toGoString(inter.builtins[parser.Rs])
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
	inter.setField(0, Awknormalstring(record))
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
				res, err := inter.eval(pat.Expr1)
				if err != nil {
					return err
				}
				if res.Bool() {
					delete(inter.rangematched, i)
				}
				toexecute = true
			} else {
				res, err := inter.eval(pat.Expr0)
				if err != nil {
					return err
				}
				if res.Bool() {
					toexecute = true
					inter.rangematched[i] = true
				}
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

	inter.stack = make([]Awkvalue, 10000)

	inter.globals = make([]Awkvalue, len(params.ResolvedItems.Globalindices))

	inter.ftable = make([]func(lexer.Token, []parser.Expr) (Awkvalue, error), len(params.ResolvedItems.Functionindices))
	inter.initializeFunctions(params)

	// IO structures

	inter.outprograms = resources{}
	inter.outfiles = resources{}
	inter.inprograms = resources{}
	inter.infiles = resources{}
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
		environ.array[splits[0]] = Awknumericstring(splits[1])
	}
	inter.setBuiltin(parser.Environ, environ)

	// Preassignment from command line
	for _, str := range params.Preassignments {
		inter.assignCommandLineString(str)
	}
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

func (inter *interpreter) cleanup() {
	inter.outprograms.closeAll()
	inter.outfiles.closeAll()
	inter.inprograms.closeAll()
	inter.infiles.closeAll()
}
