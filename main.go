package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
	"github.com/fioriandrea/aawk/runtime"
)

func main() {
	filepath := os.Args[1]
	filereader, err := os.Open(filepath)
	if err != nil {
		log.Fatal(err)
	}

	reader := bufio.NewReader(filereader)
	tokens := make(chan lexer.Token, 10)
	go lexer.GetTokens(reader, tokens)
	tree := parser.GetSyntaxTree(tokens)
	b, err := json.MarshalIndent(tree, "", "\t")
	if err != nil {
		fmt.Println("error:", err)
	}
	fmt.Println(string(b))
	runtime.Run(tree)
}
