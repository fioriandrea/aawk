package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/fioriandrea/aawk/interpreter"
	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
	"github.com/fioriandrea/aawk/resolver"
)

func printHelp(w io.Writer) {
	helpstr := `aawk — pattern scanning and processing language

SYNOPSIS
	aawk [-F sepstring] [-v assignment]... program [argument...]
 
	aawk [-F sepstring] -f progfile [-f progfile]... [-v assignment]...  [argument...]`
	fmt.Fprintf(w, "%s\n", helpstr)
}

func programError(msg string) error {
	return fmt.Errorf("%s: %s", os.Args[0], msg)
}

func parseCliError(msg string) {
	fmt.Fprintln(os.Stderr, programError(msg))
	os.Exit(1)
}

func expectedArgument(opt string) {
	parseCliError(fmt.Sprintf("expected parameter for option %s", opt))
}

func parseCliArguments() (fs string, variables []string, program io.RuneReader, remaining []string) {
	if len(os.Args[1:]) == 0 {
		printHelp(os.Stderr)
		os.Exit(1)
	}

	var i int
	var programfiles []io.Reader

	args := os.Args[1:]
outer:
	for ; i < len(args); i++ {
		switch args[i] {
		case "-h":
			fallthrough
		case "--help":
			printHelp(os.Stdout)
			os.Exit(0)
		case "-F":
			if i >= len(args) {
				expectedArgument(args[i])
			}
			i++
			fs = args[i]
			if _, err := regexp.Compile(fs); err != nil {
				parseCliError("invalid field separator")
			}
		case "-f":
			if i >= len(args) {
				expectedArgument(args[i])
			}
			i++
			fname := args[i]
			file, err := os.Open(fname)
			if err != nil {
				log.Fatal(err)
			}
			programfiles = append(programfiles, file)
		case "-v":
			if i >= len(args) {
				expectedArgument(args[i])
			}
			i++
			variables = append(variables, args[i])
		default:
			if args[i][0] == '-' && args[i] != "--" {
				parseCliError(fmt.Sprintf("unexpected option %s", args[i]))
			}
			break outer
		}
	}
	if len(programfiles) == 0 && i >= len(args) {
		parseCliError("expected program string")
	} else if len(programfiles) == 0 {
		program = strings.NewReader(args[i])
		i++
	} else {
		program = bufio.NewReader(io.MultiReader(programfiles...))
	}
	remaining = args[i:]

	return fs, variables, program, remaining
}

func Exec(fs string, variables []string, program io.RuneReader, arguments []string) error {
	if _, err := regexp.Compile(fs); err != nil {
		return programError("invalid FS")
	}

	lex := lexer.NewLexer(program)
	items, err := parser.GetItems(lex)
	if err != nil {
		return err
	}

	builtinFunctions := make([]string, 0, len(lexer.Builtinfuncs))
	for name := range lexer.Builtinfuncs {
		builtinFunctions = append(builtinFunctions, name)
	}

	globalindices, functionindices, err := resolver.ResolveVariables(items, builtinFunctions)
	if err != nil {
		return err
	}

	globalpreassign := map[int]string{}
	builtinpreassing := map[int]string{}
	for _, variable := range variables {
		splits := strings.Split(variable, "=")
		if i, ok := lexer.Builtinvars[splits[0]]; ok {
			builtinpreassing[i] = splits[1]
		} else if i, ok := globalindices[splits[0]]; ok {
			globalpreassign[i] = splits[1]
		}
	}

	return interpreter.Run(interpreter.RunParams{
		Items:            items,
		Fs:               fs,
		Arguments:        arguments,
		Globalindices:    globalindices,
		Functionindices:  functionindices,
		Builtinpreassing: builtinpreassing,
		Globalpreassign:  globalpreassign,
	})
}

func main() {
	program, variables, fs, remaining := parseCliArguments()

	err := Exec(program, variables, fs, remaining)
	if ee, ok := err.(interpreter.ErrorExit); ok {
		os.Exit(ee.Status)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
