package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"

	"github.com/fioriandrea/aawk/interpreter"
	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
	"github.com/fioriandrea/aawk/resolver"
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
var memprofile = flag.String("memprofile", "", "write memory profile to `file`")

func main() {
	if len(os.Args[1:]) == 0 {
		log.Fatal("TODO help")
	}
	flag.Parse()

	if *cpuprofile != "" {
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

	builtinFunctions := make([]string, 0, len(interpreter.Builtins))
	for name := range interpreter.Builtins {
		builtinFunctions = append(builtinFunctions, name)
	}
	globalindices, functionindices, err := resolver.ResolveVariables(tree, builtinFunctions)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	/*b, err := json.MarshalIndent(tree, "", "\t")
	if err != nil {
		fmt.Println("error:", err)
	}
	fmt.Println(string(b))*/

	err = interpreter.Run(tree, args, globalindices, functionindices)

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		defer f.Close() // error handling omitted for example
		runtime.GC()    // get up-to-date statistics
		if err := pprof.WriteHeapProfile(f); err != nil {
			log.Fatal("could not write memory profile: ", err)
		}
	}

	if ee, ok := err.(interpreter.ErrorExit); ok {
		if ee.Status != 0 {
			os.Exit(ee.Status)
		}
	} else if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
