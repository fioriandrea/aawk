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

type BinaryBoolExpr struct {
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

type IncrementExpr struct {
	Op  lexer.Token
	Lhs LhsExpr
	Expr
}

type PreIncrementExpr struct {
	IncrementExpr
}

type PostIncrementExpr struct {
	IncrementExpr
}

type TernaryExpr struct {
	Cond  Expr
	Expr0 Expr
	Expr1 Expr
	Expr
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

type IfStat struct {
	If       lexer.Token
	Cond     Expr
	Body     Stat
	ElseBody Stat
	Stat
}

type ForStat struct {
	For  lexer.Token
	Init Stat
	Cond Expr
	Inc  Stat
	Body Stat
	Stat
}

type BlockStat []Stat

func (bs BlockStat) isStat() {}
func (bs BlockStat) isNode() {}

type Item interface {
	isItem()
}

type ItemList struct {
	Items []Item
	Node
}

type PatternAction struct {
	Pattern Pattern
	Action  BlockStat
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

func (ps *parser) block() (BlockStat, error) {
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

func (ps *parser) statListUntil(types ...lexer.TokenType) (BlockStat, error) {
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
		ps.skipNewLines()
		if ps.check(types...) {
			break
		}
	}
	return stats, errtoret
}

func (ps *parser) stat() (Stat, error) {
	var stat Stat
	var err error
	switch ps.current.Type {
	case lexer.If:
		stat, err = ps.ifStat()
	case lexer.While:
		stat, err = ps.whileStat()
	case lexer.Do:
		stat, err = ps.doWhileStat()
	case lexer.For:
		stat, err = ps.forStat()
	case lexer.LeftCurly:
		stat, err = ps.block()
	default:
		stat, err = ps.simpleStat()
		if !ps.eatTerminator() && err != nil {
			stat, err = nil, ps.parseErrorAtCurrent("expected terminator")
		}
	}
	return stat, err
}

func (ps *parser) simpleStat() (Stat, error) {
	var stat Stat
	var err error
	switch ps.current.Type {
	case lexer.Print:
		stat, err = ps.printStat()
	default:
		stat, err = ps.exprStat()
	}
	return stat, err
}

func (ps *parser) exprStat() (ExprStat, error) {
	expr, err := ps.expr()
	if err != nil {
		return ExprStat{}, err
	}
	return ExprStat{Expr: expr}, nil
}

func (ps *parser) printStat() (PrintStat, error) {
	ps.eat(lexer.Print)
	op := ps.previous
	exprs, err := ps.exprListUntil()
	if err != nil {
		return PrintStat{}, err
	}
	return PrintStat{
		Print: op,
		Exprs: exprs,
	}, nil
}

func (ps *parser) ifStat() (IfStat, error) {
	ps.eat(lexer.If)
	op := ps.previous
	if !ps.eat(lexer.LeftParen) {
		return IfStat{}, ps.parseErrorAtCurrent("missing '(' for if statement condition")
	}
	cond, err := ps.expr()
	if err != nil {
		return IfStat{}, err
	}
	if !ps.eat(lexer.RightParen) {
		return IfStat{}, ps.parseErrorAtCurrent("missing ')' closing if statement condition")
	}
	ps.eat(lexer.Newline)
	body, err := ps.stat()
	if err != nil {
		return IfStat{}, err
	}
	var elsebody Stat
	if ps.eat(lexer.Else) {
		elsebody, err = ps.stat()
		if err != nil {
			return IfStat{}, err
		}
	}
	return IfStat{
		If:       op,
		Cond:     cond,
		Body:     body,
		ElseBody: elsebody,
	}, nil
}

func (ps *parser) whileStat() (ForStat, error) {
	ps.eat(lexer.While)
	op := ps.previous
	if !ps.eat(lexer.LeftParen) {
		return ForStat{}, ps.parseErrorAtCurrent("missing '(' for while statement condition")
	}
	cond, err := ps.expr()
	if err != nil {
		return ForStat{}, err
	}
	if !ps.eat(lexer.RightParen) {
		return ForStat{}, ps.parseErrorAtCurrent("missing ')' closing while statement condition")
	}
	ps.eat(lexer.Newline)
	body, err := ps.stat()
	if err != nil {
		return ForStat{}, err
	}
	return ForStat{
		For:  op,
		Cond: cond,
		Body: body,
	}, nil
}

func (ps *parser) doWhileStat() (Stat, error) {
	ps.eat(lexer.Do)
	body, err := ps.stat()
	if err != nil {
		return nil, err
	}
	if !ps.eat(lexer.While) {
		return nil, ps.parseErrorAtCurrent("expected 'while' for do-while statement")
	}
	whileop := ps.previous
	if !ps.eat(lexer.LeftParen) {
		return nil, ps.parseErrorAtCurrent("missing '(' for do-while statement condition")
	}
	cond, err := ps.expr()
	if err != nil {
		return nil, err
	}
	if !ps.eat(lexer.RightParen) {
		return nil, ps.parseErrorAtCurrent("missing ')' closing do-while statement condition")
	}
	return BlockStat([]Stat{
		body,
		ForStat{
			For:  whileop,
			Cond: cond,
			Body: body,
		},
	}), nil
}

func (ps *parser) forStat() (ForStat, error) {
	var err error
	ps.eat(lexer.For)
	op := ps.previous
	if !ps.eat(lexer.LeftParen) {
		return ForStat{}, ps.parseErrorAtCurrent("missing '(' after 'for'")
	}
	var init Stat
	if !ps.check(lexer.Semicolon) {
		init, err = ps.simpleStat()
		if err != nil {
			return ForStat{}, err
		}
	}
	if !ps.eat(lexer.Semicolon) {
		return ForStat{}, ps.parseErrorAtCurrent("expected ';' after for statement initialization")
	}

	var cond Expr
	if !ps.check(lexer.Semicolon) {
		cond, err = ps.expr()
		if err != nil {
			return ForStat{}, err
		}
	}
	if !ps.eat(lexer.Semicolon) {
		return ForStat{}, ps.parseErrorAtCurrent("expected ';' after for statement condition")
	}

	var inc Stat
	if !ps.check(lexer.RightParen) {
		inc, err = ps.simpleStat()
		if err != nil {
			return ForStat{}, err
		}
	}
	if !ps.eat(lexer.RightParen) {
		return ForStat{}, ps.parseErrorAtCurrent("expected ')' after for statement increment")
	}

	body, err := ps.stat()
	if err != nil {
		return ForStat{}, err
	}

	if cond == nil {
		cond = NumberExpr{Num: lexer.Token{
			Type:   lexer.Number,
			Lexeme: "1",
			Line:   op.Line,
		}}
	}

	return ForStat{
		For:  op,
		Init: init,
		Cond: cond,
		Inc:  inc,
		Body: body,
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
	left, err := ps.ternaryExpr()
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

func (ps *parser) ternaryExpr() (Expr, error) {
	cond, err := ps.orExpr()
	if err != nil {
		return nil, err
	}
	if ps.eat(lexer.QuestionMark) {
		expr0, err := ps.expr()
		if err != nil {
			return nil, err
		}
		if !ps.eat(lexer.Colon) {
			return nil, ps.parseErrorAtCurrent("expected ':' for ternary operator")
		}
		expr1, err := ps.expr()
		if err != nil {
			return nil, err
		}
		return TernaryExpr{
			Cond:  cond,
			Expr0: expr0,
			Expr1: expr1,
		}, nil
	}
	return cond, nil
}

func (ps *parser) orExpr() (Expr, error) {
	left, err := ps.andExpr()
	if err != nil {
		return nil, err
	}
	for ps.eat(lexer.DoublePipe) {
		op := ps.previous
		right, err := ps.andExpr()
		if err != nil {
			return nil, err
		}
		left = BinaryBoolExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}
	return left, nil
}

func (ps *parser) andExpr() (Expr, error) {
	left, err := ps.comparisonExpr()
	if err != nil {
		return nil, err
	}
	for ps.eat(lexer.DoubleAnd) {
		op := ps.previous
		right, err := ps.comparisonExpr()
		if err != nil {
			return nil, err
		}
		left = BinaryBoolExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
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
	left, err := ps.unaryExpr()
	if err != nil {
		return nil, err
	}
	for ps.eat(lexer.Star, lexer.Slash, lexer.Percent) {
		op := ps.previous
		right, err := ps.unaryExpr()
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
	if ps.eat(lexer.Plus, lexer.Minus, lexer.Not) {
		op := ps.previous
		right, err := ps.expExpr()
		if err != nil {
			return nil, err
		}
		return UnaryExpr{
			Op:    op,
			Right: right,
		}, nil
	}
	sub, err := ps.expExpr()
	if err != nil {
		return nil, err
	}
	return sub, nil
}

func (ps *parser) expExpr() (Expr, error) {
	left, err := ps.preIncrementExpr()
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

func (ps *parser) preIncrementExpr() (Expr, error) {
	if ps.eat(lexer.Increment, lexer.Decrement) {
		op := ps.previous
		expr, err := ps.expr()
		if err != nil {
			return nil, err
		}
		lhs, islhs := expr.(LhsExpr)
		if !islhs {
			return nil, ps.parseErrorAt(op, "cannot use pre-increment or pre-decrement operator on non lvalue")
		}
		return PreIncrementExpr{
			IncrementExpr{
				Op:  op,
				Lhs: lhs,
			},
		}, nil
	}
	res, err := ps.postIncrementExpr()
	return res, err
}

func (ps *parser) postIncrementExpr() (Expr, error) {
	expr, err := ps.termExpr()
	if err != nil {
		return nil, err
	}
	if ps.eat(lexer.Increment, lexer.Decrement) {
		op := ps.previous
		lhs, islhs := expr.(LhsExpr)
		if !islhs {
			return nil, ps.parseErrorAt(op, "cannot use post-increment or post-decrement operator on non lvalue")
		}
		return PostIncrementExpr{
			IncrementExpr{
				Op:  op,
				Lhs: lhs,
			},
		}, nil
	}
	return expr, nil
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

func (ps *parser) parseErrorAt(tok lexer.Token, msg string) error {
	prelude := fmt.Sprintf("%s: at line %d", os.Args[0], tok.Line)
	if ps.current.Type == lexer.Error {
		if len(msg) > 0 {
			return fmt.Errorf("%s: %s", prelude, msg)
		}
		return fmt.Errorf("%s: %s", prelude, tok.Lexeme)
	}
	return fmt.Errorf("%s (%s): %s", prelude, tok.Lexeme, msg)
}

func (ps *parser) parseErrorAtCurrent(msg string) error {
	return ps.parseErrorAt(ps.current, msg)
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

func (ps *parser) eatTerminator() bool {
	if ps.checkTerminator() {
		ps.advance()
		return true
	}
	return false
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
