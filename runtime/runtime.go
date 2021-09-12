package runtime

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

type awkvalue interface {
	isValue()
}

type awknumber float64

func (n awknumber) isValue() {}

type awkstring string

func (s awkstring) isValue() {}

type awknumericstring string

func (s awknumericstring) isValue() {}

type awkarray map[string]awkvalue

func (a awkarray) isValue() {}

type environment map[string]awkvalue

func (env environment) get(k string) awkvalue {
	return env[k]
}

func (env environment) set(k string, v awkvalue) {
	env[k] = v
}

type interpreter struct {
	env environment
}

func (inter *interpreter) execute(stat parser.Stat) error {
	switch v := stat.(type) {
	case parser.StatList:
		return inter.executeStatList(v)
	case parser.ExprStat:
		return inter.executeExprStat(v)
	case parser.PrintStat:
		return inter.executePrintStat(v)
	}
	return nil
}

func (inter *interpreter) executeStatList(sl parser.StatList) error {
	for _, stat := range sl {
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
		inter.printValue(v)
		fmt.Print(sep)
		sep = " " // TODO: use OFS
	}
	fmt.Println()
	return nil
}

func (inter *interpreter) printValue(v awkvalue) {
	switch vv := v.(type) {
	case awknumber:
		fmt.Print(vv)
	case awkstring:
		fmt.Print(vv)
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
	case parser.GroupingExpr:
		val, err = inter.eval(v.Expr)
	case parser.NumberExpr:
		val = inter.parseNumber(v)
	case parser.StringExpr:
		val = awkstring(v.Str.Lexeme)
	case parser.AssignExpr:
		val, err = inter.evalAssign(v)
	case parser.IdExpr:
		val, err = inter.evalId(v)
	case parser.IndexingExpr:
		val, err = inter.evalIndexing(v)
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
		res = inter.toString(left) + inter.toString(right)
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
		strl := string(inter.toString(left))
		strr := string(inter.toString(right))
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
	}
	return res, nil
}

func (inter *interpreter) parseNumber(n parser.NumberExpr) awkvalue {
	v, _ := strconv.ParseFloat(n.Num.Lexeme, 64)
	return awknumber(v)
}

func (inter *interpreter) evalAssign(a parser.AssignExpr) (awkvalue, error) {
	var f func(awkvalue)
	switch left := a.Left.(type) {
	case parser.IdExpr:
		_, isarr := inter.env.get(left.Id.Lexeme).(awkarray)
		if isarr {
			return nil, inter.runtimeError(left.Id, "cannot use array in scalar context")
		}
		f = func(v awkvalue) {
			inter.env.set(left.Id.Lexeme, v)
		}
	case parser.IndexingExpr:
		val := inter.env.get(left.Id.Lexeme)
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
			arr[string(ind)] = v
		}
	}
	right, err := inter.eval(a.Right)
	if err != nil {
		return nil, err
	}
	f(right)
	return right, nil
}

func (inter *interpreter) evalId(i parser.IdExpr) (awkvalue, error) {
	return inter.env.get(i.Id.Lexeme), nil
}

func (inter *interpreter) evalIndexing(i parser.IndexingExpr) (awkvalue, error) {
	v := inter.env.get(i.Id.Lexeme)
	switch vv := v.(type) {
	case awkarray:
		index, err := inter.evalIndex(i.Index)
		if err != nil {
			return nil, err
		}
		return vv[string(index)], nil
	default:
		if v != nil {
			return nil, inter.runtimeError(i.Id, "cannot index a scalar value")
		}
		inter.env.set(i.Id.Lexeme, awkarray{})
		return nil, nil
	}
}

func (inter *interpreter) evalIndex(ind []parser.Expr) (awkstring, error) {
	sep := ""
	var buff strings.Builder
	for _, expr := range ind {
		res, err := inter.eval(expr)
		if err != nil {
			return awkstring(""), err
		}
		fmt.Fprintf(&buff, "%s%s", sep, inter.toString(res))
		sep = " " // TODO use SUBSEP
	}
	return awkstring(buff.String()), nil
}

func (inter *interpreter) toNumber(v awkvalue) awknumber {
	switch vv := v.(type) {
	case awknumber:
		return vv
	case awkstring:
		return awknumber(inter.stringToNumber(string(vv)))
	case awknumericstring:
		return awknumber(inter.stringToNumber(string(vv)))
	default:
		return awknumber(0)
	}
}

func (inter *interpreter) toString(v awkvalue) awkstring {
	switch vv := v.(type) {
	case awknumber:
		return awkstring(inter.numberToString(float64(vv)))
	case awkstring:
		return vv
	case awknumericstring:
		return awkstring(vv)
	default:
		return awkstring("")
	}
}

func (inter *interpreter) numberToString(n float64) string {
	if math.Trunc(n) == n {
		return fmt.Sprintf("%.6g", n) // TODO: use CONVFMT
	} else {
		return fmt.Sprintf("%d", int64(n))
	}
}

func (inter *interpreter) stringToNumber(s string) float64 {
	var f float64
	fmt.Sscan(s, &f)
	return f
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
	inter.env = environment{}
	items, begins := filterItems(items, func(item parser.Item) bool {
		return specialFilter(item, lexer.Begin)
	})
	items, ends := filterItems(items, func(item parser.Item) bool {
		return specialFilter(item, lexer.End)
	})

	for _, beg := range begins {
		patact := beg.(parser.PatternAction)
		err := inter.execute(patact.Action)
		if err != nil {
			return err
		}
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	for _, end := range ends {
		patact := end.(parser.PatternAction)
		err := inter.execute(patact.Action)
		if err != nil {
			return err
		}
	}
	return nil
}
