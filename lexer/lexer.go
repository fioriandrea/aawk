package lexer

import (
	"fmt"
	"io"
	"log"
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
	Match
	NotMatch
	DoubleAnd
	DoublePipe
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
	End
	Break
	Continue
	Delete
	Do
	Else
	Exit
	For
	Function
	If
	In
	Next
	Print
	Printf
	Return
	While
	Getline

	Identifier

	Regex
	String
	Number

	Concat
	Error

	TokenCount
)

var keywords = map[string]TokenType{
	"BEGIN":    Begin,
	"END":      End,
	"break":    Break,
	"continue": Continue,
	"delete":   Delete,
	"do":       Do,
	"else":     Else,
	"exit":     Exit,
	"for":      For,
	"function": Function,
	"if":       If,
	"in":       In,
	"next":     Next,
	"print":    Print,
	"printf":   Printf,
	"return":   Return,
	"while":    While,
	"getline":  Getline,
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
			current: Error,
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

type lexer struct {
	line         int
	currentRune  rune
	reader       io.RuneReader
	previousType TokenType
}

func GetTokens(reader io.RuneReader, output chan Token) {
	lex := lexer{
		line:   1,
		reader: reader,
	}
	lex.advance()
	for {
		lex.next(output)
	}
}

func (l *lexer) next(output chan Token) {
	contains := func(s []TokenType, e TokenType) bool {
		for _, a := range s {
			if a == e {
				return true
			}
		}
		return false
	}
	switch {
	case l.atEnd():
		output <- l.makeToken(Eof, "EOF")
	case l.currentRune == '\\':
		potentialErr := l.makeErrorToken("unexpected '\\'")
		l.advance()
		if l.currentRune == '\n' {
			l.newLine()
		} else {
			output <- potentialErr
		}
	case l.currentRune == '\n':
		if contains([]TokenType{Comma, LeftCurly, DoubleAnd, DoublePipe, Do, Else}, l.previousType) {
			l.newLine()
		} else {
			output <- l.newLine()
		}
	case unicode.IsSpace(l.currentRune):
		l.advance()
	case l.currentRune == '#':
		for l.currentRune != '\n' && !l.atEnd() {
			l.advance()
		}
	case l.currentRune == '"':
		output <- l.string()
	case unicode.IsLetter(l.currentRune) || l.currentRune == '_':
		output <- l.identifier()
	case unicode.IsDigit(l.currentRune):
		output <- l.number()
	default:
		output <- l.punctuation()
	}
}

func (l *lexer) newLine() Token {
	l.line++
	l.advance()
	return l.makeToken(Newline, "\n")
}

func (l *lexer) string() Token {
	var lexeme strings.Builder
	prev := l.currentRune
	l.advance()
	for l.currentRune != '\n' && !l.atEnd() {
		if l.currentRune == '"' && prev != '\\' {
			break
		}
		l.currentRuneInside(&lexeme)
		prev = l.currentRune
		l.advance()
	}

	defer l.advance()
	if l.currentRune != '"' {
		return l.makeErrorToken("unterminated string")
	}

	return l.makeTokenFromBuilder(String, lexeme)
}

func (l *lexer) identifier() Token {
	var lexeme strings.Builder
	for l.currentRune == '_' || unicode.IsDigit(l.currentRune) || unicode.IsLetter(l.currentRune) {
		l.advanceInside(&lexeme)
	}
	rettype := Identifier
	if t, ok := keywords[lexeme.String()]; ok {
		rettype = t
	}

	return l.makeTokenFromBuilder(rettype, lexeme)
}

func (l *lexer) number() Token {
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

func (l *lexer) punctuation() Token {
	var lexeme strings.Builder
	currnode := punctuations
	for {
		fmt.Fprintf(&lexeme, "%c", l.currentRune)
		if currnode.longer == nil {
			break
		}
		if v, ok := currnode.longer[l.currentRune]; ok {
			currnode = v
			l.advance()
		} else {
			break
		}
	}
	if currnode.current == Error {
		return l.makeErrorToken(fmt.Sprintf("undefined operator '%s'", lexeme.String()))
	}
	return l.makeTokenFromBuilder(currnode.current, lexeme)
}

func (l *lexer) makeTokenFromBuilder(ttype TokenType, builder strings.Builder) Token {
	return l.makeToken(ttype, builder.String())
}

func (l *lexer) makeToken(ttype TokenType, lexeme string) Token {
	l.previousType = ttype
	return Token{
		Type:   ttype,
		Lexeme: lexeme,
		Line:   l.line,
	}
}

func (l *lexer) makeErrorToken(msg string) Token {
	return l.makeToken(Error, msg)
}

func (l *lexer) advance() rune {
	c, _, err := l.reader.ReadRune()
	if err != nil {
		if err != io.EOF {
			log.Fatal(err)
		}
		c = '\000'
	}
	l.currentRune = c
	return c
}

func (l *lexer) currentRuneInside(builder *strings.Builder) {
	fmt.Fprintf(builder, "%c", l.currentRune)
}

func (l *lexer) advanceInside(builder *strings.Builder) {
	l.currentRuneInside(builder)
	l.advance()
}

func (l *lexer) atEnd() bool {
	return l.currentRune == '\000'
}
