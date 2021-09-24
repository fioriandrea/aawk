/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package interpreter

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

type resources map[string]io.Closer

func (st resources) get(name string, spawner func(string) (io.Closer, error)) (io.Closer, error) {
	s, ok := st[name]
	if ok {
		return s, nil
	}
	s, err := spawner(name)
	if err != nil {
		return nil, err
	}
	st[name] = s
	return s, nil
}

func (st resources) close(name string) error {
	s, ok := st[name]
	if !ok {
		return nil
	}
	delete(st, name)
	return s.Close()
}

func (st resources) closeAll() []error {
	errors := make([]error, 0)
	for name := range st {
		err := st.close(name)
		if err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}

type ByteReadCloser interface {
	io.ByteReader
	io.Closer
}

type outcommand struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

func (c outcommand) Write(p []byte) (int, error) {
	n, err := c.stdin.Write(p)
	return n, err
}

func (c outcommand) Close() error {
	if err := c.stdin.Close(); err != nil {
		return err
	}
	if err := c.cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func spawnOutCommand(name string, stdout io.Writer, stderr io.Writer) (outcommand, error) {
	cmd := exec.Command("sh", "-c", name)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return outcommand{}, err
	}
	if err := cmd.Start(); err != nil {
		return outcommand{}, err
	}
	res := outcommand{
		stdin: stdin,
		cmd:   cmd,
	}
	return res, nil
}

func spawnOutFile(name string, mode int) (*os.File, error) {
	return os.OpenFile(name, os.O_CREATE|os.O_WRONLY|mode, 0600)
}

type incommand struct {
	stdout *bufio.Reader
	cmd    *exec.Cmd
}

func (ic incommand) ReadByte() (byte, error) {
	return ic.stdout.ReadByte()
}

func (ic incommand) Close() error {
	if err := ic.cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func spawnInCommand(name string, stdin io.Reader, stderr io.Writer) (incommand, error) {
	cmd := exec.Command("sh", "-c", name)
	cmd.Stdin = stdin
	cmd.Stderr = stderr
	stdoutp, err := cmd.StdoutPipe()
	if err != nil {
		return incommand{}, err
	}
	if err := cmd.Start(); err != nil {
		return incommand{}, err
	}
	res := incommand{
		stdout: bufio.NewReader(stdoutp),
		cmd:    cmd,
	}
	return res, nil
}

type infile struct {
	reader io.ByteReader
	file   *os.File
}

func (inf infile) ReadByte() (byte, error) {
	return inf.reader.ReadByte()
}

func (inf infile) Close() error {
	return inf.file.Close()
}

func spawnInFile(name string) (infile, error) {
	file, err := os.Open(name)
	if err != nil {
		return infile{}, err
	}
	reader := bufio.NewReader(file)
	return infile{
		reader: reader,
		file:   file,
	}, nil
}

func (inter *interpreter) nextRecord(r io.ByteReader) (string, error) {
	return nextRecord(r, inter.getRs())
}

func (inter *interpreter) nextRecordCurrentFile() (string, error) {
	s, err := inter.nextRecord(inter.currentFile)
	if err == nil {
		inter.builtins[parser.Nr] = Awknumber(inter.builtins[parser.Nr].Float() + 1)
		inter.builtins[parser.Fnr] = Awknumber(inter.builtins[parser.Fnr].Float() + 1)
		return s, err
	} else if err != io.EOF {
		return "", err
	}
	if cl, ok := inter.currentFile.(io.Closer); ok {
		if err := cl.Close(); err != nil {
			return "", err
		}
	}
	for {
		inter.argindex++
		if inter.argindex > int(inter.builtins[parser.Argc].Float()) {
			// No file has ever been processed, so start processing stdin
			if inter.currentFile == nil {
				inter.currentFile = inter.stdinFile
				return inter.nextRecordCurrentFile()
			}
			break
		}
		fname := inter.toGoString(inter.builtins[parser.Argv].array[fmt.Sprintf("%d", inter.argindex)])
		if fname == "" {
			continue
		} else if lexer.CommandLineAssignRegex.MatchString(fname) {
			inter.assignCommandLineString(fname)
			continue
		} else if fname == "-" {
			inter.currentFile = inter.stdinFile
		} else {
			file, err := os.Open(fname)
			if err != nil {
				return "", err
			}
			inter.currentFile = infile{
				reader: bufio.NewReader(file),
				file:   file,
			}
		}
		inter.builtins[parser.Filename] = Awknormalstring(fname)
		return inter.nextRecordCurrentFile()
	}
	return s, io.EOF
}

func nextRecord(reader io.ByteReader, delim string) (string, error) {
	if reader == nil {
		return "", io.EOF
	} else if delim == "" {
		return nextMultilineRecord(reader)
	} else {
		return nextSimpleRecord(reader, delim[0])
	}
}

func nextMultilineRecord(reader io.ByteReader) (string, error) {
	var buff strings.Builder
	err := skipBlanks(&buff, reader)
	if err != nil {
		return "", err
	}
	for {
		s, err := nextSimpleRecord(reader, '\n')
		if err != nil {
			return handleEndOfInput(buff.String(), err)
		}
		if s == "" {
			break
		}
		fmt.Fprintf(&buff, "\n%s", s)
	}
	return buff.String(), nil
}

func nextSimpleRecord(reader io.ByteReader, delim byte) (string, error) {
	var buff strings.Builder
	for {
		c, err := reader.ReadByte()
		if err != nil {
			return handleEndOfInput(buff.String(), err)
		}
		if c == delim {
			break
		}
		buff.WriteByte(c)
	}
	return buff.String(), nil
}

func skipBlanks(buff io.Writer, reader io.ByteReader) error {
	for {
		s, err := nextSimpleRecord(reader, '\n')
		if err != nil {
			return err
		}
		if s != "" {
			fmt.Fprintf(buff, "%s", s)
			break
		}
	}
	return nil
}

func handleEndOfInput(cum string, err error) (string, error) {
	if err != io.EOF {
		return "", err
	}
	if len(cum) == 0 {
		return "", io.EOF
	}
	return cum, nil
}
