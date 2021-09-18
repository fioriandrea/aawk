package runtime

import (
	"math"
	"strings"
	"time"

	"github.com/fioriandrea/aawk/parser"
)

func getExprAtOrNil(i int, exprs []parser.Expr) parser.Expr {
	if i >= len(exprs) {
		return nil
	}
	return exprs[i]
}

func (inter *interpreter) evalLength(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) > 1 {
		return nil, inter.runtimeError(ce.Called, "too may arguments")
	}
	strv, err := inter.eval(getExprAtOrNil(0, ce.Args))
	if err != nil {
		return nil, err
	}
	return awknumber(len([]rune(inter.toGoString(strv)))), nil
}

func (inter *interpreter) evalClose(ce parser.CallExpr) (awkvalue, error) {
	file, err := inter.eval(getExprAtOrNil(0, ce.Args))
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
}

func (inter *interpreter) evalSprintf(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) == 0 {
		ce.Args = append(ce.Args, nil)
	}
	var str strings.Builder
	err := inter.fprintf(&str, ce.Called, ce.Args)
	if err != nil {
		return nil, err
	}
	return awknormalstring(str.String()), nil
}

func (inter *interpreter) evalSqrt(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) > 1 {
		return nil, inter.runtimeError(ce.Called, "too may arguments")
	}
	n, err := inter.eval(getExprAtOrNil(0, ce.Args))
	if err != nil {
		return nil, err
	}
	num := inter.toGoFloat(n)
	if num < 0 {
		return nil, inter.runtimeError(ce.Called, "cannot compute sqrt of a negative number")
	}
	return awknumber(math.Sqrt(num)), nil
}

func (inter *interpreter) evalLog(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) > 1 {
		return nil, inter.runtimeError(ce.Called, "too may arguments")
	}
	n, err := inter.eval(getExprAtOrNil(0, ce.Args))
	if err != nil {
		return nil, err
	}
	num := inter.toGoFloat(n)
	if num <= 0 {
		return nil, inter.runtimeError(ce.Called, "cannot compute log of a number <= 0")
	}
	return awknumber(math.Log(num)), nil
}

func (inter *interpreter) evalSin(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) > 1 {
		return nil, inter.runtimeError(ce.Called, "too may arguments")
	}
	n, err := inter.eval(getExprAtOrNil(0, ce.Args))
	if err != nil {
		return nil, err
	}
	num := inter.toGoFloat(n)
	return awknumber(math.Sin(num)), nil
}

func (inter *interpreter) evalCos(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) > 1 {
		return nil, inter.runtimeError(ce.Called, "too may arguments")
	}
	n, err := inter.eval(getExprAtOrNil(0, ce.Args))
	if err != nil {
		return nil, err
	}
	num := inter.toGoFloat(n)
	return awknumber(math.Cos(num)), nil
}

func (inter *interpreter) evalExp(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) > 1 {
		return nil, inter.runtimeError(ce.Called, "too may arguments")
	}
	n, err := inter.eval(getExprAtOrNil(0, ce.Args))
	if err != nil {
		return nil, err
	}
	num := inter.toGoFloat(n)
	return awknumber(math.Exp(num)), nil
}

func (inter *interpreter) evalAtan2(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) > 2 {
		return nil, inter.runtimeError(ce.Called, "too may arguments")
	}
	n1, err := inter.eval(getExprAtOrNil(0, ce.Args))
	if err != nil {
		return nil, err
	}
	n2, err := inter.eval(getExprAtOrNil(1, ce.Args))
	if err != nil {
		return nil, err
	}
	num1 := inter.toGoFloat(n1)
	num2 := inter.toGoFloat(n2)
	return awknumber(math.Atan2(num1, num2)), nil
}

func (inter *interpreter) evalInt(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) > 1 {
		return nil, inter.runtimeError(ce.Called, "too may arguments")
	}
	n, err := inter.eval(getExprAtOrNil(0, ce.Args))
	if err != nil {
		return nil, err
	}
	num := inter.toGoFloat(n)
	return awknumber(int(num)), nil
}

func (inter *interpreter) evalRand(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) > 0 {
		return nil, inter.runtimeError(ce.Called, "too may arguments")
	}
	n := inter.rng.Float64()
	return awknumber(n), nil
}

func (inter *interpreter) evalSrand(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) > 1 {
		return nil, inter.runtimeError(ce.Called, "too many arguments")
	}
	ret := inter.rng.rngseed
	if len(ce.Args) == 0 {
		inter.rng.SetSeed(time.Now().UTC().UnixNano())
	} else {
		seed, err := inter.eval(ce.Args[0])
		if err != nil {
			return nil, err
		}
		inter.rng.SetSeed(int64(inter.toGoFloat(seed)))
	}
	return awknumber(ret), nil
}
