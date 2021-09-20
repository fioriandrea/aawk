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

var ErrNext = errors.New("next")
var ErrBreak = errors.New("break")
var ErrContinue = errors.New("continue")

type errorReturn struct {
	val awkvalue
}

func (er errorReturn) Error() string {
	return "return"
}

type ErrorExit struct {
	Status int
}

func (ee ErrorExit) Error() string {
	return "exit"
}

type environment struct {
	locals  []awkvalue
	calling *environment
}

func newEnvironment(calling *environment, locals []awkvalue) *environment {
	return &environment{
		locals:  locals,
		calling: calling,
	}
}

type RNG struct {
	*rand.Rand
	rngseed int64
}

func (r *RNG) SetSeed(i int64) {
	r.rngseed = i
	r.Seed(i)
}

func NewRNG(seed int64) RNG {
	return RNG{
		Rand:    rand.New(rand.NewSource(seed)),
		rngseed: seed,
	}
}

type interpreter struct {
	env          *environment
	ftable       []Callable
	builtins     []awkvalue
	fields       []awkvalue
	globals      []awkvalue
	outprograms  outwriters
	outfiles     outwriters
	currentFile  io.RuneReader
	inprograms   inreaders
	infiles      inreaders
	rng          RNG
	inprint      bool
	rangematched map[int]bool
	fprintfcache map[string]func() (string, []func(awkvalue) interface{})
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
		return ErrNext
	case *parser.BreakStat:
		return ErrBreak
	case *parser.ContinueStat:
		return ErrContinue
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
	inter.inprint = true
	defer func() { inter.inprint = false }()
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
			w = inter.outprograms.get(filestr, spawnOutProgram)
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
		fmt.Fprintf(w, "%s", inter.getField(0).string(inter.getConvfmt()))
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
			fmt.Fprintf(w, "%s%s", sep, v.string(inter.getConvfmt()))
			sep = inter.builtins[lexer.Ofs].string(inter.getConvfmt())
		}
	}
	fmt.Fprintf(w, "%s", inter.builtins[lexer.Ors].string(inter.getConvfmt()))
	return nil
}

func (inter *interpreter) executePrintf(w io.Writer, ps *parser.PrintStat) error {
	err := inter.fprintf(w, ps.Print, ps.Exprs)
	return err
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
		if err == ErrBreak {
			break
		} else if err != nil && err != ErrContinue {
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
		if err == ErrBreak {
			break
		} else if err != nil && err != ErrContinue {
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
	return errorReturn{
		val: v,
	}
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
		v := inter.getVariable(lhs.Id)
		if v.typ == Null {
			inter.setVariable(lhs.Id, awkarray(map[string]awkvalue{}))
			v = inter.getVariable(lhs.Id)
		}
		if v.typ != Array {
			return inter.runtimeError(lhs.Token(), "cannot index a scalar")
		}
		ind, err := inter.evalIndex(lhs.Index)
		if err != nil {
			return err
		}
		delete(v.array, ind.string(inter.getConvfmt()))
		return nil
	case *parser.IdExpr:
		v := inter.getVariable(lhs)
		if v.typ != Array && v.typ != Null {
			return inter.runtimeError(lhs.Token(), "cannot index a scalar")
		}
		if v.typ == Array {
			inter.setVariable(lhs, awkarray(map[string]awkvalue{}))
		}
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

func (inter *interpreter) evalBinary(b *parser.BinaryExpr) (awkvalue, error) {
	left, err := inter.eval(b.Left)
	if err != nil {
		return null(), err
	}
	right, err := inter.eval(b.Right)
	if err != nil {
		return null(), err
	}
	arrl := left.typ == Array
	arrr := right.typ == Array
	if arrl || arrr {
		return null(), inter.runtimeError(b.Token(), "cannot use array in scalar context")
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
		return awknormalstring(left.string(inter.getConvfmt()) + right.string(inter.getConvfmt())), nil
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
	var reader io.RuneReader
	switch gl.Op.Type {
	case lexer.Pipe:
		reader = inter.inprograms.get(filestr, spawnInProgram)
	case lexer.Less:
		reader = inter.infiles.get(filestr, spawnInFile)
	default:
		reader = inter.currentFile
	}
	var record string
	record, err = nextRecord(reader, inter.getRsRune())
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
	str := elem.string(inter.getConvfmt())
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
	res := right.MatchString(left.string(inter.getConvfmt()))
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
	var regexstr string
	switch v := e.(type) {
	case *parser.RegexExpr:
		regexstr = v.Regex.Lexeme
	default:
		rev, err := inter.eval(e)
		if err != nil {
			return nil, err
		}
		regexstr = rev.string(inter.getConvfmt())
	}
	res, err := regexp.Compile(regexstr)
	if err != nil {
		return nil, inter.runtimeError(e.Token(), "invalid regular expression")
	}
	return res, nil
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
		format := inter.getConvfmt()
		strl := left.string(format)
		strr := right.string(format)
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
	if right.typ == Array {
		return null(), inter.runtimeError(u.Right.Token(), "cannot use array in scalar context")
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
		inter.setVariable(left, val)
	case *parser.DollarExpr:
		i, err := inter.evalDollar(left)
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
	return inter.getVariable(i), nil
}

func (inter *interpreter) evalIndexing(i *parser.IndexingExpr) (awkvalue, error) {
	v := inter.getVariable(i.Id)
	switch v.typ {
	case Array:
		index, err := inter.evalIndex(i.Index)
		if err != nil {
			return null(), err
		}
		res := v.array[index.str]
		v.array[index.str] = res
		return res, nil
	default:
		if v.typ != Null {
			return null(), inter.runtimeError(i.Token(), "cannot index a scalar value")
		}
		err := inter.setVariable(i.Id, awkarray(map[string]awkvalue{}))
		if err != nil {
			return null(), err
		}
		return inter.evalIndexing(i)
	}
}

func (inter *interpreter) evalIndex(ind []parser.Expr) (awkvalue, error) {
	sep := ""
	var buff strings.Builder
	format := inter.getConvfmt()
	for _, expr := range ind {
		res, err := inter.eval(expr)
		if err != nil {
			return awknormalstring(""), err
		}
		fmt.Fprintf(&buff, "%s%s", sep, res.string(format))
		sep = inter.builtins[lexer.Subsep].string(format)
	}
	return awknormalstring(buff.String()), nil
}

func (inter *interpreter) processInputRecord(text string) {
	inter.setField(0, awknormalstring(text))
}

func (inter *interpreter) getField(i int) awkvalue {
	if i < 0 || i >= len(inter.fields) {
		return null()
	}
	return inter.fields[i]
}

func (inter *interpreter) setField(i int, v awkvalue) {
	format := inter.getConvfmt()
	if i >= 1 && i < len(inter.fields) {
		inter.fields[i] = awknumericstring(v.string(format))
		ofs := inter.builtins[lexer.Ofs].string(format)
		sep := ""
		var res strings.Builder
		for _, field := range inter.fields[1:] {
			fmt.Fprintf(&res, "%s%s", sep, field.string(format))
			sep = ofs
		}
		inter.fields[0] = awknumericstring(res.String())
	} else if i >= len(inter.fields) {
		for i >= len(inter.fields) {
			inter.fields = append(inter.fields, null())
		}
		inter.setField(i, v)
	} else if i == 0 {
		str := v.string(format)
		inter.fields[0] = awknumericstring(str)
		inter.fields = inter.fields[0:1]
		restr := inter.builtins[lexer.Fs].string(format)
		if len(restr) == 1 {
			if restr == " " {
				restr = "\\s+"
				str = strings.TrimSpace(str)
			} else {
				restr = "[" + restr + "]"
			}
		}
		re := regexp.MustCompile(restr)
		for _, split := range re.Split(str, -1) {
			inter.fields = append(inter.fields, awknumericstring(split))
		}
		inter.builtins[lexer.Nf] = awknumber(float64(len(inter.fields) - 1))
	}
}

func (inter *interpreter) getVariable(id *parser.IdExpr) awkvalue {
	if id.Index >= 0 {
		return inter.globals[id.Index]
	} else if id.Index < 0 && id.LocalIndex >= 0 {
		return inter.env.locals[id.LocalIndex]
	} else {
		return inter.builtins[id.BuiltinIndex]
	}
}

func (inter *interpreter) setVariable(id *parser.IdExpr, v awkvalue) error {
	if id.Index >= 0 {
		inter.globals[id.Index] = v
	} else if id.Index < 0 && id.LocalIndex >= 0 {
		inter.env.locals[id.LocalIndex] = v
	} else {
		if id.BuiltinIndex == lexer.Fs {
			return inter.setFs(id.Id, v)
		}
		inter.builtins[id.BuiltinIndex] = v
	}
	return nil
}

func (inter *interpreter) initBuiltInVars(paths []string) {
	inter.builtins[lexer.Convfmt] = awknormalstring("%.6g")
	inter.builtins[lexer.Filename] = awknormalstring("")
	inter.builtins[lexer.Fs] = awknormalstring(" ")
	inter.builtins[lexer.Nf] = awknormalstring("")
	inter.builtins[lexer.Nr] = awknormalstring("")
	inter.builtins[lexer.Ofmt] = awknormalstring("%.6g")
	inter.builtins[lexer.Ofs] = awknormalstring(" ")
	inter.builtins[lexer.Ors] = awknormalstring("\n")
	inter.builtins[lexer.Rs] = awknormalstring("\n")
	inter.builtins[lexer.Subsep] = awknormalstring("\034")

	argc := len(paths) + 1
	argv := map[string]awkvalue{}
	argv["0"] = awknumericstring(os.Args[0])
	for i := 1; i <= argc-1; i++ {
		argv[fmt.Sprintf("%d", i)] = awknumericstring(paths[i-1])
	}
	inter.builtins[lexer.Argc] = awknumber(float64(argc))
	inter.builtins[lexer.Argv] = awkarray(argv)

	environ := awkarray(map[string]awkvalue{})
	for _, envpair := range os.Environ() {
		splits := strings.Split(envpair, "=")
		environ.array[splits[0]] = awknumericstring(splits[1])
	}
	inter.builtins[lexer.Environ] = environ
}

func (inter *interpreter) initialize(paths []string, functions []parser.Item, globalindices map[string]int, functionindices map[string]int) error {
	inter.builtins = make([]awkvalue, len(lexer.Builtinvars))
	inter.env = nil
	inter.globals = make([]awkvalue, len(globalindices))
	inter.initBuiltInVars(paths)
	inter.outprograms = newOutWriters()
	inter.outfiles = newOutWriters()
	inter.inprograms = newInReaders()
	inter.infiles = newInReaders()
	inter.rng = NewRNG(0)
	inter.currentFile = bufio.NewReader(os.Stdin)
	inter.rangematched = map[int]bool{}
	inter.fprintfcache = map[string]func() (string, []func(awkvalue) interface{}){}

	inter.ftable = make([]Callable, len(functionindices))

	for name, native := range Builtins {
		inter.ftable[functionindices[name]] = native
	}

	for _, item := range functions {
		fi := item.(*parser.FunctionDef)
		inter.ftable[functionindices[fi.Name.Lexeme]] = AwkFunction(*fi) // TODO
	}

	return nil
}

func (inter *interpreter) cleanup() {
	inter.outprograms.closeAll()
	inter.outfiles.closeAll()
	inter.inprograms.closeAll()
	inter.infiles.closeAll()
}

func (inter *interpreter) runtimeError(tok lexer.Token, msg string) error {
	return fmt.Errorf("%s: at line %d (%s): %s", os.Args[0], tok.Line, tok.Lexeme, msg)
}

func filterItems(items []parser.Item, filterOut func(parser.Item) bool) ([]parser.Item, []parser.Item) {
	newitems := items[:0]
	res := make([]parser.Item, 0)
	for _, item := range items {
		if filterOut(item) {
			res = append(res, item)
		} else {
			newitems = append(newitems, item)
		}
	}
	return newitems, res
}

func specialFilter(item parser.Item, ttype lexer.TokenType) bool {
	patact, ok := item.(*parser.PatternAction)
	if !ok {
		return false
	}
	pat, ok := patact.Pattern.(*parser.SpecialPattern)
	if !ok {
		return false
	}
	if pat.Type.Type != ttype {
		return false
	}
	return true
}

func Run(items []parser.Item, paths []string, globalindices map[string]int, functionindices map[string]int) error {
	var inter interpreter
	var errexit ErrorExit
	var skipNormals bool

	items, functions := filterItems(items, func(item parser.Item) bool {
		switch item.(type) {
		case *parser.FunctionDef:
			return true
		}
		return false
	})

	items, begins := filterItems(items, func(item parser.Item) bool {
		return specialFilter(item, lexer.Begin)
	})

	items, normals := filterItems(items, func(item parser.Item) bool {
		switch v := item.(type) {
		case *parser.PatternAction:
			switch v.Pattern.(type) {
			case *parser.ExprPattern:
				return true
			case *parser.RangePattern:
				return true
			}
		}
		return false
	})

	_, ends := filterItems(items, func(item parser.Item) bool {
		return specialFilter(item, lexer.End)
	})

	inter.initialize(paths, functions, globalindices, functionindices)

	for _, beg := range begins {
		patact := beg.(*parser.PatternAction)
		err := inter.execute(patact.Action)
		if ee, ok := err.(ErrorExit); ok {
			errexit = ee
			skipNormals = true
			break
		} else if err != nil {
			return err
		}
	}

	if !skipNormals {
		err := inter.processNormals(normals, paths)
		if ee, ok := err.(ErrorExit); ok {
			errexit = ee
		} else if err != nil {
			return err
		}
	}

	for _, end := range ends {
		patact := end.(*parser.PatternAction)
		err := inter.execute(patact.Action)
		if ee, ok := err.(ErrorExit); ok {
			errexit = ee
			break
		} else if err != nil {
			return err
		}
	}

	inter.cleanup()

	return errexit
}

func (inter *interpreter) processNormals(normals []parser.Item, paths []string) error {
	if len(normals) == 0 {
		return nil
	}

	inter.builtins[lexer.Nr] = awknumber(1)
	argv := inter.builtins[lexer.Argv]
	processed := false
	for i := 1; i <= int(inter.builtins[lexer.Argc].float()); i++ {
		filename := argv.array[fmt.Sprintf("%d", i)].string(inter.getConvfmt())
		if i == int(inter.builtins[lexer.Argc].float()) && !processed || filename == "-" {
			filename = "-"
			err := inter.processFile(normals, os.Stdin)
			if err != nil && err != io.EOF {
				return err
			}
		} else if filename == "" {
			continue
		} else {
			processed = true
			file, err := os.Open(filename)
			if err != nil {
				return err
			}
			err = inter.processFile(normals, file)
			if err != nil && err != io.EOF {
				return err
			}
			err = file.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (inter *interpreter) getRsRune() rune {
	rs := inter.builtins[lexer.Rs].string(inter.getConvfmt())
	if rs == "" {
		rs = "\n"
	}
	runes := []rune(rs)
	return runes[0]
}

func (inter *interpreter) processFile(normals []parser.Item, f *os.File) error {
	inter.builtins[lexer.Filename] = awknormalstring(f.Name())
	inter.currentFile = bufio.NewReader(f)
outer:
	for inter.builtins[lexer.Fnr] = awknumber(1); ; inter.builtins[lexer.Nr], inter.builtins[lexer.Fnr] = awknumber(inter.builtins[lexer.Nr].float()+1), awknumber(inter.builtins[lexer.Fnr].float()+1) {
		text, err := nextRecord(inter.currentFile, inter.getRsRune())
		if err != nil {
			return err
		}
		inter.processInputRecord(text)
		for i, normal := range normals {
			patact := normal.(*parser.PatternAction)
			var toexecute bool
			switch pat := patact.Pattern.(type) {
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
				err := inter.execute(patact.Action)
				if err == ErrNext {
					continue outer
				}
				if err != nil {
					return err
				}
			}
		}
	}
}

func (inter *interpreter) setFs(token lexer.Token, v awkvalue) error {
	str := v.string(inter.getConvfmt())
	_, err := regexp.Compile(str)
	if err != nil {
		return inter.runtimeError(token, "invalid regular expression")
	}
	inter.builtins[lexer.Fs] = awknormalstring(str)
	return nil
}

func (inter *interpreter) getOfmt() string {
	return inter.builtins[lexer.Ofmt].str
}

func (inter *interpreter) getConvfmt() string {
	return inter.builtins[lexer.Convfmt].str
}
