package parser

import (
	"fmt"
	"os"

	"github.com/fioriandrea/aawk/lexer"
)

type parser struct {
	lexer      lexer.Lexer
	current    lexer.Token
	previous   lexer.Token
	inprint    bool
	ingetline  bool
	parendepth int
	nextable   bool
	loopdepth  int
	infunction bool
}

func (ps *parser) isInGetline() bool {
	return ps.ingetline && ps.parendepth == 0
}

func (ps *parser) isInPrint() bool {
	return ps.inprint && ps.parendepth == 0
}

func GetSyntaxTree(lexer lexer.Lexer) ([]Item, error) {
	ps := parser{
		lexer: lexer,
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
	ps.skipNewLines()
	for ps.current.Type != lexer.Eof {
		item, err := ps.item()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return nil, err
		}
		items = append(items, item)
		ps.eatTerminator()
		ps.skipNewLines()
	}
	return items, nil
}

func (ps *parser) item() (Item, error) {
	switch ps.current.Type {
	case lexer.Function:
		return ps.functionItem()
	default:
		return ps.patternActionItem()
	}
}

func (ps *parser) functionItem() (Item, error) {
	ps.infunction = true
	defer func() { ps.infunction = false }()
	ps.advance()
	if !ps.eat(lexer.Identifier) {
		return nil, ps.parseErrorAtCurrent("expected identifier after 'function'")
	}
	name := ps.previous
	if !ps.eat(lexer.LeftParen) {
		return nil, ps.parseErrorAtCurrent("expected '(' after function name")
	}
	args := make([]lexer.Token, 0)
	for ps.eat(lexer.Identifier) {
		if lexer.Builtinvars[ps.previous.Lexeme] {
			return nil, ps.parseErrorAt(ps.previous, "cannot use built in variable as function parameter")
		}
		args = append(args, ps.previous)
		if !ps.eat(lexer.Comma) {
			break
		}
	}
	if !ps.eat(lexer.RightParen) {
		return nil, ps.parseErrorAtCurrent("expected ')' after argument list")
	}
	ps.skipNewLines()
	if !ps.check(lexer.LeftCurly) {
		return nil, ps.parseErrorAtCurrent("expected '{' before function body")
	}
	body, err := ps.blockStat()
	if err != nil {
		return nil, err
	}
	return FunctionDef{
		Name: name,
		Args: args,
		Body: body,
	}, nil
}

func (ps *parser) patternActionItem() (Item, error) {
	begtok := ps.current
	pat, err := ps.pattern()
	if err != nil {
		return nil, err
	}
	if pat == nil {
		pat = ExprPattern{
			Expr: NumberExpr{
				Num: lexer.Token{
					Type:   lexer.Number,
					Lexeme: "1",
					Line:   begtok.Line,
				},
			},
		}
	}
	var act BlockStat
	if ps.check(lexer.LeftCurly) {
		act, err = ps.blockStat()
		if err != nil {
			return nil, err
		}
	}
	switch pat.(type) {
	case SpecialPattern:
		if act == nil {
			return nil, ps.parseErrorAt(begtok, "special pattern must have an action")
		}
	default:
		if act == nil {
			begtok.Type = lexer.Print
			act = BlockStat{
				PrintStat{
					Print: begtok,
				},
			}
		}
	}
	ps.eatTerminator()
	return PatternAction{Pattern: pat, Action: act}, nil

}

func (ps *parser) pattern() (Pattern, error) {
	switch ps.current.Type {
	case lexer.Begin:
		fallthrough
	case lexer.End:
		ps.nextable = false
		ps.advance()
		return SpecialPattern{Type: ps.previous}, nil
	default:
		ps.nextable = true
		if ps.check(lexer.LeftCurly) {
			return nil, nil
		}
		res, err := ps.expr()
		if err != nil {
			return nil, err
		}
		if ps.eat(lexer.Comma) {
			op := ps.previous
			if ps.check(lexer.LeftCurly) {
				return nil, ps.parseErrorAt(ps.previous, "expected pattern")
			}
			res1, err := ps.expr()
			if err != nil {
				return nil, err
			}
			return RangePattern{
				Expr0: res,
				Comma: op,
				Expr1: res1,
			}, nil
		}
		return ExprPattern{
			Expr: res,
		}, nil
	}
}

func (ps *parser) blockStat() (BlockStat, error) {
	ps.eat(lexer.LeftCurly)
	ret, err := ps.statListUntil(lexer.RightCurly)
	if err != nil {
		return nil, err
	}
	if !ps.eat(lexer.RightCurly) {
		err := ps.parseErrorAtCurrent("expected '}'")
		fmt.Fprint(os.Stderr, err)
		return nil, err
	}
	return ret, nil
}

func (ps *parser) checkAllowedAfterNexts() bool {
	return ps.checkTerminator() || ps.check(lexer.RightCurly)
}

func (ps *parser) nextStat() (NextStat, error) {
	ps.eat(lexer.Next)
	op := ps.previous
	if !ps.nextable {
		return NextStat{}, ps.parseErrorAt(op, "cannot use 'next' inside BEGIN or END")
	}
	if !ps.checkAllowedAfterNexts() {
		return NextStat{}, ps.parseErrorAtCurrent("unexpected token after 'next'")
	}
	return NextStat{
		Next: op,
	}, nil
}

func (ps *parser) breakStat() (BreakStat, error) {
	ps.eat(lexer.Break)
	op := ps.previous
	if ps.loopdepth == 0 {
		return BreakStat{}, ps.parseErrorAt(op, "cannot have break outside loop")
	}
	if !ps.checkAllowedAfterNexts() {
		return BreakStat{}, ps.parseErrorAtCurrent("unexpected token after 'break'")
	}
	return BreakStat{
		Break: op,
	}, nil
}

func (ps *parser) continueStat() (ContinueStat, error) {
	ps.eat(lexer.Continue)
	op := ps.previous
	if ps.loopdepth == 0 {
		return ContinueStat{}, ps.parseErrorAt(op, "cannot have continue outside loop")
	}
	if !ps.checkAllowedAfterNexts() {
		return ContinueStat{}, ps.parseErrorAtCurrent("unexpected token after 'continue'")
	}
	return ContinueStat{
		Continue: op,
	}, nil
}

func (ps *parser) returnStat() (ReturnStat, error) {
	ps.eat(lexer.Return)
	op := ps.previous
	if !ps.infunction {
		return ReturnStat{}, ps.parseErrorAt(op, "cannot have return outside a function")
	}
	var expr Expr
	if !ps.checkEndOfReturn() {
		var err error
		expr, err = ps.expr()
		if err != nil {
			return ReturnStat{}, err
		}
	}
	return ReturnStat{
		Return:    op,
		ReturnVal: expr,
	}, nil
}

func (ps *parser) statListUntil(types ...lexer.TokenType) (BlockStat, error) {
	ps.skipNewLines()
	stats := make([]Stat, 0)
	var errtoret error = nil
	for ps.current.Type != lexer.Eof && !ps.check(types...) {
		stat, err := ps.stat()
		if err != nil {
			errtoret = err
			fmt.Fprintln(os.Stderr, err)
			for !ps.checkTerminator() {
				ps.advance()
			}
			continue
		}
		stats = append(stats, stat)
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
		stat, err = ps.blockStat()
	case lexer.Next:
		stat, err = ps.nextStat()
	case lexer.Break:
		stat, err = ps.breakStat()
	case lexer.Continue:
		stat, err = ps.continueStat()
	case lexer.Return:
		stat, err = ps.returnStat()
	case lexer.Semicolon:
		fallthrough
	case lexer.Newline:
		ps.advance()
		stat, err = nil, nil
	default:
		stat, err = ps.simpleStat()
	}
	ps.skipNewLines()
	return stat, err
}

func (ps *parser) simpleStat() (Stat, error) {
	var stat Stat
	var err error
	switch ps.current.Type {
	case lexer.Print:
		fallthrough
	case lexer.Printf:
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
	ps.inprint = true
	defer func() { ps.inprint = false }()
	ps.eat(lexer.Print, lexer.Printf)
	op := ps.previous
	exprs, err := ps.exprListEmpty(func() bool { return ps.checkEndOfPrintExprList() })
	if err != nil {
		return PrintStat{}, err
	}
	if len(exprs) == 1 {
		exprlist, ok := exprs[0].(ExprList)
		if ok {
			exprs = exprlist
		}
	} else {
		for _, expr := range exprs {
			if _, isexprlist := expr.(ExprList); isexprlist {
				return PrintStat{}, ps.parseErrorAt(op, "cannot have multiple expression lists in output statement")
			}
		}
	}
	var redir lexer.Token
	var file Expr
	if ps.eat(lexer.Pipe, lexer.Greater, lexer.DoubleGreater) {
		redir = ps.previous
		file, err = ps.concatExpr()
		if err != nil {
			return PrintStat{}, err
		}
		if file == nil {
			return PrintStat{}, ps.parseErrorAt(redir, "expected expression after redirection operator")
		}
	}
	if op.Type == lexer.Printf && len(exprs) == 0 {
		return PrintStat{}, ps.parseErrorAt(op, "'printf' requires at least one argument")
	}
	return PrintStat{
		Print:   op,
		Exprs:   exprs,
		RedirOp: redir,
		File:    file,
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
	ps.loopdepth++
	defer func() { ps.loopdepth-- }()
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
	ps.loopdepth++
	defer func() { ps.loopdepth-- }()
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

func (ps *parser) forStat() (Stat, error) {
	ps.loopdepth++
	defer func() { ps.loopdepth-- }()
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
		if ps.eat(lexer.RightParen) {
			return ps.finishForEachStat(op, init)
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

func (ps *parser) finishForEachStat(fortok lexer.Token, init Stat) (ForEachStat, error) {
	ps.eat(lexer.RightParen)
	rparen := ps.previous
	exprstat, isexprstat := init.(ExprStat)
	if !isexprstat {
		return ForEachStat{}, ps.parseErrorAt(rparen, "expected ';'")
	}
	inexpr, isinexpr := exprstat.Expr.(InExpr)
	if !isinexpr {
		return ForEachStat{}, ps.parseErrorAt(rparen, "expected ';'")
	}
	id, isid := inexpr.Left.(IdExpr)
	if !isid {
		return ForEachStat{}, ps.parseErrorAt(rparen, "expected ';'")
	}
	body, err := ps.stat()
	if err != nil {
		return ForEachStat{}, err
	}
	return ForEachStat{
		For:   fortok,
		Id:    id,
		In:    inexpr.Op,
		Array: inexpr.Right,
		Body:  body,
	}, nil

}

func (ps *parser) exprListEmpty(eolfn func() bool) ([]Expr, error) {
	if eolfn() || ps.checkTerminator() {
		return nil, nil
	}
	exprs, err := ps.exprList(eolfn)
	return exprs, err
}

func (ps *parser) exprList(eolfn func() bool) ([]Expr, error) {
	exprs := make([]Expr, 0)
	expr, err := ps.expr()
	if err != nil {
		return nil, err
	}
	exprs = append(exprs, expr)

	for !eolfn() && !ps.checkTerminator() {
		if !ps.eat(lexer.Comma) {
			return nil, ps.parseErrorAtCurrent("expected ','")
		}
		ps.skipNewLines()
		expr, err := ps.expr()
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, expr)
	}
	return exprs, nil
}

func (ps *parser) expr() (Expr, error) {
	sub, err := ps.pipeGetlineExpr()
	return sub, err
}

func (ps *parser) pipeGetlineExpr() (Expr, error) {
	prog, err := ps.assignExpr()
	if err != nil {
		return nil, err
	}
	if !ps.isInPrint() && ps.eat(lexer.Pipe) {
		op := ps.previous
		if !ps.eat(lexer.Getline) {
			return nil, ps.parseErrorAtCurrent("expected 'getline' after '|'")
		}
		getline := ps.previous
		var variable LhsExpr
		if ps.checkBeginLhs() {
			varexpr, err := ps.expr()
			if err != nil {
				return nil, err
			}
			var islhs bool
			variable, islhs = varexpr.(LhsExpr)
			if !islhs {
				return nil, ps.parseErrorAt(op, "expected lhs after 'getline'")
			}
		}
		return GetlineExpr{
			Op:       op,
			Getline:  getline,
			Variable: variable,
			File:     prog,
		}, nil
	}
	return prog, nil
}

func (ps *parser) assignExpr() (Expr, error) {
	left, err := ps.ternaryExpr()
	if err != nil {
		return nil, err
	}
	if ps.eat(lexer.Assign, lexer.ExpAssign, lexer.ModAssign, lexer.MulAssign, lexer.DivAssign, lexer.PlusAssign, lexer.MinusAssign) {
		equal := ps.previous
		lhs, ok := left.(LhsExpr)
		if !ok {
			return nil, ps.parseErrorAt(equal, "cannot assign to a non left hand side")
		}
		right, err := ps.expr()
		if err != nil {
			return nil, err
		}
		op := equal
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
			Equal: equal,
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
		op := ps.previous
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
			Cond:     cond,
			Question: op,
			Expr0:    expr0,
			Expr1:    expr1,
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
	left, err := ps.matchExpr()
	if err != nil {
		return nil, err
	}
	for ps.eat(lexer.DoubleAnd) {
		op := ps.previous
		right, err := ps.matchExpr()
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

func (ps *parser) matchExpr() (Expr, error) {
	left, err := ps.inExpr()
	if err != nil {
		return nil, err
	}
	if ps.eat(lexer.Match, lexer.NotMatch) {
		op := ps.previous
		right, err := ps.inExpr()
		if err != nil {
			return nil, err
		}
		left = MatchExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}
	return left, nil
}

func (ps *parser) inExpr() (Expr, error) {
	var left Expr
	left, err := ps.comparisonExpr()
	if err != nil {
		return nil, err
	}
	for ps.eat(lexer.In) {
		op := ps.previous
		right, err := ps.termExpr()
		if err != nil {
			return nil, err
		}
		id, isid := right.(IdExpr)
		if !isid {
			return nil, ps.parseErrorAt(op, "cannot use 'in' for non identifier")
		}
		left = InExpr{
			Left:  left,
			Op:    op,
			Right: id,
		}
	}
	if _, isexplist := left.(ExprList); isexplist && !ps.isInPrint() {
		return nil, ps.parseErrorAtCurrent("expected 'in'")
	}
	return left, nil
}

func (ps *parser) comparisonExpr() (Expr, error) {
	left, err := ps.concatExpr()
	if err != nil {
		return nil, err
	}
	if !ps.isInGetline() && (ps.eat(lexer.Equal, lexer.NotEqual, lexer.Less, lexer.LessEqual, lexer.GreaterEqual) || (!ps.isInPrint() && ps.eat(lexer.Greater))) {
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
	for !ps.checkTerminator() && ps.checkAllowedAfterConcat() {
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
	expr, err := ps.dollarExpr()
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

func (ps *parser) dollarExpr() (Expr, error) {
	if ps.eat(lexer.Dollar) {
		dollar := ps.previous
		expr, err := ps.termExpr()
		if err != nil {
			return nil, err
		}
		return DollarExpr{
			Dollar: dollar,
			Field:  expr,
		}, nil
	}
	texpr, err := ps.termExpr()
	return texpr, err
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
		if id.Type == lexer.Identifier && ps.eat(lexer.LeftSquare) {
			sub, err = ps.insideIndexing(id)
		} else if ps.eat(lexer.LeftParen) {
			sub, err = ps.callExpr(id)
		} else if id.Type == lexer.Identifier {
			sub, err = IdExpr{
				Id: id,
			}, nil
		} else {
			return nil, ps.parseErrorAt(id, "unexpected builtin function name")
		}
	case lexer.Getline:
		sub, err = ps.getlineExpr()
	case lexer.Slash:
		fallthrough
	case lexer.DivAssign:
		sub, err = ps.regexExpr()
	case lexer.Error:
		defer ps.advance()
		sub, err = nil, ps.parseErrorAtCurrent("")
	default:
		defer ps.advance()
		sub, err = nil, ps.parseErrorAtCurrent("unexpected token")
	}
	if err != nil && !ps.checkAllowedAfterExpr() {
		sub, err = nil, ps.parseErrorAtCurrent("unexpected token after term")
	}
	return sub, err
}

func (ps *parser) regexExpr() (Expr, error) {
	ps.advanceRegex()
	if ps.current.Type == lexer.Error {
		return nil, ps.parseErrorAtCurrent("")
	}
	ps.advance()
	return RegexExpr{
		Regex: ps.previous,
	}, nil
}

func (ps *parser) callExpr(called lexer.Token) (Expr, error) {
	ps.parendepth++
	defer func() { ps.parendepth-- }()
	exprs, err := ps.exprListEmpty(func() bool { return ps.check(lexer.RightParen) })
	if err != nil {
		return nil, err
	}
	if !ps.eat(lexer.RightParen) {
		return nil, ps.parseErrorAtCurrent("expected ')' after call")
	}
	return CallExpr{
		Called: called,
		Args:   exprs,
	}, nil
}

func (ps *parser) getlineExpr() (Expr, error) {
	ps.ingetline = true
	defer func() { ps.ingetline = false }()
	ps.eat(lexer.Getline)
	getline := ps.previous
	var variable LhsExpr
	if ps.checkBeginLhs() {
		varexpr, err := ps.expr()
		if err != nil {
			return nil, err
		}
		var islhs bool
		variable, islhs = varexpr.(LhsExpr)
		if !islhs {
			return nil, ps.parseErrorAt(getline, "cannot assign with getline to non lhs")
		}
	}
	var op lexer.Token
	var file Expr
	if ps.eat(lexer.Less) {
		op = ps.previous
		var err error
		file, err = ps.expr()
		if err != nil {
			return nil, err
		}
	}
	return GetlineExpr{
		Op:       op,
		Getline:  getline,
		Variable: variable,
		File:     file,
	}, nil
}

func (ps *parser) groupingExpr() (Expr, error) {
	ps.parendepth++
	defer func() { ps.parendepth-- }()
	ps.eat(lexer.LeftParen)
	var exprl []Expr
	var err error
	exprl, err = ps.exprList(func() bool { return ps.check(lexer.RightParen) })
	if err != nil {
		return nil, err
	}
	if !ps.eat(lexer.RightParen) {
		return nil, ps.parseErrorAtCurrent("expected closing ')'")
	} else if len(exprl) == 1 {
		return exprl[0], nil
	} else {
		return ExprList(exprl), nil
	}
}

func (ps *parser) insideIndexing(id lexer.Token) (Expr, error) {
	exprs, err := ps.exprList(func() bool { return ps.check(lexer.RightSquare) })
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
			return fmt.Errorf("%s: lexer error: %s", prelude, msg)
		}
		return fmt.Errorf("%s: lexer error: %s", prelude, tok.Lexeme)
	}
	return fmt.Errorf("%s (%s): parse error: %s", prelude, tok.Lexeme, msg)
}

func (ps *parser) parseErrorAtCurrent(msg string) error {
	return ps.parseErrorAt(ps.current, msg)
}

func (ps *parser) advance() {
	t := ps.lexer.Next()
	ps.previous = ps.current
	ps.current = t
}

func (ps *parser) advanceRegex() {
	t := ps.lexer.NextRegex()
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

func (ps *parser) checkBeginLhs() bool {
	return ps.check(lexer.Dollar, lexer.Identifier)
}

func (ps *parser) checkAllowedAfterExpr() bool {
	return !ps.check(lexer.Do, lexer.Print, lexer.Printf, lexer.While, lexer.For, lexer.Function, lexer.Break, lexer.Continue, lexer.Next)
}

func (ps *parser) checkAllowedAfterConcat() bool {
	return ps.check(lexer.Getline, lexer.Dollar, lexer.Not, lexer.Identifier, lexer.Number, lexer.String, lexer.LeftParen)
}

func (ps *parser) checkEndOfPrintExprList() bool {
	// stop at rightcurly (e.g. {print "a"}), rightparen (end of func args), pipe , doublegreater and greater (redir)
	// cannot just check for comma presence because {print "a" print "b"} would pass
	return ps.checkTerminator() || ps.check(lexer.RightCurly, lexer.RightParen, lexer.RightSquare, lexer.Pipe, lexer.DoubleGreater, lexer.Greater)
}

func (ps *parser) checkEndOfReturn() bool {
	return ps.check(lexer.RightCurly) || ps.checkTerminator()
}
