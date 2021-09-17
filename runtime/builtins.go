package runtime

import (
	"strings"

	"github.com/fioriandrea/aawk/parser"
)

func (inter *interpreter) evalClose(ce parser.CallExpr) (awkvalue, error) {
	if len(ce.Args) == 0 {
		ce.Args = append(ce.Args, nil)
	}
	file, err := inter.eval(ce.Args[0])
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
