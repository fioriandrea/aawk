package interpreter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

type Callable interface {
	Call(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error)
}

type UserFunction struct {
	Arity int
	Body  parser.BlockStat
}

func (inter *interpreter) giveStackFrame(size int) ([]awkvalue, int) {
	if inter.stackcount+size > cap(inter.stack) {
		return make([]awkvalue, size), 0
	}
	inter.stackcount += size
	return inter.stack[inter.stackcount-size : inter.stackcount], size
}

func (inter *interpreter) releaseStackFrame(size int) {
	inter.stackcount -= size
}

func (f *UserFunction) Call(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
	sublocals, size := inter.giveStackFrame(f.Arity)

	linkarrays := map[int]*parser.IdExpr{}
	for i := 0; i < f.Arity; i++ {
		var arg parser.Expr
		if len(args) > 0 {
			arg = args[0]
			args = args[1:]
		}
		v, err := inter.eval(arg)
		if err != nil {
			return null(), err
		}
		sublocals[i] = v

		// undefined values could be used as arrays
		if idexpr, ok := arg.(*parser.IdExpr); ok && v.typ == Null {
			linkarrays[i] = idexpr
		}
	}

	for _, arg := range args {
		_, err := inter.eval(arg)
		if err != nil {
			return null(), err
		}
	}

	prevlocals := inter.locals
	inter.locals = sublocals

	defer func() {
		// link back arrays
		inter.locals = prevlocals
		inter.releaseStackFrame(size)

		for local, calling := range linkarrays {
			v := sublocals[local]
			if v.typ == Array {
				inter.setVariable(calling, v)
			}
		}
	}()

	err := inter.execute(f.Body)
	var retval awkvalue
	if errRet, ok := err.(errorReturn); ok {
		retval = awkvalue(errRet)
	} else if err != nil {
		return null(), err
	}

	return retval, nil
}

type NativeFunction func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error)

func (nf NativeFunction) Call(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
	return nf(inter, called, args)
}

var Builtinfuncs = map[string]NativeFunction{
	// Arithmetic functions

	"atan2": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 2 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n1, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		n2, err := inter.eval(args[1])
		if err != nil {
			return null(), err
		}
		num1 := n1.float()
		num2 := n2.float()
		return awknumber(math.Atan2(num1, num2)), nil
	},

	"cos": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		return awknumber(math.Cos(num)), nil
	},

	"sin": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		return awknumber(math.Sin(num)), nil
	},

	"exp": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		return awknumber(math.Exp(num)), nil
	},

	"log": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		if num <= 0 {
			return null(), inter.runtimeError(called, "cannot compute log of a number <= 0")
		}
		return awknumber(math.Log(num)), nil
	},

	"sqrt": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		if num < 0 {
			return null(), inter.runtimeError(called, "cannot compute sqrt of a negative number")
		}
		return awknumber(math.Sqrt(num)), nil
	},

	"int": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		n, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		num := n.float()
		return awknumber(float64(int(num))), nil
	},

	"rand": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 0 {
			return null(), inter.runtimeError(called, "too may arguments")
		}
		n := inter.rng.Float64()
		return awknumber(n), nil
	},

	"srand": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return null(), inter.runtimeError(called, "too many arguments")
		}
		ret := inter.rng.rngseed
		if len(args) == 0 {
			inter.rng.SetSeed(time.Now().UTC().UnixNano())
		} else {
			seed, err := inter.eval(args[0])
			if err != nil {
				return null(), err
			}
			inter.rng.SetSeed(int64(seed.float()))
		}
		return awknumber(float64(ret)), nil
	},

	// String functions

	"index": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 2 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		v0, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		v1, err := inter.eval(args[1])
		if err != nil {
			return null(), err
		}
		str := inter.toGoString(v0)
		substr := inter.toGoString(v1)
		return awknumber(float64(strings.Index(str, substr) + 1)), nil
	},

	"sprintf": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) == 0 {
			args = append(args, nil)
		}
		var str strings.Builder
		err := inter.fprintf(&str, called, args)
		if err != nil {
			return null(), err
		}
		return awknormalstring(str.String()), nil
	},

	"substr": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) == 2 {
			args = append(args, nil)
		}
		if len(args) != 3 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		vs, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		s := []rune(inter.toGoString(vs))
		vm, err := inter.eval(args[1])
		if err != nil {
			return null(), err
		}
		m := int(vm.float()) - 1
		if m < 0 {
			m = 0
		} else if m > len(s) {
			m = len(s)
		}
		var n int
		if args[2] == nil {
			n = len(s)
		} else {
			vn, err := inter.eval(args[2])
			if err != nil {
				return null(), err
			}
			n = int(vn.float())
		}
		if n < 0 {
			n = 0
		} else if n+m > len(s) {
			n = len(s) - m
		}
		return awknormalstring(string(s[m : m+n])), nil
	},

	"tolower": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		v, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		return awknormalstring(strings.ToLower(inter.toGoString(v))), nil
	},

	"toupper": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		v, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		return awknormalstring(strings.ToUpper(inter.toGoString(v))), nil
	},

	// IO functions

	"close": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		file, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		str := inter.toGoString(file)
		opr := inter.outprograms.close(str)
		oprn := 0
		if opr != nil {
			oprn = 1
		}
		of := inter.outfiles.close(str)
		ofn := 0
		if of != nil {
			ofn = 1
		}
		ipr := inter.inprograms.close(str)
		iprn := 0
		if ipr != nil {
			iprn = 1
		}

		return awknumber(float64(oprn + ofn + iprn)), nil
	},

	"system": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) != 1 {
			return null(), inter.runtimeError(called, "incorrect number of arguments")
		}
		v, err := inter.eval(args[0])
		if err != nil {
			return null(), err
		}
		cmdstr := inter.toGoString(v)
		return awknumber(float64(System(cmdstr))), nil
	},
}

func System(cmd string) int {
	c := exec.Command("sh", "-c", cmd)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	err := c.Run()

	if err == nil {
		return 0
	}

	// Figure out the exit code
	if ws, ok := c.ProcessState.Sys().(syscall.WaitStatus); ok {
		if ws.Exited() {
			return ws.ExitStatus()
		}

		if ws.Signaled() {
			return -int(ws.Signal())
		}
	}

	return -1
}

// Source: https://github.com/benhoyt/goawk/blob/master/interp/functions.go
func (inter *interpreter) parseFmtTypes(print lexer.Token, s string) (format string, types []func(awkvalue) interface{}, err error) {
	if f, ok := inter.fprintfcache[s]; ok {
		format, types = f()
		return format, types, nil
	}

	ofmt := inter.getOfmt()
	tostr := func(v awkvalue) interface{} {
		return v.string(ofmt)
	}
	tofloat := func(v awkvalue) interface{} {
		return v.float()
	}
	toint := func(v awkvalue) interface{} {
		return int(v.float())
	}
	touint := func(v awkvalue) interface{} {
		return uint(v.float())
	}
	tochar := func(v awkvalue) interface{} {
		return []rune(v.string(ofmt))[0]
	}

	out := []byte(s)
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			i++
			if i >= len(s) {
				return "", nil, errors.New("expected type specifier after %")
			}
			if s[i] == '%' {
				continue
			}
			for i < len(s) && bytes.IndexByte([]byte(" .-+*#0123456789"), s[i]) >= 0 {
				if s[i] == '*' {
					types = append(types, toint)
				}
				i++
			}
			if i >= len(s) {
				return "", nil, errors.New("expected type specifier after %")
			}
			var t func(awkvalue) interface{}
			switch s[i] {
			case 's':
				t = tostr
			case 'd', 'i', 'o', 'x', 'X':
				t = toint
			case 'f', 'e', 'E', 'g', 'G':
				t = tofloat
			case 'u':
				t = touint
				out[i] = 'd'
			case 'c':
				t = tochar
				out[i] = 's'
			default:
				return "", nil, fmt.Errorf("invalid format type %q", s[i])
			}
			types = append(types, t)
		}
	}

	format = string(out)
	if len(inter.fprintfcache) < 100 {
		inter.fprintfcache[s] = func() (string, []func(awkvalue) interface{}) { return format, types }
	}
	return format, types, nil
}

func (inter *interpreter) fprintf(w io.Writer, print lexer.Token, exprs []parser.Expr) error {
	format, err := inter.eval(exprs[0])
	if err != nil {
		return err
	}
	formatstr, convs, err := inter.parseFmtTypes(print, format.string(inter.getConvfmt()))
	if err != nil {
		return nil
	}
	if len(convs) > len(exprs[1:]) {
		return inter.runtimeError(print, "run out of arguments for formatted output")
	}
	args := make([]interface{}, 0, len(convs))
	for _, expr := range exprs[1:] {
		arg, err := inter.eval(expr)
		if err != nil {
			return err
		}
		if arg.typ == Array {
			return inter.runtimeError(print, "cannot print array")
		}
		args = append(args, convs[0](arg))
		convs = convs[1:]
	}
	fmt.Fprintf(w, formatstr, args...)
	return nil
}
