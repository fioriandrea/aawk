package runtime

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
)

type RuneReadCloser interface {
	io.RuneReader
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

func spawnOutProgram(name string) io.WriteCloser {
	cmd := exec.Command("sh", "-c", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	res := outcommand{
		stdin: stdin,
		cmd:   cmd,
	}
	return res
}

func spawnOutFile(name string, mode int) io.WriteCloser {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|mode, 0600)
	if err != nil {
		log.Fatal(err)
	}
	return f
}

func spawnFileAppend(name string) io.WriteCloser {
	return spawnOutFile(name, os.O_APPEND)
}

func spawnFileNormal(name string) io.WriteCloser {
	return spawnOutFile(name, 0)
}

type streams map[string]io.Closer

func (st streams) get(name string, spawner func(string) io.Closer) io.Closer {
	s, ok := st[name]
	if ok {
		return s
	}
	st[name] = spawner(name)
	return st[name]
}

func (st streams) close(name string) error {
	s, ok := st[name]
	if !ok {
		return nil
	}
	delete(st, name)
	return s.Close()
}

func (st streams) closeAll() {
	for name := range st {
		st.close(name)
	}
}

type outwriters struct {
	streams streams
}

func newOutWriters() outwriters {
	return outwriters{
		streams: streams{},
	}
}

func (ow outwriters) get(name string, spawner func(string) io.WriteCloser) io.WriteCloser {
	f := func(name string) io.Closer {
		return io.Closer(spawner(name))
	}
	return ow.streams.get(name, f).(io.WriteCloser)
}

func (ow outwriters) close(name string) error {
	return ow.streams.close(name)
}

func (ow outwriters) closeAll() {
	ow.streams.closeAll()
}

type inreaders struct {
	streams streams
}

func newInReaders() inreaders {
	return inreaders{
		streams: streams{},
	}
}

func (ir inreaders) get(name string, spawner func(string) RuneReadCloser) RuneReadCloser {
	f := func(name string) io.Closer {
		return io.Closer(spawner(name))
	}
	return ir.streams.get(name, f).(RuneReadCloser)
}

func (ir inreaders) close(name string) error {
	return ir.streams.close(name)
}

func (ir inreaders) closeAll() {
	ir.streams.closeAll()
}

type incommand struct {
	stdout *bufio.Reader
	cmd    *exec.Cmd
}

func (ic incommand) ReadRune() (rune, int, error) {
	r, size, err := ic.stdout.ReadRune()
	return r, size, err
}

func (ic incommand) Close() error {
	if err := ic.cmd.Wait(); err != nil {
		return err
	}
	return nil
}

func spawnInProgram(name string) RuneReadCloser {
	cmd := exec.Command("sh", "-c", name)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		log.Fatal(err)
	}
	res := incommand{
		stdout: bufio.NewReader(stdout),
		cmd:    cmd,
	}
	return res
}

type infile struct {
	reader *bufio.Reader
	file   *os.File
}

func (inf infile) ReadRune() (rune, int, error) {
	r, size, err := inf.reader.ReadRune()
	return r, size, err
}

func (inf infile) Close() error {
	return inf.file.Close()
}

func spawnInFile(name string) RuneReadCloser {
	file, err := os.Open(name)
	if err != nil {
		log.Fatal(err)
	}
	reader := bufio.NewReader(file)
	return infile{
		reader: reader,
		file:   file,
	}
}

func nextRecord(reader io.RuneReader, delim rune) (string, error) {
	var buff strings.Builder
	for {
		c, _, err := reader.ReadRune()
		if err != nil {
			if err != io.EOF {
				return "", err
			}
			str := buff.String()
			if len(str) == 0 {
				return "", err
			}
			return str, nil
		}
		if c == delim {
			break
		}
		fmt.Fprintf(&buff, "%c", c)
	}
	return buff.String(), nil
}
