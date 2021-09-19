package interpreter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

func getExprAtOrNil(i int, exprs []parser.Expr) parser.Expr {
	if i >= len(exprs) {
		return nil
	}
	return exprs[i]
}

type Callable interface {
	Call(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error)
}

type AwkFunction parser.FunctionDef

func (af AwkFunction) Call(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
	subenv := newEnvironment(inter.env, make([]awkvalue, len(af.Args)))

	linkarrays := map[int]parser.IdExpr{}
	for i := range af.Args {
		var arg parser.Expr
		if len(args) > 0 {
			arg = args[0]
			args = args[1:]
		}
		v, err := inter.eval(arg)
		if err != nil {
			return null(), err
		}
		subenv.locals[i] = v

		// undefined values could be used as an array
		if idexpr, ok := arg.(parser.IdExpr); ok && v.typ == Null {
			linkarrays[i] = idexpr
		}
	}

	for _, arg := range args {
		_, err := inter.eval(arg)
		if err != nil {
			return null(), err
		}
	}
	inter.env = subenv
	defer func() { inter.env = subenv.calling }()

	err := inter.execute(af.Body)
	var retval awkvalue
	if errRet, ok := err.(errorReturn); ok {
		retval = errRet.val
	} else if err != nil {
		return null(), err
	}

	// link back arrays
	inter.env = subenv.calling
	for local, calling := range linkarrays {
		v := subenv.locals[local]
		if v.typ == Array {
			inter.setVariable(calling, v)
		}
	}
	return retval, nil
}

type NativeFunction func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error)

func (nf NativeFunction) Call(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
	return nf(inter, called, args)
}

var Builtins = map[string]NativeFunction{
	"length": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return null(), inter.runtimeError(called, "too may arguments")
		}
		strv, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return null(), err
		}
		return awknumber(float64(len([]rune(inter.toGoString(strv))))), nil
	},

	"close": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		file, err := inter.eval(getExprAtOrNil(0, args))
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

	"sqrt": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return null(), inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return null(), err
		}
		num := inter.toGoFloat(n)
		if num < 0 {
			return null(), inter.runtimeError(called, "cannot compute sqrt of a negative number")
		}
		return awknumber(math.Sqrt(num)), nil
	},

	"log": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return null(), inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return null(), err
		}
		num := inter.toGoFloat(n)
		if num <= 0 {
			return null(), inter.runtimeError(called, "cannot compute log of a number <= 0")
		}
		return awknumber(math.Log(num)), nil
	},

	"sin": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return null(), inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return null(), err
		}
		num := inter.toGoFloat(n)
		return awknumber(math.Sin(num)), nil
	},

	"cos": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return null(), inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return null(), err
		}
		num := inter.toGoFloat(n)
		return awknumber(math.Cos(num)), nil
	},

	"exp": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return null(), inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return null(), err
		}
		num := inter.toGoFloat(n)
		return awknumber(math.Exp(num)), nil
	},

	"atan2": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 2 {
			return null(), inter.runtimeError(called, "too may arguments")
		}
		n1, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return null(), err
		}
		n2, err := inter.eval(getExprAtOrNil(1, args))
		if err != nil {
			return null(), err
		}
		num1 := inter.toGoFloat(n1)
		num2 := inter.toGoFloat(n2)
		return awknumber(math.Atan2(num1, num2)), nil
	},

	"int": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return null(), inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return null(), err
		}
		num := inter.toGoFloat(n)
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
			inter.rng.SetSeed(int64(inter.toGoFloat(seed)))
		}
		return awknumber(float64(ret)), nil
	},
}

// Source: https://github.com/benhoyt/goawk/blob/master/interp/functions.go
func (inter *interpreter) parseFmtTypes(print lexer.Token, s string) (format string, types []func(awkvalue) interface{}, err error) {
	if f, ok := inter.fprintfcache[s]; ok {
		format, types = f()
		return format, types, nil
	}

	tostr := func(v awkvalue) interface{} {
		return inter.toGoString(v)
	}
	tofloat := func(v awkvalue) interface{} {
		return inter.toGoFloat(v)
	}
	toint := func(v awkvalue) interface{} {
		return int(inter.toGoFloat(v))
	}
	touint := func(v awkvalue) interface{} {
		return uint(inter.toGoFloat(v))
	}
	tochar := func(v awkvalue) interface{} {
		return []rune(inter.toGoString(v))[0]
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
	formatstr, convs, err := inter.parseFmtTypes(print, inter.toGoString(format))
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
