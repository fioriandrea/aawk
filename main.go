package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/pprof"
	"strings"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
	"github.com/fioriandrea/aawk/resolver"
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
var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")

func main() {
	if len(os.Args[1:]) == 0 {
		log.Fatal("TODO help")
	}
	flag.Parse()

	if isFlagPassed("cpuprofile") {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

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

	builtinFunctions := make([]string, 0, len(runtime.Builtins))
	for name, _ := range runtime.Builtins {
		builtinFunctions = append(builtinFunctions, name)
	}
	tree, globalindices, functionindices, err := resolver.ResolveVariables(tree, builtinFunctions)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	err = runtime.Run(tree, args, globalindices, functionindices)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
