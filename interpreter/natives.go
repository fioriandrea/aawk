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

/* Can handle basic numeric types, strings, Awkvalue and map[string]Awkvalue.  It
 * cannot handle generic maps. Although it would be easy to go from native ->
 * awk, it would be really hard to go from awk -> native because awk values can
 * be either strings or numeric strings or numbers or undefined. Go maps must
 * have a single type. It is also hard to use interfaces because the map would
 * have to be copied (i.e. not passed by reference). For this reason, maps can
 * only take Awkvalue as values.
 */
func (inter *interpreter) evalNativeFunction(called lexer.Token, f interface{}, exprargs []parser.Expr) (Awkvalue, error) {
	ftype := reflect.TypeOf(f)

	// Check number of arguments
	if !ftype.IsVariadic() && ftype.NumIn() != len(exprargs) {
		return Awknil(), inter.runtimeError(called, fmt.Sprintf("wrong number of arguments (expected %d)", ftype.NumIn()))
	} else if ftype.IsVariadic() && len(exprargs) < ftype.NumIn()-1 {
		return Awknil(), inter.runtimeError(called, fmt.Sprintf("wrong number of arguments for varadic function (expected at least %d)", ftype.NumIn()-1))
	}

	// Collect arguments
	args := make([]reflect.Value, 0)
	for i := 0; i < len(exprargs); i++ {
		expr := exprargs[i]
		awkarg, err := inter.evalArrayAllowed(expr)
		if err != nil {
			return Awknil(), err
		}

		var argtype reflect.Type
		if ftype.IsVariadic() && i >= ftype.NumIn()-1 {
			argtype = ftype.In(ftype.NumIn() - 1).Elem()
		} else {
			argtype = ftype.In(i)
		}

		// Array checks
		if awkarg.typ == Array && argtype.Kind() != reflect.Map {
			return Awknil(), inter.runtimeError(called, "cannot use array in scalar context")
		} else if awkarg.typ != Array && awkarg.typ != Null && argtype.Kind() == reflect.Map {
			return Awknil(), inter.runtimeError(called, "cannot use scalar in array context")
		} else if awkarg.typ == Null && argtype.Kind() == reflect.Map {
			if _, ok := expr.(*parser.IdExpr); !ok {
				return Awknil(), inter.runtimeError(expr.Token(), "cannot assing array to non variable")
			}
		}

		nativearg := awkvalueToNative(awkarg, argtype, inter.getConvfmt())
		args = append(args, nativearg)

		// Assign maps back to undefined variables
		if id, isid := expr.(*parser.IdExpr); argtype.Kind() == reflect.Map && awkarg.typ == Null && isid {
			defer inter.setVariable(id, nativeToAwkvalue(nativearg))
		}
	}

	ret := reflect.ValueOf(f).Call(args)
	if len(ret) == 0 {
		return Awknil(), nil
	} else if len(ret) == 1 {
		return nativeToAwkvalue(ret[0]), nil
	} else {
		return nativeToAwkvalue(ret[0]), ret[1].Interface().(error)
	}
}

func awkvalueToNative(v Awkvalue, nativetype reflect.Type, convfmt string) reflect.Value {
	switch nativetype.Kind() {
	case reflect.Int:
		return reflect.ValueOf(int(v.Float()))
	case reflect.Int8:
		return reflect.ValueOf(int8(v.Float()))
	case reflect.Int16:
		return reflect.ValueOf(int16(v.Float()))
	case reflect.Int32:
		return reflect.ValueOf(int32(v.Float()))
	case reflect.Int64:
		return reflect.ValueOf(int64(v.Float()))
	case reflect.Uint:
		return reflect.ValueOf(uint(v.Float()))
	case reflect.Uint8:
		return reflect.ValueOf(uint8(v.Float()))
	case reflect.Uint16:
		return reflect.ValueOf(uint16(v.Float()))
	case reflect.Uint32:
		return reflect.ValueOf(uint32(v.Float()))
	case reflect.Uint64:
		return reflect.ValueOf(uint64(v.Float()))
	case reflect.Float32:
		return reflect.ValueOf(float32(v.Float()))
	case reflect.Float64:
		return reflect.ValueOf(v.Float())
	case reflect.String:
		return reflect.ValueOf(v.String(convfmt))
	case reflect.Map:
		return reflect.ValueOf(v.array)
	case reflect.Struct: // Awkvalue
		return reflect.ValueOf(v)
	}
	panic(nativetype.Kind().String())
}

func nativeToAwkvalue(nat reflect.Value) Awkvalue {
	switch nat.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return Awknumber(float64(nat.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return Awknumber(float64(nat.Uint()))
	case reflect.Float32, reflect.Float64:
		return Awknumber(nat.Float())
	case reflect.String:
		return Awknormalstring(nat.String())
	case reflect.Map:
		return Awkarray(nat.Interface().(map[string]Awkvalue))
	case reflect.Struct:
		return nat.Interface().(Awkvalue)
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
		if ftype.IsVariadic() && i == ftype.NumIn()-1 && !isNativeTypeCompatible(intype.Elem()) || !ftype.IsVariadic() && !isNativeTypeCompatible(intype) {
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
	return ntype.Key().Kind() == reflect.String && ntype.Elem() == reflect.TypeOf(Awknil())
}

func isNativeTypeCompatibleScalar(ntype reflect.Type) bool {
	switch ntype.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.String:
		return true
	default:
		return false
	}
}
