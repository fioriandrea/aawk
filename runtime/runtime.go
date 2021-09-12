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

type value interface {
	isValue()
}

type number struct {
	n float64
	value
}

type environment map[string]value

type interpreter struct {
	env environment
}

func (inter *interpreter) execute(stat parser.Stat) {
	switch v := stat.(type) {
	case parser.StatList:
		inter.executeStatList(v)
	case parser.ExprStat:
		inter.executeExprStat(v)
	}
}

func (inter *interpreter) executeStatList(sl parser.StatList) {
	for _, stat := range sl {
		inter.execute(stat)
	}
}

func (inter *interpreter) executeExprStat(es parser.ExprStat) {
	fmt.Println(inter.eval(es.Expr))
}

func (inter *interpreter) eval(expr parser.Expr) value {
	switch v := expr.(type) {
	case parser.BinaryExpr:
		return inter.evalBinary(v)
	case parser.UnaryExpr:
		return inter.evalUnary(v)
	case parser.GroupingExpr:
		return inter.eval(v.Expr)
	case parser.NumberExpr:
		return inter.parseNumber(v)
	case parser.AssignExpr:
		return inter.evalAssign(v)
	case parser.IdExpr:
		return inter.evalId(v)
	}
	return nil
}

func (inter *interpreter) evalBinary(b parser.BinaryExpr) value {
	left := inter.eval(b.Left).(number)
	right := inter.eval(b.Right).(number)
	res := number{}
	switch b.Op.Type {
	case lexer.Plus:
		res.n = left.n + right.n
	case lexer.Minus:
		res.n = left.n - right.n
	case lexer.Star:
		res.n = left.n * right.n
	case lexer.Slash:
		res.n = left.n / right.n
	case lexer.Caret:
		res.n = math.Pow(left.n, right.n)
	}
	return res
}

func (inter *interpreter) evalUnary(u parser.UnaryExpr) value {
	right := inter.eval(u.Right).(number)
	res := number{}
	switch u.Op.Type {
	case lexer.Minus:
		res.n = -right.n
	case lexer.Plus:
		res.n = right.n
	}
	return res
}

func (inter *interpreter) parseNumber(n parser.NumberExpr) value {
	v, _ := strconv.ParseFloat(n.Num.Lexeme, 64)
	return number{n: v}
}

func (inter *interpreter) evalAssign(a parser.AssignExpr) value {
	left := a.Left.(parser.IdExpr)
	right := inter.eval(a.Right)
	inter.env[left.Id.Lexeme] = right
	return right
}

func (inter *interpreter) evalId(i parser.IdExpr) value {
	v, ok := inter.env[i.Id.Lexeme]
	if !ok {
		v = number{n: 0}
	}
	return v
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
