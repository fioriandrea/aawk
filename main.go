/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/fioriandrea/aawk/interpreter"
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

	fs = " "

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
			if len(args[i]) > 0 && args[i][0] == '-' && args[i] != "--" {
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

func main() {
	fs, variables, program, remaining := parseCliArguments()

	errs := interpreter.ExecuteCL(fs, variables, program, os.Args[0], remaining, os.Stdin, os.Stdout, os.Stderr)
	for _, err := range errs {
		if ee, ok := err.(interpreter.ErrorExit); ok {
			os.Exit(ee.Status)
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], err.Error())
		}
	}
	if len(errs) > 0 {
		os.Exit(1)
	}
}
