package parser

import (
	"fmt"
	"os"

	"github.com/fioriandrea/aawk/lexer"
)

type Node interface {
	isNode()
}

type ExprNode interface {
	isExprNode()
	Node
}

type BinaryExpr struct {
	Left  ExprNode
	Op    lexer.Token
	Right ExprNode
	ExprNode
}

type UnaryExpr struct {
	Op    lexer.Token
	Right ExprNode
	ExprNode
}

type NumberExpr struct {
	Num lexer.Token
	ExprNode
}

type GroupingExpr struct {
	Expr ExprNode
	ExprNode
}

type AssignExpr struct {
	Left  LhsExprNode
	Right ExprNode
	ExprNode
}

type LhsExprNode interface {
	isLhsExprNode()
	ExprNode
}

type IdExpr struct {
	Id lexer.Token
	LhsExprNode
}

type StatNode interface {
	isStatNode()
	Node
}

type ExprStatNode struct {
	Expr ExprNode
	StatNode
}

type StatListNode struct {
	Stats []StatNode
	StatNode
}

type parser struct {
	tokens   chan lexer.Token
	current  lexer.Token
	previous lexer.Token
}

func GetSyntaxTree(tokens chan lexer.Token) Node {
	ps := parser{
		tokens: tokens,
	}
	ps.advance()
	return ps.statList()
}

func (ps *parser) statList() StatListNode {
	stats := make([]StatNode, 0)
	for ps.current.Type != lexer.Eof {
		stats = append(stats, ps.stat())
		ps.eatError("expected end of statement", lexer.Eof, lexer.Semicolon, lexer.Newline)
	}
	return StatListNode{Stats: stats}
}

func (ps *parser) stat() StatNode {
	switch ps.current.Type {
	default:
		return ps.exprStat()
	}
}

func (ps *parser) exprStat() StatNode {
	expr := ps.expr()
	return ExprStatNode{Expr: expr}
}

func (ps *parser) expr() ExprNode {
	return ps.assignExpr()
}

func (ps *parser) assignExpr() ExprNode {
	left := ps.addExpr()
	if ps.eat(lexer.Assign) {
		lhs, ok := left.(LhsExprNode)
		if !ok {
			ps.parseErrorAtCurrent("cannot assign to a non left hand side")
			return nil
		}
		return AssignExpr{
			Left:  lhs,
			Right: ps.expr(),
		}
	}
	return left
}

func (ps *parser) addExpr() ExprNode {
	left := ps.mulExpr()
	if ps.eat(lexer.Plus, lexer.Minus) {
		op := ps.previous
		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: ps.mulExpr(),
		}
	}
	return left
}

func (ps *parser) mulExpr() ExprNode {
	left := ps.expExpr()
	if ps.eat(lexer.Star, lexer.Slash) {
		op := ps.previous
		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: ps.expExpr(),
		}
	}
	return left
}

func (ps *parser) expExpr() ExprNode {
	left := ps.unaryExpr()
	if ps.eat(lexer.Caret) {
		op := ps.previous
		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: ps.expr(),
		}
	}
	return left
}

func (ps *parser) unaryExpr() ExprNode {
	if ps.eat(lexer.Increment, lexer.Decrement, lexer.Plus, lexer.Minus) {
		op := ps.previous
		return UnaryExpr{
			Op:    op,
			Right: ps.termExpr(),
		}
	}
	return ps.termExpr()
}

func (ps *parser) termExpr() ExprNode {
	defer ps.advance()
	switch ps.current.Type {
	case lexer.Number:
		return NumberExpr{
			Num: ps.current,
		}
	case lexer.LeftParen:
		return ps.groupingExpr()
	case lexer.Identifier:
		return IdExpr{
			Id: ps.current,
		}
	default:
		ps.parseErrorAtCurrent("unexpected token")
		return nil
	}
}

func (ps *parser) groupingExpr() ExprNode {
	ps.advance()
	toret := GroupingExpr{
		Expr: ps.expr(),
	}
	ps.checkError("expected ')'", lexer.RightParen)
	return toret
}

func (ps *parser) parseErrorAtCurrent(msg string) {
	fmt.Errorf("%s: at %d: %s\n", os.Args[0], ps.current.Line, msg)
}

func (ps *parser) advance() {
	t := <-ps.tokens
	ps.previous = ps.current
	ps.current = t
}

func (ps *parser) check(types ...lexer.TokenType) bool {
	for _, tt := range types {
		if tt == ps.current.Type {
			return true
		}
	}
	return false
}

func (ps *parser) checkError(msg string, types ...lexer.TokenType) bool {
	if !ps.check(types...) {
		ps.parseErrorAtCurrent(msg)
		return false
	}
	return true
}

func (ps *parser) eatError(msg string, types ...lexer.TokenType) bool {
	if !ps.eat(types...) {
		ps.parseErrorAtCurrent(msg)
		return false
	}
	return true
}

func (ps *parser) eat(types ...lexer.TokenType) bool {
	if ps.check(types...) {
		ps.advance()
		return true
	}
	return false
}
