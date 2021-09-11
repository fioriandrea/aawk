package parser

import (
	"fmt"
	"os"

	"github.com/fioriandrea/aawk/lexer"
)

type Node interface {
	isNode()
}

type Expr interface {
	isExpr()
	Node
}

type BinaryExpr struct {
	Left  Expr
	Op    lexer.Token
	Right Expr
	Expr
}

type UnaryExpr struct {
	Op    lexer.Token
	Right Expr
	Expr
}

type NumberExpr struct {
	Num lexer.Token
	Expr
}

type GroupingExpr struct {
	InnerExpr Expr
	Expr
}

type AssignExpr struct {
	Left  LhsExpr
	Right Expr
	Expr
}

type LhsExpr interface {
	isLhs()
	Expr
}

type IdExpr struct {
	Id lexer.Token
	LhsExpr
}

type Stat interface {
	isStat()
	Node
}

type ExprStat struct {
	Expr Expr
	Stat
}

type StatList struct {
	Stats []Stat
	Stat
}

type Item interface {
	isItem()
}

type ItemList struct {
	Items []Item
	Node
}

type PatternAction struct {
	Pattern Pattern
	Action  StatList
	Item
}

type Pattern interface {
	isPattern()
}

type SpecialPattern struct {
	Type lexer.Token
	Pattern
}

type parser struct {
	tokens   chan lexer.Token
	current  lexer.Token
	previous lexer.Token
}

func GetSyntaxTree(tokens chan lexer.Token) []Item {
	ps := parser{
		tokens: tokens,
	}
	ps.advance()
	return ps.itemList()
}

func (ps *parser) itemList() []Item {
	items := make([]Item, 0)
	for ps.current.Type != lexer.Eof {
		items = append(items, ps.item())
		if !ps.checkTerminator() {
			ps.parseErrorAtCurrent("expected terminator")
		}
		ps.advance()
		ps.skipNewLines()
	}
	return items
}

func (ps *parser) item() Item {
	pat := ps.pattern()
	act := ps.block()
	return PatternAction{Pattern: pat, Action: act}
}

func (ps *parser) pattern() Pattern {
	defer ps.advance()
	switch ps.current.Type {
	case lexer.Begin:
		fallthrough
	case lexer.End:
		return SpecialPattern{Type: ps.current}
	default:
		ps.parseErrorAtCurrent("unexpected token")
		return nil
	}
}

func (ps *parser) block() StatList {
	ps.eatError("expected '{'", lexer.LeftCurly)
	ret := ps.statListUntil(lexer.RightCurly)
	ps.eatError("expected '}'", lexer.RightCurly)
	return ret
}

func (ps *parser) statListUntil(types ...lexer.TokenType) StatList {
	stats := make([]Stat, 0)
	for ps.current.Type != lexer.Eof {
		ps.skipNewLines()
		stats = append(stats, ps.stat())
		if !ps.checkTerminator() {
			ps.parseErrorAtCurrent("expected terminator")
		}
		ps.advance()
		ps.skipNewLines()
		if ps.check(types...) {
			break
		}
	}
	return StatList{Stats: stats}
}

func (ps *parser) stat() Stat {
	switch ps.current.Type {
	default:
		return ps.exprStat()
	}
}

func (ps *parser) exprStat() Stat {
	expr := ps.expr()
	return ExprStat{Expr: expr}
}

func (ps *parser) expr() Expr {
	return ps.assignExpr()
}

func (ps *parser) assignExpr() Expr {
	left := ps.addExpr()
	if ps.eat(lexer.Assign) {
		lhs, ok := left.(LhsExpr)
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

func (ps *parser) addExpr() Expr {
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

func (ps *parser) mulExpr() Expr {
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

func (ps *parser) expExpr() Expr {
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

func (ps *parser) unaryExpr() Expr {
	if ps.eat(lexer.Increment, lexer.Decrement, lexer.Plus, lexer.Minus) {
		op := ps.previous
		return UnaryExpr{
			Op:    op,
			Right: ps.termExpr(),
		}
	}
	return ps.termExpr()
}

func (ps *parser) termExpr() Expr {
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

func (ps *parser) groupingExpr() Expr {
	ps.advance()
	toret := GroupingExpr{
		Expr: ps.expr(),
	}
	ps.checkError("expected ')'", lexer.RightParen)
	return toret
}

func (ps *parser) parseErrorAtCurrent(msg string) {
	fmt.Fprintf(os.Stderr, "%s: at %d (%s): %s\n", os.Args[0], ps.current.Line, ps.current.Lexeme, msg)
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

func (ps *parser) checkTerminator() bool {
	return ps.check(lexer.Newline, lexer.Eof, lexer.Semicolon)
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

func (ps *parser) skipNewLines() {
	for ps.check(lexer.Newline) {
		ps.advance()
	}
}
