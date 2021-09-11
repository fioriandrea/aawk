package main

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/fioriandrea/aawk/lexer"
)

func main() {
	text := "if (1.3e-12) { print 1, 2, \"ciao \\\"\"\nwhat if i knew i was in rome?"
	r := bufio.NewReader(strings.NewReader(text))
	t := make(chan lexer.Token)
	go lexer.GetTokens(r, t)
	for token := range t {
		fmt.Println(token)
		if token.Type == lexer.Eof {
			break
		}
	}
}
