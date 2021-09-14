package main

import (
	"bufio"
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
	lexer := lexer.NewLexer(reader)
	tree, err := parser.GetSyntaxTree(lexer)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	/*b, err := json.MarshalIndent(tree, "", "\t")
	if err != nil {
		fmt.Println("error:", err)
	}
	fmt.Println(string(b))*/
	err = runtime.Run(tree)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
