package parser

import (
	"fmt"
	"os"

	"github.com/fioriandrea/aawk/lexer"
)

type Node interface {
	isNode()
}

type BinaryExpr struct {
	Left  Node
	Op    lexer.Token
	Right Node
	Node
}

type UnaryExpr struct {
	Op    lexer.Token
	Right Node
	Node
}

type NumberExpr struct {
	Num lexer.Token
	Node
}

type GroupingExpr struct {
	Expr Node
	Node
}

type NodeError struct {
	msg string
	Node
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
	return ps.expr()
}

func (ps *parser) expr() Node {
	return ps.addExpr()
}

func (ps *parser) addExpr() Node {
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

func (ps *parser) mulExpr() Node {
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

func (ps *parser) expExpr() Node {
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

func (ps *parser) unaryExpr() Node {
	if ps.eat(lexer.Increment, lexer.Decrement, lexer.Plus, lexer.Minus) {
		op := ps.previous
		return UnaryExpr{
			Op:    op,
			Right: ps.termExpr(),
		}
	}
	return ps.termExpr()
}

func (ps *parser) termExpr() Node {
	defer ps.advance()
	switch ps.current.Type {
	case lexer.Number:
		return NumberExpr{
			Num: ps.current,
		}
	case lexer.LeftParen:
		return ps.groupingExpr()
	default:
		return ps.parseErrorAtCurrent("unexpected token")
	}
}

func (ps *parser) groupingExpr() Node {
	ps.advance()
	toret := GroupingExpr{
		Expr: ps.expr(),
	}
	ps.checkError("expected ')'", lexer.RightParen)
	return toret
}

func (ps *parser) parseErrorAtCurrent(msg string) Node {
	toret := NodeError{
		msg: fmt.Sprintf("%s: at %d: %s\n", os.Args[0], ps.current.Line, msg),
	}
	fmt.Errorf("%s", toret.msg)
	return toret
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

func (ps *parser) eat(types ...lexer.TokenType) bool {
	if ps.check(types...) {
		ps.advance()
		return true
	}
	return false
}
