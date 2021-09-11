package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

func main() {
	filepath := os.Args[1]
	filereader, err := os.Open(filepath)
	if err != nil {
		log.Fatal(err)
	}

	r := bufio.NewReader(filereader)
	t := make(chan lexer.Token, 10)
	go lexer.GetTokens(r, t)
	b, err := json.MarshalIndent(parser.GetSyntaxTree(t), "", "\t")
	if err != nil {
		fmt.Println("error:", err)
	}
	fmt.Println(string(b))
}
