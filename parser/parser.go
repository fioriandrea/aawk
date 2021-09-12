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

type StringExpr struct {
	Str lexer.Token
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

type IndexingExpr struct {
	Id    lexer.Token
	Index []Expr
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

type PrintStat struct {
	Print lexer.Token
	Exprs []Expr
	Stat
}

type StatList []Stat

func (sl StatList) isStat() {}
func (sl StatList) isNode() {}

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

func GetSyntaxTree(tokens chan lexer.Token) ([]Item, error) {
	ps := parser{
		tokens: tokens,
	}
	ps.advance()
	res, err := ps.itemList()
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (ps *parser) itemList() ([]Item, error) {
	items := make([]Item, 0)
	for ps.current.Type != lexer.Eof {
		item, err := ps.item()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		if !ps.checkTerminator() {
			return nil, ps.parseErrorAtCurrent("expected terminator")
		}
		ps.advance()
		ps.skipNewLines()
	}
	return items, nil
}

func (ps *parser) item() (Item, error) {
	pat, err := ps.pattern()
	if err != nil {
		return nil, err
	}
	act, err := ps.block()
	if err != nil {
		return nil, err
	}
	return PatternAction{Pattern: pat, Action: act}, nil
}

func (ps *parser) pattern() (Pattern, error) {
	defer ps.advance()
	switch ps.current.Type {
	case lexer.Begin:
		fallthrough
	case lexer.End:
		return SpecialPattern{Type: ps.current}, nil
	default:
		return nil, ps.parseErrorAtCurrent("unexpected token")
	}
}

func (ps *parser) block() (StatList, error) {
	if !ps.eat(lexer.LeftCurly) {
		return nil, ps.parseErrorAtCurrent("expected '{'")
	}
	ret, err := ps.statListUntil(lexer.RightCurly)
	if err != nil {
		return nil, err
	}
	if !ps.eat(lexer.RightCurly) {
		return nil, ps.parseErrorAtCurrent("expected '}'")
	}
	return ret, nil
}

func (ps *parser) statListUntil(types ...lexer.TokenType) (StatList, error) {
	stats := make([]Stat, 0)
	var errtoret error = nil
	for ps.current.Type != lexer.Eof {
		ps.skipNewLines()
		stat, err := ps.stat()
		if err != nil {
			errtoret = err
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		stats = append(stats, stat)
		if !ps.checkTerminator() {
			errtoret = ps.parseErrorAtCurrent("expected terminator")
			fmt.Fprintln(os.Stderr, errtoret)
			continue
		}
		ps.advance()
		ps.skipNewLines()
		if ps.check(types...) {
			break
		}
	}
	return StatList(stats), errtoret
}

func (ps *parser) stat() (Stat, error) {
	switch ps.current.Type {
	case lexer.Print:
		stat, err := ps.printStat()
		return stat, err
	default:
		stat, err := ps.exprStat()
		return stat, err
	}
}

func (ps *parser) exprStat() (Stat, error) {
	expr, err := ps.expr()
	if err != nil {
		return nil, err
	}
	return ExprStat{Expr: expr}, nil
}

func (ps *parser) printStat() (Stat, error) {
	ps.advance()
	op := ps.previous
	exprs, err := ps.exprListUntil()
	if err != nil {
		return nil, err
	}
	return PrintStat{
		Print: op,
		Exprs: exprs,
	}, nil
}

func (ps *parser) exprListUntil(types ...lexer.TokenType) ([]Expr, error) {
	if ps.checkTerminator() || ps.check(types...) {
		return nil, nil
	}
	exprs, err := ps.exprList()
	return exprs, err
}

func (ps *parser) exprList() ([]Expr, error) {
	exprs := make([]Expr, 0)
	expr, err := ps.expr()
	if err != nil {
		return nil, err
	}
	exprs = append(exprs, expr)
	for ps.eat(lexer.Comma) {
		expr, err := ps.expr()
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, expr)
	}
	return exprs, nil
}

func (ps *parser) expr() (Expr, error) {
	sub, err := ps.assignExpr()
	return sub, err
}

func (ps *parser) assignExpr() (Expr, error) {
	left, err := ps.comparisonExpr()
	if err != nil {
		return nil, err
	}
	if ps.eat(lexer.Assign, lexer.ExpAssign, lexer.ModAssign, lexer.MulAssign, lexer.DivAssign, lexer.PlusAssign, lexer.MinusAssign) {
		op := ps.previous
		lhs, ok := left.(LhsExpr)
		if !ok {
			return nil, ps.parseErrorAtCurrent("cannot assign to a non left hand side")
		}
		right, err := ps.expr()
		if err != nil {
			return nil, err
		}
		switch op.Type {
		case lexer.ExpAssign:
			op.Type = lexer.Caret
		case lexer.ModAssign:
			op.Type = lexer.Percent
		case lexer.MulAssign:
			op.Type = lexer.Star
		case lexer.DivAssign:
			op.Type = lexer.Slash
		case lexer.PlusAssign:
			op.Type = lexer.Plus
		case lexer.MinusAssign:
			op.Type = lexer.Minus
		}
		if op.Type != lexer.Assign {
			right = BinaryExpr{
				Left:  left,
				Op:    op,
				Right: right,
			}
		}
		return AssignExpr{
			Left:  lhs,
			Right: right,
		}, nil
	}
	return left, nil
}

func (ps *parser) comparisonExpr() (Expr, error) {
	left, err := ps.concatExpr()
	if err != nil {
		return nil, err
	}
	if ps.eat(lexer.Equal, lexer.NotEqual, lexer.Less, lexer.LessEqual, lexer.Greater, lexer.GreaterEqual) {
		op := ps.previous
		right, err := ps.concatExpr()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}
	return left, nil
}

func (ps *parser) concatExpr() (Expr, error) {
	left, err := ps.addExpr()
	if err != nil {
		return nil, err
	}
	for !ps.checkTerminator() && ps.check(lexer.Dollar, lexer.Not, lexer.Identifier, lexer.Number, lexer.String, lexer.LeftParen) {
		op := lexer.Token{
			Type:   lexer.Concat,
			Lexeme: "",
			Line:   ps.current.Line,
		}
		right, err := ps.addExpr()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}
	return left, nil
}

func (ps *parser) addExpr() (Expr, error) {
	left, err := ps.mulExpr()
	if err != nil {
		return nil, err
	}
	for ps.eat(lexer.Plus, lexer.Minus) {
		op := ps.previous
		right, err := ps.mulExpr()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}
	return left, nil
}

func (ps *parser) mulExpr() (Expr, error) {
	left, err := ps.expExpr()
	if err != nil {
		return nil, err
	}
	for ps.eat(lexer.Star, lexer.Slash, lexer.Percent) {
		op := ps.previous
		right, err := ps.expExpr()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}
	return left, nil
}

func (ps *parser) expExpr() (Expr, error) {
	left, err := ps.unaryExpr()
	if err != nil {
		return nil, err
	}
	if ps.eat(lexer.Caret) {
		op := ps.previous
		right, err := ps.expr()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}
	return left, nil
}

func (ps *parser) unaryExpr() (Expr, error) {
	if ps.eat(lexer.Increment, lexer.Decrement, lexer.Plus, lexer.Minus) {
		op := ps.previous
		right, err := ps.termExpr()
		if err != nil {
			return nil, err
		}
		return UnaryExpr{
			Op:    op,
			Right: right,
		}, nil
	}
	sub, err := ps.termExpr()
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func (ps *parser) termExpr() (Expr, error) {
	var sub Expr
	var err error
	switch ps.current.Type {
	case lexer.Number:
		sub, err = NumberExpr{
			Num: ps.current,
		}, nil
		ps.advance()
	case lexer.String:
		sub, err = StringExpr{
			Str: ps.current,
		}, nil
		ps.advance()
	case lexer.LeftParen:
		sub, err = ps.groupingExpr()
	case lexer.Identifier:
		id := ps.current
		ps.advance()
		if ps.eat(lexer.LeftSquare) {
			sub, err = ps.insideIndexing(id)
		} else {
			sub, err = IdExpr{
				Id: id,
			}, nil
		}
	case lexer.Error:
		sub, err = nil, ps.parseErrorAtCurrent("")
		ps.advance()
	default:
		sub, err = nil, ps.parseErrorAtCurrent("unexpected token")
		ps.advance()
	}
	return sub, err
}

func (ps *parser) groupingExpr() (Expr, error) {
	ps.advance()
	expr, err := ps.expr()
	if err != nil {
		return nil, err
	}
	toret := GroupingExpr{
		Expr: expr,
	}
	if !ps.eat(lexer.RightParen) {
		return nil, ps.parseErrorAtCurrent("expected ')'")
	}
	return toret, nil
}

func (ps *parser) insideIndexing(id lexer.Token) (Expr, error) {
	exprs, err := ps.exprList()
	if err != nil {
		return nil, err
	}
	if !ps.eat(lexer.RightSquare) {
		return nil, ps.parseErrorAtCurrent("expected ']'")
	}
	return IndexingExpr{
		Id:    id,
		Index: exprs,
	}, nil
}

func (ps *parser) parseErrorAtCurrent(msg string) error {
	prelude := fmt.Sprintf("%s: at line %d", os.Args[0], ps.current.Line)
	if ps.current.Type == lexer.Error {
		if len(msg) > 0 {
			return fmt.Errorf("%s: %s", prelude, msg)
		}
		return fmt.Errorf("%s: %s", prelude, ps.current.Lexeme)
	}
	return fmt.Errorf("%s (%s): %s", prelude, ps.current.Lexeme, msg)
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
