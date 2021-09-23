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

	if rettype == Identifier && l.currentRune == '(' {
		rettype = IdentifierParen
		l.advance()
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
		l.advanceInside(&lexeme)
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
