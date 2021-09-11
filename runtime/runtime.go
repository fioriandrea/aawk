package runtime

import (
	"fmt"
	"math"
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

func (inter *interpreter) execute(stat parser.StatNode) {
	switch v := stat.(type) {
	case parser.StatListNode:
		inter.executeStatList(v)
	case parser.ExprStatNode:
		inter.executeExprStat(v)
	}
}

func (inter *interpreter) executeStatList(sl parser.StatListNode) {
	for _, stat := range sl.Stats {
		inter.execute(stat)
	}
}

func (inter *interpreter) executeExprStat(es parser.ExprStatNode) {
	fmt.Println(inter.eval(es.Expr))
}

func (inter *interpreter) eval(expr parser.ExprNode) value {
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

func Run(syntaxTree parser.Node) {
	var inter interpreter
	inter.env = environment{}
	switch v := syntaxTree.(type) {
	case parser.ExprNode:
		fmt.Println(inter.eval(v))
	case parser.StatNode:
		inter.execute(v)
	}
}
