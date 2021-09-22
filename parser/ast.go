/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package parser

import (
	"regexp"

	"github.com/fioriandrea/aawk/lexer"
)

type Node interface {
	isNode()
}

type Tokener interface {
	Token() lexer.Token
}

type Expr interface {
	isExpr()
	Node
	Tokener
}

type BinaryExpr struct {
	Left  Expr
	Op    lexer.Token
	Right Expr
	Expr
}

func (e *BinaryExpr) Token() lexer.Token {
	return e.Op
}

type BinaryBoolExpr struct {
	Left  Expr
	Op    lexer.Token
	Right Expr
	Expr
}

func (e *BinaryBoolExpr) Token() lexer.Token {
	return e.Op
}

type UnaryExpr struct {
	Op    lexer.Token
	Right Expr
	Expr
}

func (e *UnaryExpr) Token() lexer.Token {
	return e.Op
}

type NumberExpr struct {
	Num    lexer.Token
	NumVal float64
	Expr
}

func (e *NumberExpr) Token() lexer.Token {
	return e.Num
}

type StringExpr struct {
	Str lexer.Token
	Expr
}

func (e *StringExpr) Token() lexer.Token {
	return e.Str
}

type RegexExpr struct {
	Regex    lexer.Token
	Compiled *regexp.Regexp
	Expr
}

func (e *RegexExpr) Token() lexer.Token {
	return e.Regex
}

type MatchExpr struct {
	Left  Expr
	Op    lexer.Token
	Right Expr
	Expr
}

func (e *MatchExpr) Token() lexer.Token {
	return e.Op
}

type AssignExpr struct {
	Left  LhsExpr
	Equal lexer.Token
	Right Expr
	Expr
}

func (e *AssignExpr) Token() lexer.Token {
	return e.Equal
}

type LhsExpr interface {
	isLhs()
	Expr
}

type IdExpr struct {
	Id            lexer.Token
	Index         int // Index in the global namespace stack
	LocalIndex    int // Index in a function-local namespace stack
	FunctionIndex int // Index in the function table (if Id is a function)
	BuiltinIndex  int // Index in the builtin variable table (if Id is a builtin variable)
	LhsExpr
}

func (e *IdExpr) Token() lexer.Token {
	return e.Id
}

type IndexingExpr struct {
	Id    *IdExpr
	Index []Expr
	LhsExpr
}

func (e *IndexingExpr) Token() lexer.Token {
	return e.Id.Token()
}

type DollarExpr struct {
	Dollar lexer.Token
	Field  Expr
	LhsExpr
}

func (e *DollarExpr) Token() lexer.Token {
	return e.Dollar
}

type IncrementExpr struct {
	Op  lexer.Token
	Lhs LhsExpr
	Expr
}

func (e *IncrementExpr) Token() lexer.Token {
	return e.Op
}

type PreIncrementExpr struct {
	*IncrementExpr
}

type PostIncrementExpr struct {
	*IncrementExpr
}

type TernaryExpr struct {
	Cond     Expr
	Question lexer.Token
	Expr0    Expr
	Expr1    Expr
	Expr
}

func (e *TernaryExpr) Token() lexer.Token {
	return e.Question
}

type GetlineExpr struct {
	Op       lexer.Token
	Getline  lexer.Token
	Variable LhsExpr
	File     Expr
	Expr
}

func (e *GetlineExpr) Token() lexer.Token {
	return e.Getline
}

type LengthExpr struct {
	Length lexer.Token
	Arg    Expr
	Expr
}

func (e *LengthExpr) Token() lexer.Token {
	return e.Length
}

type CallExpr struct {
	Called *IdExpr
	Args   []Expr
	Expr
}

func (e *CallExpr) Token() lexer.Token {
	return e.Called.Id
}

type InExpr struct {
	Left  Expr
	Op    lexer.Token
	Right *IdExpr
	Expr
}

func (e *InExpr) Token() lexer.Token {
	return e.Op
}

type ExprList []Expr

func (el ExprList) isExpr() {}
func (el ExprList) isNode() {}

func (e ExprList) Token() lexer.Token {
	return e[0].Token()
}

type Stat interface {
	isStat()
	Node
	Tokener
}

type ExprStat struct {
	Expr Expr
	Stat
}

func (s *ExprStat) Token() lexer.Token {
	return s.Expr.Token()
}

type PrintStat struct {
	Print   lexer.Token
	Exprs   []Expr
	RedirOp lexer.Token
	File    Expr
	Stat
}

func (s *PrintStat) Token() lexer.Token {
	return s.Print
}

type DeleteStat struct {
	Delete lexer.Token
	Lhs    LhsExpr
	Stat
}

func (s *DeleteStat) Token() lexer.Token {
	return s.Delete
}

type IfStat struct {
	If       lexer.Token
	Cond     Expr
	Body     Stat
	ElseBody Stat
	Stat
}

func (s *IfStat) Token() lexer.Token {
	return s.If
}

type ForStat struct {
	For  lexer.Token
	Init Stat
	Cond Expr
	Inc  Stat
	Body Stat
	Stat
}

func (s *ForStat) Token() lexer.Token {
	return s.For
}

type ForEachStat struct {
	For   lexer.Token
	Id    *IdExpr
	In    lexer.Token
	Array *IdExpr
	Body  Stat
	Stat
}

func (s *ForEachStat) Token() lexer.Token {
	return s.For
}

type NextStat struct {
	Next lexer.Token
	Stat
}

func (s *NextStat) Token() lexer.Token {
	return s.Next
}

type BreakStat struct {
	Break lexer.Token
	Stat
}

func (s *BreakStat) Token() lexer.Token {
	return s.Break
}

type ContinueStat struct {
	Continue lexer.Token
	Stat
}

func (s *ContinueStat) Token() lexer.Token {
	return s.Continue
}

type ReturnStat struct {
	Return    lexer.Token
	ReturnVal Expr
	Stat
}

func (s *ReturnStat) Token() lexer.Token {
	return s.Return
}

type ExitStat struct {
	Exit   lexer.Token
	Status Expr
	Stat
}

func (s *ExitStat) Token() lexer.Token {
	return s.Exit
}

type BlockStat []Stat

func (bs BlockStat) isStat() {}
func (bs BlockStat) isNode() {}

func (s BlockStat) Token() lexer.Token {
	return s[0].Token()
}

type Item interface {
	isItem()
	Node
}

type ItemList struct {
	Items []Item
	Node
}

type FunctionDef struct {
	Name lexer.Token
	Args []lexer.Token
	Body BlockStat
	Item
}

func (i *FunctionDef) isItem() {}

type PatternAction struct {
	Pattern Pattern
	Action  BlockStat
	Item
}

func (i *PatternAction) isItem() {}

type Pattern interface {
	isPattern()
	Tokener
}

type SpecialPattern struct {
	Type lexer.Token
	Pattern
}

func (p *SpecialPattern) Token() lexer.Token {
	return p.Type
}

type ExprPattern struct {
	Expr Expr
	Pattern
}

func (p *ExprPattern) Token() lexer.Token {
	return p.Expr.Token()
}

type RangePattern struct {
	Expr0 Expr
	Comma lexer.Token
	Expr1 Expr
	Pattern
}

func (p *RangePattern) Token() lexer.Token {
	return p.Comma
}

type Items struct {
	Functions []*FunctionDef
	Begins    []*PatternAction
	Normals   []*PatternAction
	Ends      []*PatternAction
	All       []Item
}

type ResolvedItems struct {
	Items
	Globalindices   map[string]int
	Functionindices map[string]int
}
