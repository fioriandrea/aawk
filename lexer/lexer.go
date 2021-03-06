/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package lexer

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

type Token struct {
	Type   TokenType
	Lexeme string
	Line   int
}

type Lexer struct {
	line          int
	currentRune   rune
	program       []rune
	previousToken Token
}

func NewLexer(program []byte) Lexer {
	lex := Lexer{
		line:    1,
		program: []rune(string(program))[0:0],
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
		case unicode.IsDigit(l.currentRune) || l.currentRune == '.':
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
		if l.currentRune == '\\' {
			l.advance()
			if l.currentRune != '/' {
				fmt.Fprintf(&lexeme, "%c", '\\')
			}
			l.advanceCurrentInside(&lexeme)
		} else if l.currentRune == '/' {
			break
		} else {
			l.advanceCurrentInside(&lexeme)
		}
	}
	if l.currentRune != '/' {
		return l.makeErrorToken("unterminated regex")
	}
	l.advance()
	_, err := regexp.Compile(lexeme.String())
	if err != nil {
		return l.makeErrorToken(err.Error())
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
		if l.currentRune == '\\' {
			l.advance()
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
				seq := hexToInt(cc)
				cc = l.advance()
				if isOctalDigit(cc) {
					seq = seq*8 + hexToInt(cc)
					cc = l.advance()
					if isOctalDigit(cc) {
						seq = seq*8 + hexToInt(cc)
						l.advance()
					}
				}
				c = rune(seq)
			case 'x':
				l.advance()
				cc := l.currentRune
				if !isHexDigit(cc) {
					c = 'x'
					break
				}
				seq := hexToInt(cc)
				cc = l.advance()
				if isHexDigit(cc) {
					seq = seq*16 + hexToInt(cc)
					l.advance()
				}
				c = rune(seq)
			default:
				c = l.currentRune
				l.advance()
			}
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
		l.advanceCurrentInside(&lexeme)
	}
	rettype := Identifier
	if t, ok := Keywords[lexeme.String()]; ok {
		rettype = t
	} else if t, ok := Builtinfuncs[lexeme.String()]; ok {
		rettype = t
	}

	if rettype == Identifier && l.currentRune == '(' {
		rettype = IdentifierParen
		l.advance()
	}

	return l.makeTokenFromBuilder(rettype, lexeme)
}

func (l *Lexer) number() Token {
	var lexeme strings.Builder
	for unicode.IsDigit(l.currentRune) {
		l.advanceCurrentInside(&lexeme)
	}
	if l.currentRune == '.' {
		l.advanceCurrentInside(&lexeme)
		if !unicode.IsDigit(l.currentRune) {
			return l.makeErrorToken(fmt.Sprintf("expected numbers after '.' in number literal after '%s'", lexeme.String()))
		}
		for unicode.IsDigit(l.currentRune) {
			l.advanceCurrentInside(&lexeme)
		}
	}
	tounread := 0
	if l.currentRune == 'e' || l.currentRune == 'E' {
		tounread++
		l.advanceCurrentInside(&lexeme)
		if l.currentRune == '+' || l.currentRune == '-' {
			tounread++
			l.advanceCurrentInside(&lexeme)
		}
		if !unicode.IsDigit(l.currentRune) {
			l.unread(tounread)
			return l.makeToken(Number, lexeme.String()[:len(lexeme.String())-tounread])
		}
		for unicode.IsDigit(l.currentRune) {
			l.advanceCurrentInside(&lexeme)
		}
	}
	return l.makeTokenFromBuilder(Number, lexeme)
}

func (l *Lexer) punctuation() Token {
	var lexeme strings.Builder
	currnode := punctuations
	for {
		if v, ok := currnode.longer[l.currentRune]; ok {
			l.advanceCurrentInside(&lexeme)
			currnode = v
		} else {
			break
		}
	}
	if currnode.current == Error {
		l.advanceCurrentInside(&lexeme)
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
	var c rune
	if len(l.program) < cap(l.program) {
		l.program = l.program[:len(l.program)+1]
		c = l.program[len(l.program)-1]
	}
	l.currentRune = c
	return l.currentRune
}

func (l *Lexer) deadvance() {
	l.program = l.program[:len(l.program)-1]
	l.currentRune = l.program[len(l.program)-1]
}

func (l *Lexer) currentRuneInside(builder *strings.Builder) {
	fmt.Fprintf(builder, "%c", l.currentRune)
}

func (l *Lexer) advanceCurrentInside(builder *strings.Builder) {
	l.currentRuneInside(builder)
	l.advance()
}

func (l *Lexer) unread(ntounread int) {
	for i := 0; i < ntounread; i++ {
		l.deadvance()
	}
}

func (l *Lexer) atEnd() bool {
	return l.currentRune == '\000'
}

func isHexDigit(c rune) bool {
	c = unicode.ToLower(c)
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
}

func hexToInt(c rune) int {
	c = unicode.ToLower(c)
	if c >= '0' && c <= '9' {
		return int(c - '0')
	}
	return int(c - 'a')
}

func isOctalDigit(c rune) bool {
	return c >= '0' && c <= '7'
}
