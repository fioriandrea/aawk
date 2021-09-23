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
	Null awkvaluetype = iota
	Number
	Normalstring
	Numericstring
	Array
)

type awkvaluetype int

type awkvalue struct {
	typ   awkvaluetype
	n     float64
	str   string
	array map[string]awkvalue
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

func (v awkvalue) float() float64 {
	if v.typ == Normalstring {
		return stringToNumber(v.str)
	}
	return v.n
}

func (v awkvalue) bool() bool {
	if v.typ == Normalstring {
		return v.str != ""
	}
	return v.n != 0
}

func (v awkvalue) string(format string) string {
	if v.typ != Number {
		return v.str
	}
	return numberToString(v.n, format)
}

func awknormalstring(s string) awkvalue {
	return awkvalue{
		typ: Normalstring,
		str: s,
	}
}

func awknumericstring(s string) awkvalue {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return awknormalstring(s)
	}
	return awkvalue{
		typ: Numericstring,
		str: s,
		n:   f,
	}
}

func awknumber(n float64) awkvalue {
	return awkvalue{
		typ: Number,
		n:   n,
	}
}

func awkarray(m map[string]awkvalue) awkvalue {
	return awkvalue{
		typ:   Array,
		array: m,
	}
}

func null() awkvalue {
	return awkvalue{}
}

func (inter *interpreter) toGoString(v awkvalue) string {
	return v.string(inter.getConvfmt())
}
