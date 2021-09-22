/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package lexer

type TokenType int

const (
	Eof TokenType = iota

	Increment
	Decrement
	Caret
	Not
	Plus
	Minus
	Star
	Slash
	Percent
	Less
	LessEqual
	NotEqual
	Equal
	Greater
	GreaterEqual
	DoubleGreater
	Match
	NotMatch
	DoubleAnd
	DoublePipe
	Pipe
	QuestionMark
	Colon
	Comma
	ExpAssign
	ModAssign
	MulAssign
	DivAssign
	PlusAssign
	MinusAssign
	Assign
	LeftCurly
	RightCurly
	LeftSquare
	RightSquare
	LeftParen
	RightParen
	Dollar
	Semicolon

	Newline

	Begin
	Break
	Continue
	Delete
	Do
	Else
	End
	Exit
	For
	Function
	Getline
	If
	In
	Length
	Next
	Print
	Printf
	Rand
	Return
	While
	Identifier
	IdentifierParen

	Regex
	String
	Number

	Concat
	Error

	TokenCount
)

// these functions are special (parsing-wise) because
// they can be called with parentheses separated from
// the name (e.g. cos (x))
var Builtinfuncs = map[string]bool{
	"atan2":   true,
	"close":   true,
	"cos":     true,
	"exp":     true,
	"gsub":    true,
	"index":   true,
	"int":     true,
	"log":     true,
	"match":   true,
	"rand":    true,
	"sin":     true,
	"split":   true,
	"sprintf": true,
	"sqrt":    true,
	"srand":   true,
	"sran":    true,
	"substr":  true,
	"sub":     true,
	"system":  true,
	"tolower": true,
	"toupper": true,
}

var keywords = map[string]TokenType{
	"BEGIN":    Begin,
	"break":    Break,
	"continue": Continue,
	"delete":   Delete,
	"do":       Do,
	"else":     Else,
	"END":      End,
	"exit":     Exit,
	"for":      For,
	"function": Function,
	"getline":  Getline,
	"if":       If,
	"in":       In,
	"length":   Length,
	"next":     Next,
	"printf":   Printf,
	"print":    Print,
	"return":   Return,
	"while":    While,
}

type trienode struct {
	current TokenType
	longer  map[rune]trienode
}

var punctuations = trienode{
	current: Error,
	longer: map[rune]trienode{
		'+': {
			current: Plus,
			longer: map[rune]trienode{
				'+': {
					current: Increment,
				},
				'=': {
					current: PlusAssign,
				},
			},
		},
		'-': {
			current: Minus,
			longer: map[rune]trienode{
				'-': {
					current: Decrement,
				},
				'=': {
					current: MinusAssign,
				},
			},
		},
		'*': {
			current: Star,
			longer: map[rune]trienode{
				'=': {
					current: MulAssign,
				},
			},
		},
		'/': {
			current: Slash,
			longer: map[rune]trienode{
				'=': {
					current: DivAssign,
				},
			},
		},
		'%': {
			current: Percent,
			longer: map[rune]trienode{
				'=': {
					current: ModAssign,
				},
			},
		},
		'^': {
			current: Caret,
			longer: map[rune]trienode{
				'=': {
					current: ExpAssign,
				},
			},
		},
		'!': {
			current: Not,
			longer: map[rune]trienode{
				'=': {
					current: NotEqual,
				},
				'~': {
					current: NotMatch,
				},
			},
		},
		'=': {
			current: Assign,
			longer: map[rune]trienode{
				'=': {
					current: Equal,
				},
			},
		},
		'<': {
			current: Less,
			longer: map[rune]trienode{
				'=': {
					current: LessEqual,
				},
			},
		},
		'>': {
			current: Greater,
			longer: map[rune]trienode{
				'=': {
					current: GreaterEqual,
				},
				'>': {
					current: DoubleGreater,
				},
			},
		},
		'~': {
			current: Match,
		},
		'?': {
			current: QuestionMark,
		},
		':': {
			current: Colon,
		},
		',': {
			current: Comma,
		},
		'{': {
			current: LeftCurly,
		},
		'}': {
			current: RightCurly,
		},
		'[': {
			current: LeftSquare,
		},
		']': {
			current: RightSquare,
		},
		'(': {
			current: LeftParen,
		},
		')': {
			current: RightParen,
		},
		'$': {
			current: Dollar,
		},
		';': {
			current: Semicolon,
		},
		'&': {
			current: Error,
			longer: map[rune]trienode{
				'&': {
					current: DoubleAnd,
				},
			},
		},
		'|': {
			current: Pipe,
			longer: map[rune]trienode{
				'|': {
					current: DoublePipe,
				},
			},
		},
	},
}
