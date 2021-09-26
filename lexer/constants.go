/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package lexer

import "regexp"

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
	Tilde
	NotTilde
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
	RightCurly
	LeftSquare
	RightSquare
	LeftParen
	RightParen
	Dollar
	Semicolon

	Newline

	Begin
	End
	Function
	Getline
	In
	Else

	LeftCurly
	Break
	Continue
	Delete
	Do
	Exit
	For
	If
	Next
	Print
	Printf
	Return
	While

	BeginFuncs
	Atan2
	Close
	Cos
	Exp
	Gsub
	Index
	Int
	Length
	Log
	Match
	Rand
	Sin
	Split
	Sprintf
	Sqrt
	Srand
	Sub
	Substr
	System
	Tolower
	Toupper
	EndFuncs

	Identifier
	IdentifierParen

	Regex
	String
	Number

	Concat
	Error

	TokenCount
)

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
	"next":     Next,
	"printf":   Printf,
	"print":    Print,
	"return":   Return,
	"while":    While,

	"atan2":   Atan2,
	"close":   Close,
	"cos":     Cos,
	"exp":     Exp,
	"gsub":    Gsub,
	"index":   Index,
	"int":     Int,
	"length":  Length,
	"log":     Log,
	"match":   Match,
	"rand":    Rand,
	"sin":     Sin,
	"split":   Split,
	"sprintf": Sprintf,
	"sqrt":    Sqrt,
	"srand":   Srand,
	"substr":  Substr,
	"sub":     Sub,
	"system":  System,
	"tolower": Tolower,
	"toupper": Toupper,
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
					current: NotTilde,
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
			current: Tilde,
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

var CommandLineAssignRegex = regexp.MustCompile(`^[_a-zA-Z0-9]+=.*`)

func IsBuiltinFunction(t TokenType) bool {
	return t > BeginFuncs && t < EndFuncs
}
