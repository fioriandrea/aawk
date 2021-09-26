/*
 * Copyright (C) 2021 Andrea Fiori <andrea.fiori.1998@gmail.com>
 *
 * Licensed under GPLv2, see file LICENSE in this source tree.
 */

package interpreter

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	Null Awkvaluetype = iota
	Number
	Normalstring
	Numericstring
	Array
)

type Awkvaluetype int

// Why a struct instead of an interface, you might ask. Interfaces allocated
// too much memory (strings don't fit into an interface), whereas structs do
// not have this problem. The ideal would have been to have C-style unions.

type Awkvalue struct {
	Typ   Awkvaluetype
	N     float64
	Str   string
	Array map[string]Awkvalue
}

func stringToNumber(s string) float64 {
	var f float64
	fmt.Sscan(s, &f)
	return f
}

func numberToString(n float64, format string) string {
	if math.Trunc(n) == n {
		return fmt.Sprintf("%d", int64(n))
	} else {
		return fmt.Sprintf(format, n)
	}
}

func (v Awkvalue) Float() float64 {
	if v.Typ == Normalstring {
		return stringToNumber(v.Str)
	}
	return v.N
}

func (v Awkvalue) Bool() bool {
	if v.Typ == Normalstring {
		return v.Str != ""
	}
	return v.N != 0
}

func (v Awkvalue) String(format string) string {
	if v.Typ != Number {
		return v.Str
	}
	return numberToString(v.N, format)
}

func Awknormalstring(s string) Awkvalue {
	return Awkvalue{
		Typ: Normalstring,
		Str: s,
	}
}

func Awknumericstring(s string) Awkvalue {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return Awknormalstring(s)
	}
	return Awkvalue{
		Typ: Numericstring,
		Str: s,
		N:   f,
	}
}

func Awknumber(n float64) Awkvalue {
	return Awkvalue{
		Typ: Number,
		N:   n,
	}
}

func Awkarray(m map[string]Awkvalue) Awkvalue {
	return Awkvalue{
		Typ:   Array,
		Array: m,
	}
}

var Awknull = Awkvalue{}

func (inter *interpreter) toGoString(v Awkvalue) string {
	return v.String(inter.getConvfmt())
}

func nullToArray(v Awkvalue) Awkvalue {
	if v.Array != nil {
		v.Typ = Array
	} else {
		v = Awkarray(map[string]Awkvalue{})
	}
	return v
}
