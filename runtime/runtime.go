package runtime

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"

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

type awkuninitialized struct {
	awkvalue
}

type environment map[string]awkvalue

type interpreter struct {
	env environment
}

func (inter *interpreter) execute(stat parser.Stat) {
	switch v := stat.(type) {
	case parser.StatList:
		inter.executeStatList(v)
	case parser.ExprStat:
		inter.executeExprStat(v)
	case parser.PrintStat:
		inter.executePrintStat(v)
	}
}

func (inter *interpreter) executeStatList(sl parser.StatList) {
	for _, stat := range sl {
		inter.execute(stat)
	}
}

func (inter *interpreter) executeExprStat(es parser.ExprStat) {
	inter.eval(es.Expr)
}

func (inter *interpreter) executePrintStat(ps parser.PrintStat) {
	sep := ""
	for _, expr := range ps.Exprs {
		inter.printValue(inter.eval(expr))
		fmt.Print(sep)
		sep = " " // TODO: use OFS
	}
	fmt.Println()
}

func (inter *interpreter) printValue(v awkvalue) {
	switch vv := v.(type) {
	case awknumber:
		fmt.Print(vv)
	case awkstring:
		fmt.Print(vv)
	case awkuninitialized:
	}
}

func (inter *interpreter) eval(expr parser.Expr) awkvalue {
	switch v := expr.(type) {
	case parser.BinaryExpr:
		return inter.evalBinary(v)
	case parser.UnaryExpr:
		return inter.evalUnary(v)
	case parser.GroupingExpr:
		return inter.eval(v.Expr)
	case parser.NumberExpr:
		return inter.parseNumber(v)
	case parser.StringExpr:
		return awkstring(v.Str.Lexeme)
	case parser.AssignExpr:
		return inter.evalAssign(v)
	case parser.IdExpr:
		return inter.evalId(v)
	}
	return nil
}

func (inter *interpreter) evalBinary(b parser.BinaryExpr) awkvalue {
	left := inter.eval(b.Left)
	right := inter.eval(b.Right)
	var res awkvalue
	switch b.Op.Type {
	case lexer.Plus:
		res = inter.toNumber(left) + inter.toNumber(right)
	case lexer.Minus:
		res = inter.toNumber(left) - inter.toNumber(right)
	case lexer.Star:
		res = inter.toNumber(left) * inter.toNumber(right)
	case lexer.Slash:
		res = inter.toNumber(left) / inter.toNumber(right)
	case lexer.Caret:
		res = awknumber(math.Pow(float64(inter.toNumber(left)), float64(inter.toNumber(right))))
	case lexer.Concat:
		res = inter.toString(left) + inter.toString(right)
	}
	return res
}

func (inter *interpreter) evalUnary(u parser.UnaryExpr) awkvalue {
	right := inter.eval(u.Right).(awknumber)
	var res awknumber
	switch u.Op.Type {
	case lexer.Minus:
		res = -right
	case lexer.Plus:
		res = right
	}
	return res
}

func (inter *interpreter) parseNumber(n parser.NumberExpr) awkvalue {
	v, _ := strconv.ParseFloat(n.Num.Lexeme, 64)
	return awknumber(v)
}

func (inter *interpreter) evalAssign(a parser.AssignExpr) awkvalue {
	left := a.Left.(parser.IdExpr)
	right := inter.eval(a.Right)
	inter.env[left.Id.Lexeme] = right
	return right
}

func (inter *interpreter) evalId(i parser.IdExpr) awkvalue {
	v, ok := inter.env[i.Id.Lexeme]
	if !ok {
		v = awkuninitialized{}
	}
	return v
}

func (inter *interpreter) toNumber(v awkvalue) awknumber {
	switch vv := v.(type) {
	case awknumber:
		return vv
	case awkstring:
		return awknumber(inter.stringToNumber(string(vv)))
	case awkuninitialized:
		return awknumber(0)
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
	case awkuninitialized:
		return awkstring("")
	default:
		return awkstring("")
	}
}

func (inter *interpreter) numberToString(n float64) string {
	return fmt.Sprintf("%.6g", n) // TODO: use CONVFMT
}

func (inter *interpreter) stringToNumber(s string) float64 {
	var f float64
	fmt.Sscan(s, &f)
	return f
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

func Run(items []parser.Item) {
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
		inter.execute(patact.Action)
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	for _, end := range ends {
		patact := end.(parser.PatternAction)
		inter.execute(patact.Action)
	}
}
