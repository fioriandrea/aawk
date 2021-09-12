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

var punctuations = map[string]TokenType{
	"++": Increment,
	"--": Decrement,
	"^":  Caret,
	"!":  Not,
	"+":  Plus,
	"-":  Minus,
	"*":  Star,
	"/":  Slash,
	"%":  Percent,
	"<":  Less,
	"<=": LessEqual,
	"!=": NotEqual,
	"==": Equal,
	">":  Greater,
	">=": GreaterEqual,
	"~":  Match,
	"!~": NotMatch,
	"&&": DoubleAnd,
	"||": DoublePipe,
	"?":  QuestionMark,
	":":  Colon,
	",":  Comma,
	"^=": ExpAssign,
	"%=": ModAssign,
	"*=": MulAssign,
	"/=": DivAssign,
	"+=": PlusAssign,
	"-=": MinusAssign,
	"=":  Assign,
	"{":  LeftCurly,
	"}":  RightCurly,
	"[":  LeftSquare,
	"]":  RightSquare,
	"(":  LeftParen,
	")":  RightParen,
	"$":  Dollar,
	";":  Semicolon,
}

type Token struct {
	Type   TokenType
	Lexeme string
	Line   int
}

type lexer struct {
	line        int
	currentRune rune
	reader      io.RuneReader
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
	var toret Token
	for unicode.IsSpace(l.currentRune) && l.currentRune != '\n' {
		l.advance()
	}
	switch {
	case l.atEnd():
		toret = l.makeToken(Eof, "EOF")
	case l.currentRune == '\n':
		toret = l.newLine()
	case l.currentRune == '"':
		toret = l.string()
	case unicode.IsLetter(l.currentRune) || l.currentRune == '_':
		toret = l.identifier()
	case unicode.IsDigit(l.currentRune):
		toret = l.number()
	default:
		toret = l.punctuation()
	}
	output <- toret
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
		if l.currentRune != '\\' || prev == '\\' {
			l.currentRuneInside(&lexeme)
		}
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
	for !unicode.IsSpace(l.currentRune) && !unicode.IsLetter(l.currentRune) && !unicode.IsDigit(l.currentRune) && !l.atEnd() {
		fmt.Fprintf(&lexeme, "%c", l.currentRune)
		l.advance()
	}
	if t, ok := punctuations[lexeme.String()]; ok {
		return l.makeTokenFromBuilder(t, lexeme)
	}
	return l.makeErrorToken(fmt.Sprintf("undefined operator '%s'", lexeme.String()))
}

func (l *lexer) makeTokenFromBuilder(ttype TokenType, builder strings.Builder) Token {
	return l.makeToken(ttype, builder.String())
}

func (l *lexer) makeToken(ttype TokenType, lexeme string) Token {
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
