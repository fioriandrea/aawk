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
	"log"
	"math"
	"math/rand"
	"os"
	"regexp"
	"strings"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

type CommandLine struct {
	Fs          string
	Variables   []string
	Program     io.RuneReader
	Programname string
	Arguments   []string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

type RunParams struct {
	ResolvedItems    parser.ResolvedItems
	Fs               string
	Arguments        []string
	Programname      string
	Builtinpreassing map[int]string
	Globalpreassign  map[int]string
	Stdin            io.Reader
	Stdout           io.Writer
	Stderr           io.Writer
}

type ErrorExit struct {
	Status int
}

func (ee ErrorExit) Error() string {
	return "exit"
}

func ExecuteCL(cl CommandLine) []error {
	if _, err := regexp.Compile(cl.Fs); err != nil {
		return []error{fmt.Errorf("invalid FS")}
	}

	lex := lexer.NewLexer(cl.Program)
	bifunctions := make([]string, 0, len(builtinfuncs))
	for name := range builtinfuncs {
		bifunctions = append(bifunctions, name)
	}
	items, errs := parser.Parse(lex, bifunctions)
	if len(errs) > 0 {
		return errs
	}

	globalpreassign := map[int]string{}
	builtinpreassing := map[int]string{}
	for _, variable := range cl.Variables {
		splits := strings.Split(variable, "=")
		if i, ok := parser.Builtinvars[splits[0]]; ok {
			builtinpreassing[i] = splits[1]
		} else if i, ok := items.Globalindices[splits[0]]; ok {
			globalpreassign[i] = splits[1]
		}
	}

	return []error{Exec(RunParams{
		ResolvedItems:    items,
		Fs:               cl.Fs,
		Arguments:        cl.Arguments,
		Programname:      cl.Programname,
		Builtinpreassing: builtinpreassing,
		Globalpreassign:  globalpreassign,
		Stdin:            cl.Stdin,
		Stdout:           cl.Stdout,
		Stderr:           cl.Stderr,
	})}
}

func Exec(params RunParams) error {
	var inter interpreter
	inter.initialize(params)
	defer inter.cleanup()
	return inter.run()
}

type interpreter struct {
	// Program
	items parser.Items

	// Stacks
	ftable     []Callable
	builtins   []awkvalue
	fields     []awkvalue
	globals    []awkvalue
	stack      []awkvalue
	stackcount int
	locals     []awkvalue

	// IO
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	outprograms outwriters
	outfiles    outwriters
	argindex    int
	currentFile io.RuneReader
	stdinFile   io.RuneReader
	inprograms  inreaders
	infiles     inreaders
	rng         rng

	// Caches
	rangematched map[int]bool
	fprintfcache map[string][]func(awkvalue) interface{}
	fsregex      *regexp.Regexp
	spaceregex   *regexp.Regexp
}

var errNext = errors.New("next")
var errBreak = errors.New("break")
var errContinue = errors.New("continue")

type errorReturn awkvalue

func (er errorReturn) Error() string {
	return "return"
}

type rng struct {
	*rand.Rand
	rngseed int64
}

func (r *rng) SetSeed(i int64) {
	r.rngseed = i
	r.Seed(i)
}

func NewRNG(seed int64) rng {
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
	var w io.Writer
	w = os.Stdout
	if ps.File != nil {
		file, err := inter.eval(ps.File)
		if err != nil {
			return err
		}
		filestr := file.string(inter.getConvfmt())
		switch ps.RedirOp.Type {
		case lexer.Pipe:
			w = inter.outprograms.get(filestr, inter.spawnOutProgram)
		case lexer.Greater:
			w = inter.outfiles.get(filestr, spawnFileNormal)
		case lexer.DoubleGreater:
			w = inter.outfiles.get(filestr, spawnFileAppend)
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
		sep := ""
		for _, expr := range ps.Exprs {
			v, err := inter.eval(expr)
			if err != nil {
				return err
			}
			if v.typ == Array {
				return inter.runtimeError(ps.Token(), "cannot print array")
			}
			fmt.Fprint(w, sep)
			fmt.Fprint(w, v.string(inter.getOfmt()))
			sep = inter.toGoString(inter.builtins[parser.Ofs])
		}
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
	if cond.bool() {
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
		if !cond.bool() {
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
		err := inter.setVariable(fes.Id, awknormalstring(k))
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
		Status: int(v.float()),
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
		return inter.setVariable(lhs, awkarray(map[string]awkvalue{}))
	}
	return nil
}

func (inter *interpreter) eval(expr parser.Expr) (awkvalue, error) {
	var val awkvalue
	var err error
	switch v := expr.(type) {
	case *parser.BinaryExpr:
		val, err = inter.evalBinary(v)
	case *parser.UnaryExpr:
		val, err = inter.evalUnary(v)
	case *parser.NumberExpr:
		val = awknumber(v.NumVal)
	case *parser.StringExpr:
		val = awknormalstring(v.Str.Lexeme)
	case *parser.AssignExpr:
		val, err = inter.evalAssign(v)
	case *parser.IdExpr:
		val, err = inter.evalId(v)
	case *parser.IndexingExpr:
		val, err = inter.evalIndexing(v)
	case *parser.PreIncrementExpr:
		val, err = inter.evalPreIncrement(v)
	case *parser.PostIncrementExpr:
		val, err = inter.evalPostIncrement(v)
	case *parser.TernaryExpr:
		val, err = inter.evalTernary(v)
	case *parser.BinaryBoolExpr:
		val, err = inter.evalBinaryBool(v)
	case *parser.DollarExpr:
		val, err = inter.evalDollar(v)
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

func (inter *interpreter) evalArrayAllowed(expr parser.Expr) (awkvalue, error) {
	if id, ok := expr.(*parser.IdExpr); ok {
		return inter.getVariable(id), nil
	}
	return inter.eval(expr)
}

func (inter *interpreter) evalBinary(b *parser.BinaryExpr) (awkvalue, error) {
	left, err := inter.eval(b.Left)
	if err != nil {
		return null(), err
	}
	right, err := inter.eval(b.Right)
	if err != nil {
		return null(), err
	}
	switch b.Op.Type {
	case lexer.Plus:
		return awknumber(left.float() + right.float()), nil
	case lexer.Minus:
		return awknumber(left.float() - right.float()), nil
	case lexer.Star:
		return awknumber(left.float() * right.float()), nil
	case lexer.Slash:
		if right.float() == 0 {
			return null(), inter.runtimeError(b.Right.Token(), "attempt to divide by 0")
		}
		return awknumber(left.float() / right.float()), nil
	case lexer.Percent:
		if right.float() == 0 {
			return null(), inter.runtimeError(b.Right.Token(), "attempt to divide by 0")
		}
		return awknumber(math.Mod(left.float(), right.float())), nil
	case lexer.Caret:
		return awknumber(math.Pow(left.float(), right.float())), nil
	case lexer.Concat:
		return awknormalstring(inter.toGoString(left) + inter.toGoString(right)), nil
	case lexer.Equal:
		c := inter.compareValues(left, right)
		if c == 0 {
			return awknumber(1), nil
		} else {
			return awknumber(0), nil
		}
	case lexer.NotEqual:
		c := inter.compareValues(left, right)
		if c != 0 {
			return awknumber(1), nil
		} else {
			return awknumber(0), nil
		}
	case lexer.Less:
		c := inter.compareValues(left, right)
		if c < 0 {
			return awknumber(1), nil
		} else {
			return awknumber(0), nil
		}
	case lexer.LessEqual:
		c := inter.compareValues(left, right)
		if c <= 0 {
			return awknumber(1), nil
		} else {
			return awknumber(0), nil
		}
	case lexer.Greater:
		c := inter.compareValues(left, right)
		if c > 0 {
			return awknumber(1), nil
		} else {
			return awknumber(0), nil
		}
	case lexer.GreaterEqual:
		c := inter.compareValues(left, right)
		if c >= 0 {
			return awknumber(1), nil
		} else {
			return awknumber(0), nil
		}
	}
	return null(), nil
}

func (inter *interpreter) evalBinaryBool(bb *parser.BinaryBoolExpr) (awkvalue, error) {
	var val awkvalue
	var err error
	switch bb.Op.Type {
	case lexer.DoubleAnd:
		val, err = inter.evalAnd(bb)
	case lexer.DoublePipe:
		val, err = inter.evalOr(bb)
	}
	return val, err
}

func (inter *interpreter) evalDollar(de *parser.DollarExpr) (awkvalue, error) {
	ind, err := inter.eval(de.Field)
	if err != nil {
		return null(), err
	}
	return inter.getField(int(ind.float())), nil
}

func (inter *interpreter) evalGetline(gl *parser.GetlineExpr) (awkvalue, error) {
	var err error
	var filestr string
	if gl.File != nil {
		file, err := inter.eval(gl.File)
		if err != nil {
			return null(), err
		}
		filestr = file.string(inter.getConvfmt())
	}
	var fetchRecord func() (string, error)
	switch gl.Op.Type {
	case lexer.Pipe:
		reader := inter.inprograms.get(filestr, inter.spawnInProgram)
		fetchRecord = func() (string, error) { return inter.nextRecord(reader) }
	case lexer.Less:
		reader := inter.infiles.get(filestr, spawnInFile)
		fetchRecord = func() (string, error) { return inter.nextRecord(reader) }
	default:
		fetchRecord = inter.nextRecordCurrentFile
	}
	var record string
	record, err = fetchRecord()
	retval := awknumber(0)
	if err == nil {
		retval.n = 1
	} else if err == io.EOF {
		retval.n = 0
	} else {
		retval.n = -1
	}
	recstr := awknumericstring(record)
	if gl.Variable != nil && retval.n > 0 {
		_, err := inter.evalAssignToLhs(gl.Variable, recstr)
		if err != nil {
			return null(), err
		}
	} else if retval.n > 0 {
		inter.setField(0, recstr)
	}
	return retval, nil
}

func (inter *interpreter) evalLength(le *parser.LengthExpr) (awkvalue, error) {
	var str string
	if le.Arg == nil {
		str = inter.toGoString(inter.getField(0))
	} else {
		v, err := inter.eval(le.Arg)
		if err != nil {
			return null(), err
		}
		str = inter.toGoString(v)
	}
	return awknumber(float64(len([]rune(str)))), nil
}

func (inter *interpreter) evalCall(ce *parser.CallExpr) (awkvalue, error) {
	f := inter.ftable[ce.Called.FunctionIndex]
	return f.Call(inter, ce.Called.Id, ce.Args)
}

func (inter *interpreter) evalIn(ine *parser.InExpr) (awkvalue, error) {
	var elem awkvalue
	var err error
	switch v := ine.Left.(type) {
	case parser.ExprList:
		elem, err = inter.evalIndex(v)
	default:
		elem, err = inter.eval(v)
	}
	if err != nil {
		return null(), err
	}
	v := inter.getVariable(ine.Right)
	if v.typ != Array {
		return awknumber(0), nil
	}
	str := inter.toGoString(elem)
	_, ok := v.array[str]
	if ok {
		return awknumber(1), nil
	} else {
		return awknumber(0), nil
	}
}

func (inter *interpreter) evalRegexExpr(re *parser.RegexExpr) (awkvalue, error) {
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
			Type:   lexer.Match,
			Line:   re.Regex.Line,
		},
		Right: re,
	}
	return inter.evalMatchExpr(expr)
}

func (inter *interpreter) evalMatchExpr(me *parser.MatchExpr) (awkvalue, error) {
	left, err := inter.eval(me.Left)
	if err != nil {
		return null(), err
	}
	right, err := inter.evalRegex(me.Right)
	if err != nil {
		return null(), err
	}
	var negate bool
	if me.Op.Type == lexer.NotMatch {
		negate = true
	}
	res := right.MatchString(inter.toGoString(left))
	if negate {
		res = !res
	}
	if res {
		return awknumber(1), nil
	} else {
		return awknumber(0), nil
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
		res, err := regexp.Compile(inter.toGoString(rev))
		if err != nil {
			return nil, inter.runtimeError(e.Token(), "invalid regular expression")
		}
		return res, nil
	}
}

func (inter *interpreter) evalAnd(bb *parser.BinaryBoolExpr) (awkvalue, error) {
	left, err := inter.eval(bb.Left)
	if err != nil {
		return null(), err
	}
	if !left.bool() {
		return awknumber(0), nil
	}
	right, err := inter.eval(bb.Right)
	if err != nil {
		return null(), err
	}
	if right.bool() {
		return awknumber(1), nil
	} else {
		return awknumber(0), nil
	}
}

func (inter *interpreter) evalOr(bb *parser.BinaryBoolExpr) (awkvalue, error) {
	left, err := inter.eval(bb.Left)
	if err != nil {
		return null(), err
	}
	if left.bool() {
		return awknumber(1), nil
	}
	right, err := inter.eval(bb.Right)
	if err != nil {
		return null(), err
	}
	if right.bool() {
		return awknumber(1), nil
	} else {
		return awknumber(0), nil
	}
}

func (inter *interpreter) compareValues(left, right awkvalue) float64 {
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
	return left.float() - right.float()
}

func (inter *interpreter) evalUnary(u *parser.UnaryExpr) (awkvalue, error) {
	right, err := inter.eval(u.Right)
	if err != nil {
		return null(), err
	}
	res := awknumber(0)
	switch u.Op.Type {
	case lexer.Minus:
		res.n = -right.float()
	case lexer.Plus:
		res.n = right.float()
	case lexer.Not:
		if right.bool() {
			res = awknumber(0)
		} else {
			res = awknumber(1)
		}
	}
	return res, nil
}

func (inter *interpreter) evalAssign(a *parser.AssignExpr) (awkvalue, error) {
	right, err := inter.eval(a.Right)
	if err != nil {
		return null(), err
	}
	res, err := inter.evalAssignToLhs(a.Left, right)
	return res, err
}

func (inter *interpreter) evalPreIncrement(pr *parser.PreIncrementExpr) (awkvalue, error) {
	_, ival, err := inter.evalIncrement(pr.IncrementExpr)
	if err != nil {
		return null(), err
	}
	return ival, nil
}

func (inter *interpreter) evalPostIncrement(po *parser.PostIncrementExpr) (awkvalue, error) {
	val, _, err := inter.evalIncrement(po.IncrementExpr)
	if err != nil {
		return null(), err
	}
	return val, nil
}

func (inter *interpreter) evalIncrement(inc *parser.IncrementExpr) (awkvalue, awkvalue, error) {
	varval, err := inter.eval(inc.Lhs)
	if err != nil {
		return null(), null(), err
	}
	val := awknumber(varval.float())
	ival := awknumber(0)
	switch inc.Op.Type {
	case lexer.Increment:
		ival.n = val.n + 1
	case lexer.Decrement:
		ival.n = val.n - 1
	}
	_, err = inter.evalAssignToLhs(inc.Lhs, ival)
	if err != nil {
		return null(), null(), err
	}
	return val, ival, nil
}

func (inter *interpreter) evalAssignToLhs(lhs parser.LhsExpr, val awkvalue) (awkvalue, error) {
	switch left := lhs.(type) {
	case *parser.IdExpr:
		if inter.getVariable(left).typ == Array {
			return null(), inter.runtimeError(left.Token(), "cannot use array in scalar context")
		}
		err := inter.setVariable(left, val)
		if err != nil {
			return null(), err
		}
	case *parser.DollarExpr:
		i, err := inter.eval(left.Field)
		if err != nil {
			return null(), err
		}
		inter.setField(int(i.float()), val)
	case *parser.IndexingExpr:
		arrval := inter.getVariable(left.Id)
		if arrval.typ == Null {
			arrval = awkarray(map[string]awkvalue{})
			inter.setVariable(left.Id, arrval)
		}
		if arrval.typ != Array {
			return null(), inter.runtimeError(left.Token(), "cannot index scalar variable")
		}
		ind, err := inter.evalIndex(left.Index)
		if err != nil {
			return null(), err
		}
		arrval.array[ind.str] = val
	}
	return val, nil
}

func (inter *interpreter) evalTernary(te *parser.TernaryExpr) (awkvalue, error) {
	cond, err := inter.eval(te.Cond)
	if err != nil {
		return null(), err
	}
	if cond.bool() {
		return inter.eval(te.Expr0)
	} else {
		return inter.eval(te.Expr1)
	}
}

func (inter *interpreter) evalId(i *parser.IdExpr) (awkvalue, error) {
	v := inter.getVariable(i)
	if v.typ == Array {
		return null(), inter.runtimeError(i.Token(), "cannot use array in scalar context")
	}
	return v, nil
}

func (inter *interpreter) evalIndexing(i *parser.IndexingExpr) (awkvalue, error) {
	v, err := inter.getArrayVariable(i.Id)
	if err != nil {
		return null(), err
	}
	index, err := inter.evalIndex(i.Index)
	if err != nil {
		return null(), err
	}
	res := v.array[index.str]
	return res, nil
}

func (inter *interpreter) evalIndex(ind []parser.Expr) (awkvalue, error) {
	indices := make([]string, 0, 5)
	for _, expr := range ind {
		res, err := inter.eval(expr)
		if err != nil {
			return null(), err
		}
		indices = append(indices, inter.toGoString(res))
	}
	return awknormalstring(strings.Join(indices, inter.toGoString(inter.builtins[parser.Subsep]))), nil
}

func (inter *interpreter) getField(i int) awkvalue {
	if i < 0 || i >= len(inter.fields) {
		return null()
	}
	return inter.fields[i]
}

func (inter *interpreter) setField(i int, v awkvalue) {
	if i >= 1 && i < len(inter.fields) {
		inter.fields[i] = awknumericstring(inter.toGoString(v))
		ofs := inter.toGoString(inter.builtins[parser.Ofs])
		sep := ""
		var res strings.Builder
		for _, field := range inter.fields[1:] {
			fmt.Fprintf(&res, "%s%s", sep, inter.toGoString(field))
			sep = ofs
		}
		inter.fields[0] = awknumericstring(res.String())
	} else if i >= len(inter.fields) {
		for i >= len(inter.fields) {
			inter.fields = append(inter.fields, null())
		}
		inter.setField(i, v)
	} else if i == 0 {
		str := inter.toGoString(v)
		inter.fields[0] = awknumericstring(str)
		inter.fields = inter.fields[0:1]
		splits, _ := inter.split(str, nil)
		for _, split := range splits {
			inter.fields = append(inter.fields, awknumericstring(split))
		}
		inter.builtins[parser.Nf] = awknumber(float64(len(inter.fields) - 1))
	}
}

func (inter *interpreter) toGoString(v awkvalue) string {
	return v.string(inter.getConvfmt())
}

func (inter *interpreter) getVariable(id *parser.IdExpr) awkvalue {
	if id.Index >= 0 {
		return inter.globals[id.Index]
	} else if id.Index < 0 && id.LocalIndex >= 0 {
		return inter.locals[id.LocalIndex]
	} else {
		return inter.builtins[id.BuiltinIndex]
	}
}

func (inter *interpreter) getArrayVariable(id *parser.IdExpr) (awkvalue, error) {
	v := inter.getVariable(id)
	switch v.typ {
	case Array:
		return v, nil
	case Null:
		err := inter.setVariable(id, awkarray(map[string]awkvalue{}))
		if err != nil {
			return null(), err
		}
		return inter.getArrayVariable(id)
	default:
		return null(), inter.runtimeError(id.Token(), "cannot use scalar in array context")
	}
}

func (inter *interpreter) setVariable(id *parser.IdExpr, v awkvalue) error {
	if id.Index >= 0 {
		inter.globals[id.Index] = v
	} else if id.Index < 0 && id.LocalIndex >= 0 {
		inter.locals[id.LocalIndex] = v
	} else {
		if id.BuiltinIndex == parser.Fs {
			return inter.setFs(id.Id, v)
		}
		inter.builtins[id.BuiltinIndex] = v
	}
	return nil
}

func (inter *interpreter) setFs(token lexer.Token, v awkvalue) error {
	str := v.string(inter.getConvfmt())
	restr := str
	if len(restr) <= 1 && restr != " " {
		restr = "[" + restr + "]"
	}
	re, err := regexp.Compile(restr)
	if err != nil {
		return inter.runtimeError(token, "invalid regular expression")
	}
	inter.builtins[parser.Fs] = awknormalstring(str)
	inter.fsregex = re
	return nil
}

func (inter *interpreter) getOfmt() string {
	return inter.builtins[parser.Ofmt].str
}

func (inter *interpreter) getConvfmt() string {
	return inter.builtins[parser.Convfmt].str
}

func (inter *interpreter) getRsStr() string {
	return inter.toGoString(inter.builtins[parser.Rs])
}

func (inter *interpreter) runtimeError(tok lexer.Token, msg string) error {
	return fmt.Errorf("at line %d (%s): runtime error: %s", tok.Line, tok.Lexeme, msg)
}

func (inter *interpreter) run() error {
	var skipNormals bool
	var errexit ErrorExit

	inter.currentFile = inter.stdinFile
	err := inter.runBegins()
	if ee, ok := err.(ErrorExit); ok {
		errexit = ee
		skipNormals = true
	} else if err != nil {
		return err
	}

	if !skipNormals {
		// nil to bootstrap walking through ARGV (zeroth file already at EOF)
		inter.currentFile = nil
		err := inter.runNormals()
		if ee, ok := err.(ErrorExit); ok {
			errexit = ee
		} else if err != nil {
			return err
		}
	}

	inter.currentFile = inter.stdinFile
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

	// No input file processed (that is, ARGC == 1 or every element of ARGV is "")
	if inter.currentFile == nil {
		inter.currentFile = inter.stdinFile
		return inter.runNormals()
	}

	return nil
}

func (inter *interpreter) processRecord(record string) error {
	inter.setField(0, awknormalstring(record))
	for i, normal := range inter.items.Normals {
		var toexecute bool
		switch pat := normal.Pattern.(type) {
		case *parser.ExprPattern:
			res, err := inter.eval(pat.Expr)
			if err != nil {
				return err
			}
			toexecute = res.bool()
		case *parser.RangePattern:
			if inter.rangematched[i] {
				res, err := inter.eval(pat.Expr1)
				if err != nil {
					return err
				}
				if res.bool() {
					delete(inter.rangematched, i)
				}
				toexecute = true
			} else {
				res, err := inter.eval(pat.Expr0)
				if err != nil {
					return err
				}
				if res.bool() {
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

func (inter *interpreter) initialize(params RunParams) {
	inter.items = params.ResolvedItems.Items

	// Stacks

	inter.builtins = make([]awkvalue, len(parser.Builtinvars))
	inter.initializeBuiltinVariables(params)

	inter.stack = make([]awkvalue, 10000)

	inter.globals = make([]awkvalue, len(params.ResolvedItems.Globalindices))
	inter.initializeGlobals(params)

	inter.ftable = make([]Callable, len(params.ResolvedItems.Functionindices))
	inter.initializeFunctions(params)

	// IO structures

	inter.outprograms = newOutWriters()
	inter.outfiles = newOutWriters()
	inter.inprograms = newInReaders()
	inter.infiles = newInReaders()
	inter.rng = NewRNG(0)
	inter.argindex = 0
	inter.stdin = params.Stdin
	inter.stdout = params.Stdout
	inter.stderr = params.Stderr
	inter.stdinFile = bufio.NewReader(inter.stdin)

	// Caches

	inter.rangematched = map[int]bool{}
	inter.fprintfcache = map[string][]func(awkvalue) interface{}{}
}

func (inter *interpreter) initializeBuiltinVariables(params RunParams) {
	// General
	inter.builtins[parser.Convfmt] = awknormalstring("%.6g")
	inter.builtins[parser.Fnr] = awknumber(0)
	inter.spaceregex = regexp.MustCompile(`\s+`)
	inter.setFs(lexer.Token{}, awknormalstring(params.Fs))
	inter.builtins[parser.Nr] = awknumber(0)
	inter.builtins[parser.Ofmt] = awknormalstring("%.6g")
	inter.builtins[parser.Ofs] = awknormalstring(" ")
	inter.builtins[parser.Ors] = awknormalstring("\n")
	inter.builtins[parser.Rs] = awknormalstring("\n")
	inter.builtins[parser.Subsep] = awknormalstring("\034")

	// ARGC and ARGV
	argc := len(params.Arguments) + 1
	argv := map[string]awkvalue{}
	argv["0"] = awknumericstring(params.Programname)
	for i := 1; i <= argc-1; i++ {
		argv[fmt.Sprintf("%d", i)] = awknumericstring(params.Arguments[i-1])
	}
	inter.builtins[parser.Argc] = awknumber(float64(argc))
	inter.builtins[parser.Argv] = awkarray(argv)

	// ENVIRON
	environ := awkarray(map[string]awkvalue{})
	for _, envpair := range os.Environ() {
		splits := strings.Split(envpair, "=")
		environ.array[splits[0]] = awknumericstring(splits[1])
	}
	inter.builtins[parser.Environ] = environ

	// Preassignment from command line
	for i, str := range params.Builtinpreassing {
		inter.builtins[i] = awknumericstring(str)
	}
}

func (inter *interpreter) initializeGlobals(params RunParams) {
	// Preassignment from command line
	for i, str := range params.Globalpreassign {
		inter.globals[i] = awknumericstring(str)
	}
}

func (inter *interpreter) initializeFunctions(params RunParams) {
	// Builtins
	for name, builtin := range builtinfuncs {
		if _, ok := lexer.Builtinfuncs[name]; !ok {
			log.Fatalf("function '%s' not listed in lexer.Builtinfuncs", name)
		}
		inter.ftable[params.ResolvedItems.Functionindices[name]] = builtin
	}

	// User defined
	for _, fi := range params.ResolvedItems.Functions {
		inter.ftable[params.ResolvedItems.Functionindices[fi.Name.Lexeme]] = &UserFunction{
			Arity: len(fi.Args),
			Body:  fi.Body,
		}
	}
}

func (inter *interpreter) cleanup() {
	inter.outprograms.closeAll()
	inter.outfiles.closeAll()
	inter.inprograms.closeAll()
	inter.infiles.closeAll()
}
