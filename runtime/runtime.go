package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"regexp"
	"strconv"
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

type awkvalue interface {
	isValue()
}

type awknumber float64

func (n awknumber) isValue() {}

type awkstring interface {
	isAwkString()
	fmt.Stringer
	awkvalue
}

type awknormalstring string

func (s awknormalstring) isValue()       {}
func (s awknormalstring) isAwkString()   {}
func (s awknormalstring) String() string { return string(s) }

type awknumericstring string

func (s awknumericstring) isValue()       {}
func (s awknumericstring) isAwkString()   {}
func (s awknumericstring) String() string { return string(s) }

type awkarray map[string]awkvalue

func (a awkarray) isValue() {}

type environment struct {
	locals  map[string]awkvalue
	calling *environment
}

func (env *environment) get(k string, globals map[string]awkvalue) awkvalue {
	if env != nil {
		v, ok := env.locals[k]
		if ok {
			return v
		}
	}
	return globals[k]
}

func (env *environment) set(k string, v awkvalue, globals map[string]awkvalue) {
	if env != nil {
		_, ok := env.locals[k]
		if ok {
			env.locals[k] = v
		}
	}
	globals[k] = v
}

func (env *environment) has(k string) bool {
	if env == nil {
		return false
	}
	_, ok := env.locals[k]
	return ok
}

func newEnvironment(calling *environment) *environment {
	return &environment{
		locals:  map[string]awkvalue{},
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
	globals      map[string]awkvalue
	ftable       map[string]Callable
	fields       []awkvalue
	outprograms  outwriters
	outfiles     outwriters
	currentFile  io.RuneReader
	inprograms   inreaders
	infiles      inreaders
	rng          RNG
	inprint      bool
	rangematched map[int]bool
}

func (inter *interpreter) execute(stat parser.Stat) error {
	switch v := stat.(type) {
	case parser.BlockStat:
		return inter.executeBlock(v)
	case parser.ExprStat:
		return inter.executeExpr(v)
	case parser.PrintStat:
		return inter.executePrint(v)
	case parser.IfStat:
		return inter.executeIf(v)
	case parser.ForStat:
		return inter.executeFor(v)
	case parser.ForEachStat:
		return inter.executeForEach(v)
	case parser.NextStat:
		return ErrNext
	case parser.BreakStat:
		return ErrBreak
	case parser.ContinueStat:
		return ErrContinue
	case parser.ReturnStat:
		return inter.executeReturn(v)
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

func (inter *interpreter) executeExpr(es parser.ExprStat) error {
	_, err := inter.eval(es.Expr)
	return err
}

func (inter *interpreter) executePrint(ps parser.PrintStat) error {
	inter.inprint = true
	defer func() { inter.inprint = false }()
	var w io.Writer
	w = os.Stdout
	if ps.File != nil {
		file, err := inter.eval(ps.File)
		if err != nil {
			return err
		}
		filestr := inter.toGoString(file)
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

func (inter *interpreter) executeSimplePrint(w io.Writer, ps parser.PrintStat) error {
	if ps.Exprs == nil {
		fmt.Fprintf(w, "%s", inter.toGoString(inter.getField(0)))
	} else {
		sep := ""
		for _, expr := range ps.Exprs {
			v, err := inter.eval(expr)
			if err != nil {
				return err
			}
			_, isarr := v.(awkarray)
			if isarr {
				return inter.runtimeError(ps.Token(), "cannot print array")
			}
			fmt.Fprintf(w, "%s%s", sep, inter.toGoString(v))
			sep = inter.toGoString(inter.globals["OFS"])
		}
	}
	fmt.Fprintf(w, "%s", inter.toGoString(inter.globals["ORS"]))
	return nil
}

func (inter *interpreter) executePrintf(w io.Writer, ps parser.PrintStat) error {
	err := inter.fprintf(w, ps.Print, ps.Exprs)
	return err
}

func (inter *interpreter) fprintfConversions(print lexer.Token, fmtstr string) ([]func(awkvalue) interface{}, error) {
	tostr := func(v awkvalue) interface{} {
		return inter.toGoString(v)
	}
	tofloat := func(v awkvalue) interface{} {
		return inter.toGoFloat(v)
	}
	toint := func(v awkvalue) interface{} {
		return int(inter.toGoFloat(v))
	}
	tochar := func(v awkvalue) interface{} {
		return []rune(inter.toGoString(v))[0]
	}

	res := make([]func(awkvalue) interface{}, 0)
	var toappend func(awkvalue) interface{}
	for i := 0; i < len(fmtstr); i++ {
		if fmtstr[i] != '%' {
			continue
		}
		i++
		if i >= len(fmtstr) {
			return nil, inter.runtimeError(print, "invalid format syntax: expected character after '%'")
		}
		switch fmtstr[i] {
		case '%':
			continue
		case 's':
			toappend = tostr
		case 'd', 'i', 'o', 'x', 'X':
			toappend = toint
		case 'f', 'e', 'E', 'g', 'G':
			toappend = tofloat
		case 'c':
			toappend = tochar
		default:
			return nil, inter.runtimeError(print, fmt.Sprintf("invalid format syntax: unknown verb %c", fmtstr[i]))
		}
		res = append(res, toappend)
	}
	return res, nil
}

func (inter *interpreter) fprintf(w io.Writer, print lexer.Token, exprs []parser.Expr) error {
	format, err := inter.eval(exprs[0])
	if err != nil {
		return err
	}
	formatstr := inter.toGoString(format)
	convs, err := inter.fprintfConversions(print, formatstr)
	if err != nil {
		return nil
	}
	if len(convs) > len(exprs[1:]) {
		return inter.runtimeError(print, "run out of arguments for formatted output")
	}
	args := make([]interface{}, 0)
	for _, expr := range exprs[1:] {
		arg, err := inter.eval(expr)
		if err != nil {
			return err
		}
		_, isarr := arg.(awkarray)
		if isarr {
			return inter.runtimeError(print, "cannot print array")
		}
		args = append(args, convs[0](arg))
		convs = convs[1:]
	}
	fmt.Fprintf(w, formatstr, args...)
	return nil
}

func (inter *interpreter) executeIf(ifs parser.IfStat) error {
	cond, err := inter.eval(ifs.Cond)
	if err != nil {
		return err
	}
	if isTruthy(cond) {
		err := inter.execute(ifs.Body)
		if err != nil {
			return err
		}
	} else if ifs.ElseBody != nil {
		err := inter.execute(ifs.ElseBody)
		if err != nil {
			return err
		}
	}
	return nil
}

func (inter *interpreter) executeFor(fs parser.ForStat) error {
	err := inter.execute(fs.Init)
	if err != nil {
		return err
	}
	for {
		cond, err := inter.eval(fs.Cond)
		if err != nil {
			return err
		}
		if !isTruthy(cond) {
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

func (inter *interpreter) executeForEach(fes parser.ForEachStat) error {
	arrv := inter.getVariable(fes.Array.Id.Lexeme)
	arr, isarr := arrv.(awkarray)
	if !isarr {
		return inter.runtimeError(fes.Array.Id, "cannot do for each on a non array")
	}
	for k := range arr {
		err := inter.setVariable(fes.Id.Id.Lexeme, fes.Id.Token(), awknormalstring(k))
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

func (inter *interpreter) executeReturn(rs parser.ReturnStat) error {
	v, err := inter.eval(rs.ReturnVal)
	if err != nil {
		return err
	}
	return errorReturn{
		val: v,
	}
}

func (inter *interpreter) eval(expr parser.Expr) (awkvalue, error) {
	var val awkvalue
	var err error
	switch v := expr.(type) {
	case parser.BinaryExpr:
		val, err = inter.evalBinary(v)
	case parser.UnaryExpr:
		val, err = inter.evalUnary(v)
	case parser.NumberExpr:
		val = inter.parseNumber(v)
	case parser.StringExpr:
		val = awknormalstring(v.Str.Lexeme)
	case parser.AssignExpr:
		val, err = inter.evalAssign(v)
	case parser.IdExpr:
		val, err = inter.evalId(v)
	case parser.IndexingExpr:
		val, err = inter.evalIndexing(v)
	case parser.PreIncrementExpr:
		val, err = inter.evalPreIncrement(v)
	case parser.PostIncrementExpr:
		val, err = inter.evalPostIncrement(v)
	case parser.TernaryExpr:
		val, err = inter.evalTernary(v)
	case parser.BinaryBoolExpr:
		val, err = inter.evalBinaryBool(v)
	case parser.DollarExpr:
		val, err = inter.evalDollar(v)
	case parser.GetlineExpr:
		val, err = inter.evalGetline(v)
	case parser.CallExpr:
		val, err = inter.evalCall(v)
	case parser.InExpr:
		val, err = inter.evalIn(v)
	case parser.MatchExpr:
		val, err = inter.evalMatchExpr(v)
	case parser.RegexExpr:
		val, err = inter.evalRegexExpr(v)
	}
	return val, err
}

func (inter *interpreter) evalBinary(b parser.BinaryExpr) (awkvalue, error) {
	left, err := inter.eval(b.Left)
	if err != nil {
		return nil, err
	}
	right, err := inter.eval(b.Right)
	if err != nil {
		return nil, err
	}
	_, arrl := left.(awkarray)
	_, arrr := right.(awkarray)
	if arrl || arrr {
		return nil, inter.runtimeError(b.Token(), "cannot use array in scalar context")
	}
	var res awkvalue
	err = nil
	switch b.Op.Type {
	case lexer.Plus:
		res = inter.toNumber(left) + inter.toNumber(right)
	case lexer.Minus:
		res = inter.toNumber(left) - inter.toNumber(right)
	case lexer.Star:
		res = inter.toNumber(left) * inter.toNumber(right)
	case lexer.Slash:
		if inter.toNumber(right) == 0 {
			err = inter.runtimeError(b.Right.Token(), "attempt to divide by 0")
			break
		}
		res = inter.toNumber(left) / inter.toNumber(right)
	case lexer.Percent:
		if inter.toNumber(right) == 0 {
			err = inter.runtimeError(b.Right.Token(), "attempt to divide by 0")
			break
		}
		res = awknumber(math.Mod(inter.toGoFloat(left), inter.toGoFloat(right)))
	case lexer.Caret:
		res = awknumber(math.Pow(inter.toGoFloat(left), inter.toGoFloat(right)))
	case lexer.Concat:
		res = awknormalstring(inter.toGoString(left) + inter.toGoString(right))
	case lexer.Equal:
		c := awknumber(inter.compareValues(left, right))
		if c == 0 {
			res = awknumber(1)
		} else {
			res = awknumber(0)
		}
	case lexer.NotEqual:
		c := awknumber(inter.compareValues(left, right))
		if c != 0 {
			res = awknumber(1)
		} else {
			res = awknumber(0)
		}
	case lexer.Less:
		c := awknumber(inter.compareValues(left, right))
		if c < 0 {
			res = awknumber(1)
		} else {
			res = awknumber(0)
		}
	case lexer.LessEqual:
		c := awknumber(inter.compareValues(left, right))
		if c <= 0 {
			res = awknumber(1)
		} else {
			res = awknumber(0)
		}
	case lexer.Greater:
		c := awknumber(inter.compareValues(left, right))
		if c > 0 {
			res = awknumber(1)
		} else {
			res = awknumber(0)
		}
	case lexer.GreaterEqual:
		c := awknumber(inter.compareValues(left, right))
		if c >= 0 {
			res = awknumber(1)
		} else {
			res = awknumber(0)
		}
	}
	return res, err
}

func (inter *interpreter) evalBinaryBool(bb parser.BinaryBoolExpr) (awkvalue, error) {
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

func (inter *interpreter) evalDollar(de parser.DollarExpr) (awkvalue, error) {
	ind, err := inter.eval(de.Field)
	if err != nil {
		return nil, err
	}
	return inter.getField(int(inter.toGoFloat(ind))), nil
}

func (inter *interpreter) evalGetline(gl parser.GetlineExpr) (awkvalue, error) {
	var err error
	var filestr string
	if gl.File != nil {
		file, err := inter.eval(gl.File)
		if err != nil {
			return nil, err
		}
		filestr = inter.toGoString(file)
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
	var retval awknumber
	if err == nil {
		retval = 1
	} else if err == io.EOF {
		retval = 0
	} else {
		retval = -1
	}
	recstr := awknumericstring(record)
	if gl.Variable != nil && retval > 0 {
		_, err := inter.evalAssignToLhs(gl.Variable, recstr)
		if err != nil {
			return nil, err
		}
	} else if retval > 0 {
		inter.setField(0, recstr)
	}
	return retval, nil
}

func (inter *interpreter) evalCall(ce parser.CallExpr) (awkvalue, error) {
	called := ce.Called
	if f, ok := inter.ftable[called.Lexeme]; ok {
		return f.Call(inter, ce.Called, ce.Args)
	}
	return nil, inter.runtimeError(ce.Token(), "cannot call non callable")
}

func (inter *interpreter) evalIn(ine parser.InExpr) (awkvalue, error) {
	var elem awkvalue
	var err error
	switch v := ine.Left.(type) {
	case parser.ExprList:
		elem, err = inter.evalIndex(v)
	default:
		elem, err = inter.eval(v)
	}
	if err != nil {
		return nil, err
	}
	v := inter.getVariable(ine.Right.Id.Lexeme)
	arr, isarr := v.(awkarray)
	if !isarr {
		return nil, inter.runtimeError(ine.Right.Token(), "cannot use 'in' for non array")
	}
	str := inter.toGoString(elem)
	_, ok := arr[str]
	if ok {
		return awknumber(1), nil
	} else {
		return awknumber(0), nil
	}
}

func (inter *interpreter) evalRegexExpr(re parser.RegexExpr) (awkvalue, error) {
	expr := parser.MatchExpr{
		Left: parser.DollarExpr{
			Dollar: lexer.Token{
				Lexeme: "$",
				Type:   lexer.Dollar,
				Line:   re.Regex.Line,
			},
			Field: parser.NumberExpr{
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

func (inter *interpreter) evalMatchExpr(me parser.MatchExpr) (awkvalue, error) {
	left, err := inter.eval(me.Left)
	if err != nil {
		return nil, err
	}
	right, err := inter.evalRegex(me.Right)
	if err != nil {
		return nil, err
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
	var regexstr string
	switch v := e.(type) {
	case parser.RegexExpr:
		regexstr = v.Regex.Lexeme
	default:
		rev, err := inter.eval(e)
		if err != nil {
			return nil, err
		}
		regexstr = inter.toGoString(rev)
	}
	res, err := regexp.Compile(regexstr)
	if err != nil {
		return nil, inter.runtimeError(e.Token(), "invalid regular expression")
	}
	return res, nil
}

func (inter *interpreter) evalAnd(bb parser.BinaryBoolExpr) (awkvalue, error) {
	left, err := inter.eval(bb.Left)
	if err != nil {
		return nil, err
	}
	if !isTruthy(left) {
		return awknumber(0), nil
	}
	right, err := inter.eval(bb.Right)
	if err != nil {
		return nil, err
	}
	if isTruthy(right) {
		return awknumber(1), nil
	} else {
		return awknumber(0), nil
	}
}

func (inter *interpreter) evalOr(bb parser.BinaryBoolExpr) (awkvalue, error) {
	left, err := inter.eval(bb.Left)
	if err != nil {
		return nil, err
	}
	if isTruthy(left) {
		return awknumber(1), nil
	}
	right, err := inter.eval(bb.Right)
	if err != nil {
		return nil, err
	}
	if isTruthy(right) {
		return awknumber(1), nil
	} else {
		return awknumber(0), nil
	}
}

func (inter *interpreter) compareValues(left, right awkvalue) float64 {
	_, nusl := left.(awknumericstring)
	_, nusr := right.(awknumericstring)

	_, nosl := left.(awknormalstring)
	_, nosr := right.(awknormalstring)

	/* Comparisons (with the '<', "<=", "!=", "==", '>', and ">=" operators)
	shall be made numerically if both operands are numeric, if one is
	numeric and the other has a string value that is a numeric string, or
	if one is numeric and the other has the uninitialized value. */
	if nosl || nosr || (left == nil && right == nil) || (nusl && nusr) {
		strl := inter.toString(left).String()
		strr := inter.toString(right).String()
		if strl == strr {
			return 0
		} else if strl < strr {
			return -1
		} else {
			return 1
		}
	}
	return float64(inter.toNumber(left) - inter.toNumber(right))
}

func (inter *interpreter) evalUnary(u parser.UnaryExpr) (awkvalue, error) {
	right, err := inter.eval(u.Right)
	if err != nil {
		return nil, err
	}
	_, arr := right.(awkarray)
	if arr {
		return nil, inter.runtimeError(u.Right.Token(), "cannot use array in scalar context")
	}
	var res awknumber
	switch u.Op.Type {
	case lexer.Minus:
		res = -inter.toNumber(right)
	case lexer.Plus:
		res = inter.toNumber(right)
	case lexer.Not:
		if isTruthy(right) {
			res = awknumber(0)
		} else {
			res = awknumber(1)
		}
	}
	return res, nil
}

func (inter *interpreter) parseNumber(n parser.NumberExpr) awkvalue {
	v, _ := strconv.ParseFloat(n.Num.Lexeme, 64)
	return awknumber(v)
}

func (inter *interpreter) evalAssign(a parser.AssignExpr) (awkvalue, error) {
	if id, isid := a.Left.(parser.IdExpr); isid {
		if _, ok := inter.ftable[id.Id.Lexeme]; ok && !inter.env.has(id.Id.Lexeme) {
			return nil, inter.runtimeError(id.Token(), "functions must be called")
		}
	}
	right, err := inter.eval(a.Right)
	if err != nil {
		return nil, err
	}
	_, isarr := right.(awkarray)
	if isarr {
		return nil, inter.runtimeError(a.Right.Token(), "cannot use array in scalar context")
	}
	res, err := inter.evalAssignToLhs(a.Left, right)
	return res, err
}

func (inter *interpreter) evalPreIncrement(pr parser.PreIncrementExpr) (awkvalue, error) {
	_, ival, err := inter.evalIncrement(pr.IncrementExpr)
	if err != nil {
		return nil, err
	}
	return ival, nil
}

func (inter *interpreter) evalPostIncrement(po parser.PostIncrementExpr) (awkvalue, error) {
	val, _, err := inter.evalIncrement(po.IncrementExpr)
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (inter *interpreter) evalIncrement(inc parser.IncrementExpr) (awkvalue, awkvalue, error) {
	varval, err := inter.eval(inc.Lhs)
	if err != nil {
		return nil, nil, err
	}
	val := inter.toNumber(varval)
	var ival awkvalue
	switch inc.Op.Type {
	case lexer.Increment:
		ival = val + 1
	case lexer.Decrement:
		ival = val - 1
	}
	_, err = inter.evalAssignToLhs(inc.Lhs, ival)
	if err != nil {
		return nil, nil, err
	}
	return val, ival, nil
}

func (inter *interpreter) evalAssignToLhs(lhs parser.LhsExpr, val awkvalue) (awkvalue, error) {
	var f func(awkvalue)
	switch left := lhs.(type) {
	case parser.IdExpr:
		_, isarr := inter.getVariable(left.Id.Lexeme).(awkarray)
		if isarr {
			return nil, inter.runtimeError(left.Token(), "cannot use array in scalar context")
		}
		f = func(v awkvalue) {
			inter.setVariable(left.Id.Lexeme, left.Token(), v)
		}
	case parser.DollarExpr:
		i, err := inter.evalDollar(left)
		if err != nil {
			return nil, err
		}
		f = func(v awkvalue) {
			inter.setField(int(inter.toGoFloat(i)), v)
		}
	case parser.IndexingExpr:
		val := inter.getVariable(left.Id.Lexeme)
		if val == nil {
			val = awkarray{}
			inter.setVariable(left.Id.Lexeme, left.Token(), val)
		}
		arr, isarr := val.(awkarray)
		if !isarr {
			return nil, inter.runtimeError(left.Token(), "cannot index scalar variable")
		}
		ind, err := inter.evalIndex(left.Index)
		if err != nil {
			return nil, err
		}
		f = func(v awkvalue) {
			arr[ind.String()] = v
		}
	}
	f(val)
	return val, nil
}

func (inter *interpreter) evalTernary(te parser.TernaryExpr) (awkvalue, error) {
	var res awkvalue
	var err error
	cond, err := inter.eval(te.Cond)
	if err != nil {
		return nil, err
	}
	if isTruthy(cond) {
		res, err = inter.eval(te.Expr0)
	} else {
		res, err = inter.eval(te.Expr1)
	}
	return res, err
}

func (inter *interpreter) evalId(i parser.IdExpr) (awkvalue, error) {
	if _, ok := inter.ftable[i.Id.Lexeme]; ok && !inter.env.has(i.Id.Lexeme) {
		return nil, inter.runtimeError(i.Token(), "functions must be called")
	}
	return inter.getVariable(i.Id.Lexeme), nil
}

func (inter *interpreter) evalIndexing(i parser.IndexingExpr) (awkvalue, error) {
	v := inter.getVariable(i.Id.Lexeme)
	switch vv := v.(type) {
	case awkarray:
		index, err := inter.evalIndex(i.Index)
		if err != nil {
			return nil, err
		}
		res := vv[index.String()]
		vv[index.String()] = res
		return res, nil
	default:
		if v != nil {
			return nil, inter.runtimeError(i.Token(), "cannot index a scalar value")
		}
		err := inter.setVariable(i.Id.Lexeme, i.Token(), awkarray{})
		if err != nil {
			return nil, err
		}
		return inter.evalIndexing(i)
	}
}

func (inter *interpreter) evalIndex(ind []parser.Expr) (awkstring, error) {
	sep := ""
	var buff strings.Builder
	for _, expr := range ind {
		res, err := inter.eval(expr)
		if err != nil {
			return awknormalstring(""), err
		}
		fmt.Fprintf(&buff, "%s%s", sep, inter.toString(res))
		sep = inter.toGoString(inter.getVariable("SUBSEP"))
	}
	return awknormalstring(buff.String()), nil
}

func (inter *interpreter) toNumber(v awkvalue) awknumber {
	switch vv := v.(type) {
	case awknumber:
		return vv
	case awkstring:
		return awknumber(inter.stringToNumber(vv.String()))
	default:
		return awknumber(0)
	}
}

func (inter *interpreter) toString(v awkvalue) awkstring {
	switch vv := v.(type) {
	case awknumber:
		return awknormalstring(inter.numberToString(float64(vv)))
	case awkstring:
		return vv
	default:
		return awknormalstring("")
	}
}

func (inter *interpreter) toGoString(v awkvalue) string {
	astr := inter.toString(v)
	var res string
	switch sub := astr.(type) {
	case awknormalstring:
		res = string(sub)
	case awknumericstring:
		res = string(sub)
	}
	return res
}

func (inter *interpreter) toGoFloat(v awkvalue) float64 {
	af := inter.toNumber(v)
	return float64(af)
}

func (inter *interpreter) numberToString(n float64) string {
	if math.Trunc(n) == n {
		return fmt.Sprintf("%d", int64(n))
	} else if inter.inprint {
		return fmt.Sprintf(inter.toGoString(inter.getVariable("OFMT")), n)
	} else {
		return fmt.Sprintf(inter.toGoString(inter.getVariable("CONVFMT")), n)
	}
}

func (inter *interpreter) stringToNumber(s string) float64 {
	var f float64
	fmt.Sscan(s, &f)
	return f
}

func (inter *interpreter) processInputRecord(text string) {
	inter.setField(0, awknormalstring(text))
}

func (inter *interpreter) getField(i int) awkvalue {
	if i >= len(inter.fields) {
		return nil
	}
	return inter.fields[i]
}

func (inter *interpreter) setField(i int, v awkvalue) {
	if i >= 1 && i < len(inter.fields) {
		inter.fields[i] = awknumericstring(inter.toGoString(v))
		ofs := inter.toGoString(inter.getVariable("OFS"))
		sep := ""
		var res strings.Builder
		for _, field := range inter.fields[1:] {
			fmt.Fprintf(&res, "%s%s", sep, inter.toGoString(field))
			sep = ofs
		}
		inter.fields[0] = awknumericstring(res.String())
	} else if i >= len(inter.fields) {
		for i >= len(inter.fields) {
			inter.fields = append(inter.fields, nil)
		}
		inter.setField(i, v)
	} else if i == 0 {
		str := inter.toGoString(v)
		inter.fields[0] = awknumericstring(str)
		inter.fields = inter.fields[0:1]
		restr := inter.toGoString(inter.getVariable("FS"))
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
		inter.setVariable("NF", lexer.Token{}, awknumber(len(inter.fields)-1))
	}
}

func (inter *interpreter) getVariable(name string) awkvalue {
	return inter.env.get(name, inter.globals)
}

func (inter *interpreter) setVariable(name string, token lexer.Token, v awkvalue) error {
	if name == "FS" {
		return inter.setFs(token, v)
	}
	inter.env.set(name, v, inter.globals)
	return nil
}

func (inter *interpreter) initBuiltInVars(paths []string) {
	inter.globals["CONVFMT"] = awknormalstring("%.6g")
	inter.globals["FILENAME"] = nil
	inter.globals["FS"] = awknormalstring(" ")
	inter.globals["NF"] = nil
	inter.globals["NR"] = nil
	inter.globals["OFMT"] = awknormalstring("%.6g")
	inter.globals["OFS"] = awknormalstring(" ")
	inter.globals["ORS"] = awknormalstring("\n")
	inter.globals["RS"] = awknormalstring("\n")
	inter.globals["SUBSEP"] = awknormalstring("\034")

	argc := len(paths) + 1
	argv := map[string]awkvalue{}
	argv["0"] = awknumericstring(os.Args[0])
	for i := 1; i <= argc-1; i++ {
		argv[fmt.Sprintf("%d", i)] = awknumericstring(paths[i-1])
	}
	inter.globals["ARGC"] = awknumber(argc)
	inter.globals["ARGV"] = awkarray(argv)

	environ := awkarray{}
	for _, envpair := range os.Environ() {
		splits := strings.Split(envpair, "=")
		environ[splits[0]] = awknumericstring(splits[1])
	}
	inter.globals["ENVIRON"] = environ
}

func (inter *interpreter) setFs(token lexer.Token, v awkvalue) error {
	str := inter.toGoString(v)
	_, err := regexp.Compile(str)
	if err != nil {
		return inter.runtimeError(token, "invalid regular expression")
	}
	inter.globals["FS"] = awknormalstring(str)
	return nil
}

func (inter *interpreter) initialize(paths []string, functions []parser.Item) error {
	inter.globals = map[string]awkvalue{}
	inter.env = nil
	inter.initBuiltInVars(paths)
	inter.outprograms = newOutWriters()
	inter.outfiles = newOutWriters()
	inter.inprograms = newInReaders()
	inter.infiles = newInReaders()
	inter.rng = NewRNG(0)
	inter.currentFile = bufio.NewReader(os.Stdin)
	inter.rangematched = map[int]bool{}

	inter.ftable = map[string]Callable{}

	for name, native := range builtins {
		inter.ftable[name] = native
	}

	for _, item := range functions {
		fi := item.(parser.FunctionDef)
		if _, ok := inter.ftable[fi.Name.Lexeme]; ok {
			return inter.runtimeError(fi.Name, "function already defined")
		}
		inter.ftable[fi.Name.Lexeme] = AwkFunction(fi)
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

func isTruthy(val awkvalue) bool {
	switch v := val.(type) {
	case awkstring:
		return v.String() != ""
	case awknumber:
		return v != 0
	default:
		return false
	}
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
	patact, ok := item.(parser.PatternAction)
	if !ok {
		return false
	}
	pat, ok := patact.Pattern.(parser.SpecialPattern)
	if !ok {
		return false
	}
	if pat.Type.Type != ttype {
		return false
	}
	return true
}

func Run(items []parser.Item, paths []string) error {
	var inter interpreter

	items, functions := filterItems(items, func(item parser.Item) bool {
		switch item.(type) {
		case parser.FunctionDef:
			return true
		}
		return false
	})

	items, begins := filterItems(items, func(item parser.Item) bool {
		return specialFilter(item, lexer.Begin)
	})

	items, normals := filterItems(items, func(item parser.Item) bool {
		switch v := item.(type) {
		case parser.PatternAction:
			switch v.Pattern.(type) {
			case parser.ExprPattern:
				return true
			case parser.RangePattern:
				return true
			}
		}
		return false
	})

	_, ends := filterItems(items, func(item parser.Item) bool {
		return specialFilter(item, lexer.End)
	})

	inter.initialize(paths, functions)

	for _, beg := range begins {
		patact := beg.(parser.PatternAction)
		err := inter.execute(patact.Action)
		if err != nil {
			return err
		}
	}

	err := inter.processNormals(normals, paths)
	if err != nil {
		return err
	}

	for _, end := range ends {
		patact := end.(parser.PatternAction)
		err := inter.execute(patact.Action)
		if err != nil {
			return err
		}
	}

	inter.cleanup()

	return nil
}

func (inter *interpreter) processNormals(normals []parser.Item, paths []string) error {
	if len(normals) == 0 {
		return nil
	}

	inter.globals["NR"] = awknumber(1)
	argv := inter.globals["ARGV"].(awkarray)
	processed := false
	for i := 1; i <= int(inter.toGoFloat(inter.globals["ARGC"])); i++ {
		filename := inter.toGoString(argv[fmt.Sprintf("%d", i)])
		if i == int(inter.toGoFloat(inter.globals["ARGC"])) && !processed || filename == "-" {
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
	rs := inter.toGoString(inter.globals["RS"])
	if rs == "" {
		rs = "\n"
	}
	runes := []rune(rs)
	return runes[0]
}

func (inter *interpreter) processFile(normals []parser.Item, f *os.File) error {
	inter.globals["FILENAME"] = awknormalstring(f.Name())
	inter.currentFile = bufio.NewReader(f)
outer:
	for inter.globals["FNR"] = awknumber(1); ; inter.globals["NR"], inter.globals["FNR"] = inter.toNumber(inter.globals["NR"])+1, inter.toNumber(inter.globals["FNR"])+1 {
		text, err := nextRecord(inter.currentFile, inter.getRsRune())
		if err != nil {
			return err
		}
		inter.processInputRecord(text)
		for i, normal := range normals {
			patact := normal.(parser.PatternAction)
			var toexecute bool
			switch pat := patact.Pattern.(type) {
			case parser.ExprPattern:
				res, err := inter.eval(pat.Expr)
				if err != nil {
					return err
				}
				toexecute = isTruthy(res)
			case parser.RangePattern:
				if inter.rangematched[i] {
					res, err := inter.eval(pat.Expr1)
					if err != nil {
						return err
					}
					if isTruthy(res) {
						delete(inter.rangematched, i)
					}
					toexecute = true
				} else {
					res, err := inter.eval(pat.Expr0)
					if err != nil {
						return err
					}
					if isTruthy(res) {
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
