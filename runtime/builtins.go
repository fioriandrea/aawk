package runtime

import (
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
	subenv := newEnvironment(inter.env)

	linkarrays := map[string]string{}

	for _, argtok := range af.Args {
		var arg parser.Expr
		if len(args) > 0 {
			arg = args[0]
			args = args[1:]
		}
		v, err := inter.eval(arg)
		if err != nil {
			return nil, err
		}
		subenv.locals[argtok.Lexeme] = v

		// undefined values could be used as an array
		if idexpr, ok := arg.(parser.IdExpr); ok && v == nil {
			linkarrays[argtok.Lexeme] = idexpr.Id.Lexeme
		}
	}

	for _, arg := range args {
		_, err := inter.eval(arg)
		if err != nil {
			return nil, err
		}
	}
	inter.env = subenv
	defer func() { inter.env = subenv.calling }()

	err := inter.execute(af.Body)
	var retval awkvalue
	if errRet, ok := err.(errorReturn); ok {
		retval = errRet.val
	} else if err != nil {
		return nil, err
	}

	// link back arrays
	for local, calling := range linkarrays {
		v := inter.env.get(local, nil)
		if _, ok := v.(awkarray); ok {
			inter.env.calling.set(calling, v, inter.globals)
		}
	}
	return retval, nil
}

type NativeFunction func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error)

func (nf NativeFunction) Call(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
	return nf(inter, called, args)
}

var builtins = map[string]NativeFunction{
	"length": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return nil, inter.runtimeError(called, "too may arguments")
		}
		strv, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return nil, err
		}
		return awknumber(len([]rune(inter.toGoString(strv)))), nil
	},

	"close": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		file, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return nil, err
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

		return awknumber(oprn + ofn + iprn), nil
	},

	"sprintf": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) == 0 {
			args = append(args, nil)
		}
		var str strings.Builder
		err := inter.fprintf(&str, called, args)
		if err != nil {
			return nil, err
		}
		return awknormalstring(str.String()), nil
	},

	"sqrt": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return nil, inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return nil, err
		}
		num := inter.toGoFloat(n)
		if num < 0 {
			return nil, inter.runtimeError(called, "cannot compute sqrt of a negative number")
		}
		return awknumber(math.Sqrt(num)), nil
	},

	"log": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return nil, inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return nil, err
		}
		num := inter.toGoFloat(n)
		if num <= 0 {
			return nil, inter.runtimeError(called, "cannot compute log of a number <= 0")
		}
		return awknumber(math.Log(num)), nil
	},

	"sin": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return nil, inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return nil, err
		}
		num := inter.toGoFloat(n)
		return awknumber(math.Sin(num)), nil
	},

	"cos": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return nil, inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return nil, err
		}
		num := inter.toGoFloat(n)
		return awknumber(math.Cos(num)), nil
	},

	"exp": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return nil, inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return nil, err
		}
		num := inter.toGoFloat(n)
		return awknumber(math.Exp(num)), nil
	},

	"atan2": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 2 {
			return nil, inter.runtimeError(called, "too may arguments")
		}
		n1, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return nil, err
		}
		n2, err := inter.eval(getExprAtOrNil(1, args))
		if err != nil {
			return nil, err
		}
		num1 := inter.toGoFloat(n1)
		num2 := inter.toGoFloat(n2)
		return awknumber(math.Atan2(num1, num2)), nil
	},

	"int": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return nil, inter.runtimeError(called, "too may arguments")
		}
		n, err := inter.eval(getExprAtOrNil(0, args))
		if err != nil {
			return nil, err
		}
		num := inter.toGoFloat(n)
		return awknumber(int(num)), nil
	},

	"rand": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 0 {
			return nil, inter.runtimeError(called, "too may arguments")
		}
		n := inter.rng.Float64()
		return awknumber(n), nil
	},

	"srand": func(inter *interpreter, called lexer.Token, args []parser.Expr) (awkvalue, error) {
		if len(args) > 1 {
			return nil, inter.runtimeError(called, "too many arguments")
		}
		ret := inter.rng.rngseed
		if len(args) == 0 {
			inter.rng.SetSeed(time.Now().UTC().UnixNano())
		} else {
			seed, err := inter.eval(args[0])
			if err != nil {
				return nil, err
			}
			inter.rng.SetSeed(int64(inter.toGoFloat(seed)))
		}
		return awknumber(ret), nil
	},
}
