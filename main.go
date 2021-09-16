package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
	"github.com/fioriandrea/aawk/runtime"
)

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

var filenamePath = flag.String("f", "", "awk program file")

func main() {
	if len(os.Args[1:]) == 0 {
		log.Fatal("TODO help")
	}
	flag.Parse()
	args := flag.Args()
	var progreader io.RuneReader
	if isFlagPassed("f") {
		file, err := os.Open(*filenamePath)
		if err != nil {
			log.Fatal(err)
		}
		progreader = bufio.NewReader(file)
	} else {
		progreader = strings.NewReader(args[0])
		args = args[1:]
	}

	lexer := lexer.NewLexer(progreader)
	tree, err := parser.GetSyntaxTree(lexer)
	if err != nil {
		os.Exit(1)
	}
	/*b, err := json.MarshalIndent(tree, "", "\t")
	if err != nil {
		fmt.Println("error:", err)
	}
	fmt.Println(string(b))*/
	err = runtime.Run(tree, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
