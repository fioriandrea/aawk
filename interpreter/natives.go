/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package interpreter

import (
	"fmt"
	"reflect"

	"github.com/fioriandrea/aawk/lexer"
	"github.com/fioriandrea/aawk/parser"
)

/* Can handle basic numeric types and map[string]string. Cannot handle generic maps.
 * Although it would be easy to go from native -> awk, it would be really hard to go
 * from awk -> native because awk values can be either strings or numeric strings or
 * numbers or undefined. Go maps must have a single type. Map values passed to a native
 * functions are reassigned to the corresponding awk variable at the end of the function
 * execution. The caveat is that all values are converted back to numeric strings. Most
 * of the time this won't matter though.
 */
func (inter *interpreter) evalNativeFunction(called lexer.Token, f interface{}, exprargs []parser.Expr) (awkvalue, error) {
	ftype := reflect.TypeOf(f)
	if ftype.NumIn() != len(exprargs) {
		return null(), inter.runtimeError(called, fmt.Sprintf("wrong number of arguments (expected %d)", ftype.NumIn()))
	}
	args := make([]reflect.Value, 0, len(exprargs))
	for i, expr := range exprargs {
		expr := expr
		awkarg, err := inter.evalArrayAllowed(expr)
		if err != nil {
			return null(), err
		}
		argtype := ftype.In(i)
		if awkarg.typ == Array && argtype.Kind() != reflect.Map {
			return null(), inter.runtimeError(called, "cannot use array in scalar context")
		} else if awkarg.typ != Array && awkarg.typ != Null && argtype.Kind() == reflect.Map {
			return null(), inter.runtimeError(called, "cannot use scalar in array context")
		} else if awkarg.typ == Null && argtype.Kind() == reflect.Map {
			if _, ok := expr.(*parser.IdExpr); !ok {
				return null(), inter.runtimeError(expr.Token(), "cannot assing array to non variable")
			}
		}
		nativearg := awkvalueToNative(awkarg, argtype, inter.getConvfmt())
		args = append(args, nativearg)
		if argtype.Kind() == reflect.Map {
			defer func() {
				// It must be an id if the type is array
				id := expr.(*parser.IdExpr)
				inter.setVariable(id, nativeToAwkvalue(nativearg))
			}()
		}
	}
	ret := reflect.ValueOf(f).Call(args)
	if len(ret) == 0 {
		return null(), nil
	} else if len(ret) == 1 {
		return nativeToAwkvalue(ret[0]), nil
	} else {
		return nativeToAwkvalue(ret[0]), ret[1].Interface().(error)
	}
}

func awkvalueToNative(v awkvalue, nativetype reflect.Type, convfmt string) reflect.Value {
	switch nativetype.Kind() {
	case reflect.Int:
		return reflect.ValueOf(int(v.float()))
	case reflect.Int8:
		return reflect.ValueOf(int8(v.float()))
	case reflect.Int16:
		return reflect.ValueOf(int16(v.float()))
	case reflect.Int32:
		return reflect.ValueOf(int32(v.float()))
	case reflect.Int64:
		return reflect.ValueOf(int64(v.float()))
	case reflect.Uint:
		return reflect.ValueOf(uint(v.float()))
	case reflect.Uint8:
		return reflect.ValueOf(uint8(v.float()))
	case reflect.Uint16:
		return reflect.ValueOf(uint16(v.float()))
	case reflect.Uint32:
		return reflect.ValueOf(uint32(v.float()))
	case reflect.Uint64:
		return reflect.ValueOf(uint64(v.float()))
	case reflect.Float32:
		return reflect.ValueOf(float32(v.float()))
	case reflect.Float64:
		return reflect.ValueOf(v.float())
	case reflect.String:
		return reflect.ValueOf(v.string(convfmt))
	case reflect.Map:
		res := map[string]string{}
		for k, av := range v.array {
			res[k] = av.string(convfmt)
		}
		return reflect.ValueOf(res)
	}
	panic(nativetype.Kind().String())
}

func nativeToAwkvalue(nat reflect.Value) awkvalue {
	switch nat.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return awknumber(float64(nat.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return awknumber(float64(nat.Uint()))
	case reflect.Float32, reflect.Float64:
		return awknumber(nat.Float())
	case reflect.String:
		return awknormalstring(nat.String())
	case reflect.Map:
		v := map[string]awkvalue{}
		iter := nat.MapRange()
		for iter.Next() {
			v[iter.Key().String()] = awknumericstring(iter.Value().String())
		}
		return awkarray(v)
	}
	panic(nat.Kind().String())
}

func validateNative(name string, native interface{}) error {
	ftype := reflect.TypeOf(native)
	if ftype.Kind() != reflect.Func {
		return fmt.Errorf("native %s is not a function", name)
	}
	for i := 0; i < ftype.NumIn(); i++ {
		intype := ftype.In(i)
		if !isNativeTypeCompatible(intype) {
			return fmt.Errorf("native %s's argument at position %d (%s) is incompatible with AWK", name, i, intype.String())
		}
	}
	if ftype.NumOut() == 1 {
		outtype := ftype.Out(0)
		if !isNativeTypeCompatibleScalar(outtype) {
			return fmt.Errorf("native %s's return value at position %d (%s) is incompatible with AWK", name, 0, outtype.String())
		}
	}
	if ftype.NumOut() == 2 {
		errtype := ftype.Out(1)
		if !errtype.Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			return fmt.Errorf("native %s's return value at position %d (%s) must implement error", name, 1, errtype.String())
		}
	}
	// The rest of the return values get ignored

	return nil
}

func isNativeTypeCompatible(ntype reflect.Type) bool {
	if isNativeTypeCompatibleScalar(ntype) {
		return true
	}
	if ntype.Kind() != reflect.Map {
		return false
	}
	return ntype.Key().Kind() == reflect.String && ntype.Elem().Kind() == reflect.String
}

func isNativeTypeCompatibleScalar(ntype reflect.Type) bool {
	switch ntype.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.String:
		return true
	default:
		return false
	}
}
