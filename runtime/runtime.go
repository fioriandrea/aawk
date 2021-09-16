package runtime

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

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

type environment map[string]awkvalue

func (env environment) get(k string) awkvalue {
	return env[k]
}

func (env environment) set(k string, v awkvalue) {
	env[k] = v
}

type outfile struct {
	stdin       chan string
	closeErrors chan error
}

func newOutFile() outfile {
	return outfile{
		stdin:       make(chan string),
		closeErrors: make(chan error),
	}
}

type outprograms struct {
	progs map[string]outfile
	wg    sync.WaitGroup
}

func newOutPrograms() outprograms {
	return outprograms{
		progs: map[string]outfile{},
	}
}

func (op outprograms) get(name string) chan string {
	prog, ok := op.progs[name]
	if ok {
		return prog.stdin
	}
	cmd := exec.Command("sh", "-c", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	prog = newOutFile()
	op.wg.Add(1)
	go func() {
		for str := range prog.stdin {
			io.WriteString(stdin, str)
		}
		if err := stdin.Close(); err != nil {
			prog.closeErrors <- err
		}
		if err := cmd.Wait(); err != nil {
			prog.closeErrors <- err
		}
		close(prog.closeErrors)
		op.wg.Done()
	}()
	op.progs[name] = prog
	return prog.stdin
}

func closeOutfile(of outfile) int {
	close(of.stdin)
	success := 0
	for err := range of.closeErrors {
		if err != nil {
			success = 1
		}
	}
	return success
}

func closeOutfileInMap(m map[string]outfile, name string) int {
	of, ok := m[name]
	if !ok {
		return 1
	}
	delete(m, name)
	return closeOutfile(of)
}

func (op outprograms) close(name string) int {
	return closeOutfileInMap(op.progs, name)
}

func (op outprograms) closeAll() {
	for name, _ := range op.progs {
		op.close(name)
	}
	op.wg.Wait()
}

type outfiles map[string]outfile

func (of outfiles) get(name string, mode int) chan string {
	res, ok := of[name]
	if ok {
		return res.stdin
	}
	f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY|mode, 0600)
	if err != nil {
		log.Fatal(err)
	}
	res.stdin = make(chan string)
	go func() {
		for str := range res.stdin {
			if _, err = f.WriteString(str); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
		}
		res.closeErrors <- f.Close()
		close(res.closeErrors)
	}()
	return res.stdin
}

func (of outfiles) getOverwrite(name string) chan string {
	return of.get(name, 0)
}

func (of outfiles) getAppend(name string) chan string {
	return of.get(name, os.O_APPEND)
}

func (of outfiles) close(name string) int {
	return closeOutfileInMap(map[string]outfile(of), name)
}

type interpreter struct {
	env         environment
	builtins    map[string]awkvalue
	fields      []awkvalue
	stdoutchan  chan string
	outprograms outprograms
	outfiles    outfiles
}

func (inter *interpreter) execute(stat parser.Stat) error {
	switch v := stat.(type) {
	case parser.BlockStat:
		return inter.executeBlockStat(v)
	case parser.ExprStat:
		return inter.executeExprStat(v)
	case parser.PrintStat:
		return inter.executePrintStat(v)
	case parser.IfStat:
		return inter.executeIfStat(v)
	case parser.ForStat:
		return inter.executeForStat(v)
	}
	return nil
}

func (inter *interpreter) executeBlockStat(bs parser.BlockStat) error {
	for _, stat := range bs {
		err := inter.execute(stat)
		if err != nil {
			return err
		}
	}
	return nil
}

func (inter *interpreter) executeExprStat(es parser.ExprStat) error {
	_, err := inter.eval(es.Expr)
	return err
}

func (inter *interpreter) executePrintStat(ps parser.PrintStat) error {
	outchan := inter.stdoutchan
	if ps.File != nil {
		file, err := inter.eval(ps.File)
		if err != nil {
			return err
		}
		filestr := inter.toGoString(file)
		switch ps.RedirOp.Type {
		case lexer.Pipe:
			outchan = inter.outprograms.get(filestr)
		case lexer.Greater:
			outchan = inter.outfiles.getOverwrite(filestr)
		case lexer.DoubleGreater:
			outchan = inter.outfiles.getAppend(filestr)
		}
	}
	switch ps.Print.Type {
	case lexer.Print:
		return inter.executeSimplePrintStat(outchan, ps)
	case lexer.Printf:
		return inter.executePrintfStat(outchan, ps)
	}
	return nil
}

func (inter *interpreter) executeSimplePrintStat(outchan chan string, ps parser.PrintStat) error {
	var res strings.Builder
	if ps.Exprs == nil {
		fmt.Fprintf(&res, "%s", inter.toGoString(inter.getField(0)))
	} else {
		sep := ""
		for _, expr := range ps.Exprs {
			v, err := inter.eval(expr)
			if err != nil {
				return err
			}
			_, isarr := v.(awkarray)
			if isarr {
				return inter.runtimeError(ps.Print, "cannot print array")
			}
			fmt.Fprintf(&res, "%s%s", sep, inter.toGoString(v))
			sep = inter.toGoString(inter.builtins["OFS"])
		}
	}
	fmt.Fprintf(&res, "%s", inter.toGoString(inter.builtins["ORS"]))
	outchan <- res.String()
	return nil
}

func (inter *interpreter) sprintfConversions(print lexer.Token, fmtstr string) ([]func(awkvalue) interface{}, error) {
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

func (inter *interpreter) sprintf(print lexer.Token, exprs []parser.Expr) (string, error) {
	format, err := inter.eval(exprs[0])
	if err != nil {
		return "", err
	}
	formatstr := inter.toGoString(format)
	convs, err := inter.sprintfConversions(print, formatstr)
	if err != nil {
		return "", nil
	}
	if len(convs) > len(exprs[1:]) {
		return "", inter.runtimeError(print, "run out of arguments for formatted output")
	}
	args := make([]interface{}, 0)
	for _, expr := range exprs[1:] {
		arg, err := inter.eval(expr)
		if err != nil {
			return "", err
		}
		_, isarr := arg.(awkarray)
		if isarr {
			return "", inter.runtimeError(print, "cannot print array")
		}
		args = append(args, convs[0](arg))
		convs = convs[1:]
	}
	return fmt.Sprintf(formatstr, args...), nil
}

func (inter *interpreter) executePrintfStat(outchan chan string, ps parser.PrintStat) error {
	str, err := inter.sprintf(ps.Print, ps.Exprs)
	if err != nil {
		return err
	}
	outchan <- str
	return nil
}

func (inter *interpreter) executeIfStat(ifs parser.IfStat) error {
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

func (inter *interpreter) executeForStat(fs parser.ForStat) error {
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
		if err != nil {
			return err
		}
		err = inter.execute(fs.Inc)
		if err != nil {
			return err
		}
	}
	return nil
}

func (inter *interpreter) eval(expr parser.Expr) (awkvalue, error) {
	var val awkvalue
	var err error
	switch v := expr.(type) {
	case parser.BinaryExpr:
		val, err = inter.evalBinary(v)
	case parser.UnaryExpr:
		val, err = inter.evalUnary(v)
	case parser.GroupingExpr:
		val, err = inter.eval(v.Expr)
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
	case parser.CloseExpr:
		val, err = inter.evalClose(v)
	case parser.SprintfExpr:
		val, err = inter.evalSprintf(v)
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
		return nil, inter.runtimeError(b.Op, "cannot use array in scalar context")
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
			err = inter.runtimeError(b.Op, "attempt to divide by 0")
			break
		}
		res = inter.toNumber(left) / inter.toNumber(right)
	case lexer.Percent:
		if inter.toNumber(right) == 0 {
			err = inter.runtimeError(b.Op, "attempt to divide by 0")
			break
		}
		res = awknumber(math.Mod(float64(inter.toNumber(left)), float64(inter.toNumber(right))))
	case lexer.Caret:
		res = awknumber(math.Pow(float64(inter.toNumber(left)), float64(inter.toNumber(right))))
	case lexer.Concat:
		res = awknormalstring(inter.toString(left).String() + inter.toString(right).String())
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

func (inter *interpreter) evalClose(ce parser.CloseExpr) (awkvalue, error) {
	file, err := inter.eval(ce.File)
	if err != nil {
		return nil, err
	}
	str := inter.toGoString(file)
	pr := inter.outprograms.close(str)
	fi := inter.outfiles.close(str)
	return awknumber(pr + fi), nil
}

func (inter *interpreter) evalSprintf(spe parser.SprintfExpr) (awkvalue, error) {
	str, err := inter.sprintf(spe.Sprintf, spe.Exprs)
	if err != nil {
		return nil, err
	}
	return awknormalstring(str), nil
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

func (inter *interpreter) compareValues(left, right awkvalue) int {
	_, nsl := left.(awknumericstring)
	_, nsr := right.(awknumericstring)

	_, sl := left.(awkstring)
	_, sr := right.(awkstring)

	/* Comparisons (with the '<', "<=", "!=", "==", '>', and ">=" operators)
	shall be made numerically if both operands are numeric, if one is
	numeric and the other has a string value that is a numeric string, or
	if one is numeric and the other has the uninitialized value. */
	if sl || sr || (left == nil && right == nil) || (nsl && nsr) {
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
	return int(inter.toNumber(left)) - int(inter.toNumber(right))
}

func (inter *interpreter) evalUnary(u parser.UnaryExpr) (awkvalue, error) {
	right, err := inter.eval(u.Right)
	if err != nil {
		return nil, err
	}
	_, arr := right.(awkarray)
	if arr {
		return nil, inter.runtimeError(u.Op, "cannot use array in scalar context")
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
	right, err := inter.eval(a.Right)
	if err != nil {
		return nil, err
	}
	_, isarr := right.(awkarray)
	if isarr {
		return nil, inter.runtimeError(a.Equal, "cannot use array in scalar context")
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
			return nil, inter.runtimeError(left.Id, "cannot use array in scalar context")
		}
		f = func(v awkvalue) {
			inter.env.set(left.Id.Lexeme, v)
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
			inter.env.set(left.Id.Lexeme, awkarray{})
		}
		arr, isarr := inter.env.get(left.Id.Lexeme).(awkarray)
		if !isarr {
			return nil, inter.runtimeError(left.Id, "cannot index scalar variable")
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
		return vv[index.String()], nil
	default:
		if v != nil {
			return nil, inter.runtimeError(i.Id, "cannot index a scalar value")
		}
		return nil, inter.setVariable(i.Id.Lexeme, i.Id, awkarray{})
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
	res, _ = strconv.Unquote(fmt.Sprintf("\"%s\"", res))
	return res
}

func (inter *interpreter) toGoFloat(v awkvalue) float64 {
	af := inter.toNumber(v)
	return float64(af)
}

func (inter *interpreter) numberToString(n float64) string {
	if math.Trunc(n) == n {
		return fmt.Sprintf(inter.toGoString(inter.getVariable("CONVFMT")), n)
	} else {
		return fmt.Sprintf("%d", int64(n))
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
			inter.setField(i, v)
		}
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
	res, ok := inter.builtins[name]
	if ok {
		return res
	}
	return inter.env.get(name)
}

func (inter *interpreter) setVariable(name string, token lexer.Token, v awkvalue) error {
	_, ok := inter.builtins[name]
	if ok {
		if name == "FS" {
			return inter.setFs(token, v)
		}
		inter.builtins[name] = v
		return nil
	}
	inter.env.set(name, v)
	return nil
}

func (inter *interpreter) initBuiltInVars() {
	inter.builtins = map[string]awkvalue{
		"CONVFMT": awknormalstring("%.6g"),
		"FS":      awknormalstring(" "),
		"NF":      nil,
		"NR":      nil,
		"OFS":     awknormalstring(" "),
		"ORS":     awknormalstring("\\n"),
		"SUBSEP":  awknormalstring("\\034"),
	}
}

func (inter *interpreter) setFs(token lexer.Token, v awkvalue) error {
	str := inter.toGoString(v)
	_, err := regexp.Compile(str)
	if err != nil {
		return inter.runtimeError(token, "invalid regular expression")
	}
	inter.builtins["FS"] = awknormalstring(str)
	return nil
}

func (inter *interpreter) initialize() {
	inter.env = environment{}
	inter.initBuiltInVars()
	inter.stdoutchan = make(chan string)
	go func() {
		for str := range inter.stdoutchan {
			fmt.Print(str)
		}
	}()
	inter.outprograms = newOutPrograms()
}

func (inter *interpreter) cleanup() {
	inter.outprograms.closeAll()
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

func Run(items []parser.Item) error {
	var inter interpreter

	inter.initialize()

	items, begins := filterItems(items, func(item parser.Item) bool {
		return specialFilter(item, lexer.Begin)
	})

	items, normals := filterItems(items, func(item parser.Item) bool {
		switch v := item.(type) {
		case parser.PatternAction:
			switch v.Pattern.(type) {
			case parser.ExprPattern:
				return true
			}
		}
		return false
	})

	_, ends := filterItems(items, func(item parser.Item) bool {
		return specialFilter(item, lexer.End)
	})

	for _, beg := range begins {
		patact := beg.(parser.PatternAction)
		err := inter.execute(patact.Action)
		if err != nil {
			return err
		}
	}

	if len(normals) > 0 {
		scanner := bufio.NewScanner(os.Stdin) // TODO: use RS
		inter.builtins["NR"] = awknumber(1)
		for scanner.Scan() {
			text := scanner.Text()
			inter.processInputRecord(text)
			for _, normal := range normals {
				patact := normal.(parser.PatternAction)
				pat := patact.Pattern.(parser.ExprPattern)
				res, err := inter.eval(pat.Expr)
				if err != nil {
					return err
				}
				if isTruthy(res) {
					err := inter.execute(patact.Action)
					if err != nil {
						return err
					}
				}
			}
			inter.builtins["NR"] = inter.toNumber(inter.builtins["NR"]) + 1
		}
		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
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
