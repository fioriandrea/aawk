/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package lexer

import (
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"unicode"
)

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

const (
	Argc = iota
	Argv
	Convfmt
	Environ
	Filename
	Fnr
	Fs
	Nf
	Nr
	Ofmt
	Ofs
	Ors
	Rlength
	Rs
	Rstart
	Subsep
)

var Builtinvars = map[string]int{
	"ARGC":     Argc,
	"ARGV":     Argv,
	"CONVFMT":  Convfmt,
	"ENVIRON":  Environ,
	"FILENAME": Filename,
	"FNR":      Fnr,
	"FS":       Fs,
	"NF":       Nf,
	"NR":       Nr,
	"OFMT":     Ofmt,
	"OFS":      Ofs,
	"ORS":      Ors,
	"RLENGTH":  Rlength,
	"RS":       Rs,
	"RSTART":   Rstart,
	"SUBSEP":   Subsep,
}

var Builtinfuncs = map[string]bool{
	"atan2":   true,
	"cos":     true,
	"sin":     true,
	"exp":     true,
	"log":     true,
	"sqrt":    true,
	"int":     true,
	"rand":    true,
	"sran":    true,
	"gsub":    true,
	"index":   true,
	"match":   true,
	"split":   true,
	"sprintf": true,
	"sub":     true,
	"substr":  true,
	"tolower": true,
	"toupper": true,
	"close":   true,
	"system":  true,
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

type Token struct {
	Type   TokenType
	Lexeme string
	Line   int
}

type Lexer struct {
	line          int
	currentRune   rune
	previousRune  rune
	reader        io.RuneReader
	previousToken Token
}

func NewLexer(reader io.RuneReader) Lexer {
	lex := Lexer{
		line:   1,
		reader: reader,
	}
	lex.advance()
	return lex
}

func (l *Lexer) Next() Token {
	contains := func(s []TokenType, e TokenType) bool {
		for _, a := range s {
			if a == e {
				return true
			}
		}
		return false
	}
	for {
		switch {
		case l.atEnd():
			return l.makeToken(Eof, "EOF")
		case l.currentRune == '\\':
			potentialErr := l.makeErrorToken("unexpected '\\'")
			l.advance()
			if l.currentRune == '\n' {
				l.newLine()
			} else {
				return potentialErr
			}
		case l.currentRune == '\n':
			if contains([]TokenType{Comma, LeftCurly, DoubleAnd, DoublePipe, Do, Else}, l.previousToken.Type) {
				l.newLine()
			} else {
				return l.newLine()
			}
		case unicode.IsSpace(l.currentRune):
			l.advance()
		case l.currentRune == '#':
			for l.currentRune != '\n' && !l.atEnd() {
				l.advance()
			}
		case l.currentRune == '"':
			return l.string()
		case unicode.IsLetter(l.currentRune) || l.currentRune == '_':
			return l.identifier()
		case unicode.IsDigit(l.currentRune):
			return l.number()
		default:
			return l.punctuation()
		}
	}
}

func (l *Lexer) NextRegex() Token {
	var lexeme strings.Builder
	fmt.Fprintf(&lexeme, "%s", l.previousToken.Lexeme[1:])
	line := l.previousToken.Line
	for !l.atEnd() && l.currentRune != '\n' {
		if l.currentRune == '/' && l.previousRune != '\\' {
			break
		}
		l.advanceInside(&lexeme)
	}
	if l.currentRune != '/' {
		return l.makeErrorToken("unterminated regex")
	}
	l.advance()
	_, err := regexp.Compile(lexeme.String())
	if err != nil {
		return l.makeErrorToken("invalid regex")
	}
	return Token{
		Lexeme: lexeme.String(),
		Type:   Regex,
		Line:   line,
	}
}

func (l *Lexer) newLine() Token {
	l.line++
	l.advance()
	return l.makeToken(Newline, "\n")
}

func (l *Lexer) string() Token {
	var lexeme strings.Builder
	l.advance()
	var c rune
	for l.currentRune != '\n' && !l.atEnd() {
		if l.previousRune == '\\' && c != '\\' {
			switch l.currentRune {
			case '"':
				c = '"'
				l.advance()
			case '/':
				c = '/'
				l.advance()
			case '\\':
				c = '\\'
				l.advance()
			case 'n':
				c = '\n'
				l.advance()
			case 't':
				c = '\t'
				l.advance()
			case 'r':
				c = '\r'
				l.advance()
			case 'a':
				c = '\a'
				l.advance()
			case 'b':
				c = '\b'
				l.advance()
			case 'f':
				c = '\f'
				l.advance()
			case 'v':
				c = '\v'
				l.advance()
			case '0', '1', '2', '3', '4', '5', '6', '7':
				cc := l.currentRune
				seq := int(cc - '0')
				cc = l.advance()
				if cc >= '0' && cc <= '7' {
					seq = seq*8 + int(cc-'0')
					cc = l.advance()
					if cc >= '0' && c <= '7' {
						seq = seq*8 + int(cc-'0')
						l.advance()
					}
				}
				c = rune(seq)

			default:
				c = l.currentRune
				l.advance()
			}
		} else if l.currentRune == '\\' {
			l.advance()
			continue
		} else if l.currentRune == '"' {
			break
		} else {
			c = l.currentRune
			l.advance()
		}
		fmt.Fprintf(&lexeme, "%c", c)
	}

	if l.currentRune != '"' {
		return l.makeErrorToken("unterminated string")
	}
	l.advance()
	return l.makeToken(String, lexeme.String())
}

func (l *Lexer) identifier() Token {
	var lexeme strings.Builder
	for l.currentRune == '_' || unicode.IsDigit(l.currentRune) || unicode.IsLetter(l.currentRune) {
		l.advanceInside(&lexeme)
	}
	rettype := Identifier
	if t, ok := keywords[lexeme.String()]; ok {
		rettype = t
	}

	if rettype == Identifier {
		if l.currentRune == '(' {
			rettype = IdentifierParen
			l.advance()
		} else if _, ok := Builtinfuncs[lexeme.String()]; ok {
			for unicode.IsSpace(l.currentRune) && l.currentRune != '\n' {
				l.advance()
			}
			if l.currentRune == '(' {
				rettype = IdentifierParen
				l.advance()
			}
		}
	}

	return l.makeTokenFromBuilder(rettype, lexeme)
}

func (l *Lexer) number() Token {
	var lexeme strings.Builder
	for unicode.IsDigit(l.currentRune) {
		l.advanceInside(&lexeme)
	}
	if l.currentRune == '.' {
		l.advanceInside(&lexeme)
		if !unicode.IsDigit(l.currentRune) {
			return l.makeErrorToken(fmt.Sprintf("expected numbers after '.' in number literal after '%s'", lexeme.String()))
		}
		for unicode.IsDigit(l.currentRune) {
			l.advanceInside(&lexeme)
		}
	}
	if l.currentRune == 'e' || l.currentRune == 'E' {
		l.advanceInside(&lexeme)
		if l.currentRune == '+' || l.currentRune == '-' {
			l.advanceInside(&lexeme)
		}
		if !unicode.IsDigit(l.currentRune) {
			return l.makeErrorToken(fmt.Sprintf("expected exponent in number literal after '%s'", lexeme.String()))
		}
		for unicode.IsDigit(l.currentRune) {
			l.advanceInside(&lexeme)
		}
	}
	return l.makeTokenFromBuilder(Number, lexeme)
}

func (l *Lexer) punctuation() Token {
	var lexeme strings.Builder
	currnode := punctuations
	for {
		if v, ok := currnode.longer[l.currentRune]; ok {
			l.advanceInside(&lexeme)
			currnode = v
		} else {
			break
		}
	}
	if currnode.current == Error {
		return l.makeErrorToken(fmt.Sprintf("undefined operator '%s'", lexeme.String()))
	}
	return l.makeTokenFromBuilder(currnode.current, lexeme)
}

func (l *Lexer) makeTokenFromBuilder(ttype TokenType, builder strings.Builder) Token {
	return l.makeToken(ttype, builder.String())
}

func (l *Lexer) makeToken(ttype TokenType, lexeme string) Token {
	l.previousToken = Token{
		Type:   ttype,
		Lexeme: lexeme,
		Line:   l.line,
	}
	return l.previousToken
}

func (l *Lexer) makeErrorToken(msg string) Token {
	return l.makeToken(Error, msg)
}

func (l *Lexer) advance() rune {
	c, _, err := l.reader.ReadRune()
	if err != nil {
		if err != io.EOF {
			log.Fatal(err)
		}
		c = '\000'
	}
	l.previousRune = l.currentRune
	l.currentRune = c
	return c
}

func (l *Lexer) currentRuneInside(builder *strings.Builder) {
	fmt.Fprintf(builder, "%c", l.currentRune)
}

func (l *Lexer) advanceInside(builder *strings.Builder) {
	l.currentRuneInside(builder)
	l.advance()
}

func (l *Lexer) atEnd() bool {
	return l.currentRune == '\000'
}
