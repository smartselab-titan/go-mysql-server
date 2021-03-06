package expression

import (
	"fmt"
	"reflect"
	"time"

	errors "gopkg.in/src-d/go-errors.v1"
	"gopkg.in/src-d/go-vitess.v1/vt/sqlparser"

	"gopkg.in/src-d/go-mysql-server.v0/sql"
)

var (
	// errUnableToCast means that we could not find common type for two arithemtic objects
	errUnableToCast = errors.NewKind("Unable to cast between types: %T, %T")

	// errUnableToEval means that we could not evaluate an expression
	errUnableToEval = errors.NewKind("Unable to evaluate an expression: %v %s %v")
)

// Arithmetic expressions (+, -, *, /, ...)
type Arithmetic struct {
	BinaryExpression
	Op string
}

// NewArithmetic creates a new Arithmetic sql.Expression.
func NewArithmetic(left, right sql.Expression, op string) *Arithmetic {
	return &Arithmetic{BinaryExpression{Left: left, Right: right}, op}
}

// NewPlus creates a new Arithmetic + sql.Expression.
func NewPlus(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.PlusStr)
}

// NewMinus creates a new Arithmetic - sql.Expression.
func NewMinus(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.MinusStr)
}

// NewMult creates a new Arithmetic * sql.Expression.
func NewMult(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.MultStr)
}

// NewDiv creates a new Arithmetic / sql.Expression.
func NewDiv(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.DivStr)
}

// NewShiftLeft creates a new Arithmetic << sql.Expression.
func NewShiftLeft(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.ShiftLeftStr)
}

// NewShiftRight creates a new Arithmetic >> sql.Expression.
func NewShiftRight(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.ShiftRightStr)
}

// NewBitAnd creates a new Arithmetic & sql.Expression.
func NewBitAnd(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.BitAndStr)
}

// NewBitOr creates a new Arithmetic | sql.Expression.
func NewBitOr(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.BitOrStr)
}

// NewBitXor creates a new Arithmetic ^ sql.Expression.
func NewBitXor(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.BitXorStr)
}

// NewIntDiv creates a new Arithmetic div sql.Expression.
func NewIntDiv(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.IntDivStr)
}

// NewMod creates a new Arithmetic % sql.Expression.
func NewMod(left, right sql.Expression) *Arithmetic {
	return NewArithmetic(left, right, sqlparser.ModStr)
}

func (a *Arithmetic) String() string {
	return fmt.Sprintf("%s %s %s", a.Left, a.Op, a.Right)
}

// Type returns the greatest type for given operation.
func (a *Arithmetic) Type() sql.Type {
	switch a.Op {
	case sqlparser.PlusStr, sqlparser.MinusStr, sqlparser.MultStr, sqlparser.DivStr:
		_, lok := a.Left.(*Interval)
		_, rok := a.Right.(*Interval)
		if lok || rok {
			return sql.Timestamp
		}

		if sql.IsInteger(a.Left.Type()) && sql.IsInteger(a.Right.Type()) {
			if sql.IsUnsigned(a.Left.Type()) && sql.IsUnsigned(a.Right.Type()) {
				return sql.Uint64
			}
			return sql.Int64
		}

		return sql.Float64

	case sqlparser.ShiftLeftStr, sqlparser.ShiftRightStr:
		return sql.Uint64

	case sqlparser.BitAndStr, sqlparser.BitOrStr, sqlparser.BitXorStr, sqlparser.IntDivStr, sqlparser.ModStr:
		if sql.IsUnsigned(a.Left.Type()) && sql.IsUnsigned(a.Right.Type()) {
			return sql.Uint64
		}
		return sql.Int64
	}

	return sql.Float64
}

// TransformUp implements the Expression interface.
func (a *Arithmetic) TransformUp(f sql.TransformExprFunc) (sql.Expression, error) {
	l, err := a.Left.TransformUp(f)
	if err != nil {
		return nil, err
	}

	r, err := a.Right.TransformUp(f)
	if err != nil {
		return nil, err
	}

	return f(NewArithmetic(l, r, a.Op))
}

// Eval implements the Expression interface.
func (a *Arithmetic) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	lval, rval, err := a.evalLeftRight(ctx, row)
	if err != nil {
		return nil, err
	}

	lval, rval, err = a.convertLeftRight(lval, rval)
	if err != nil {
		return nil, err
	}

	switch a.Op {
	case sqlparser.PlusStr:
		return plus(lval, rval)
	case sqlparser.MinusStr:
		return minus(lval, rval)
	case sqlparser.MultStr:
		return mult(lval, rval)
	case sqlparser.DivStr:
		return div(lval, rval)
	case sqlparser.BitAndStr:
		return bitAnd(lval, rval)
	case sqlparser.BitOrStr:
		return bitOr(lval, rval)
	case sqlparser.BitXorStr:
		return bitXor(lval, rval)
	case sqlparser.ShiftLeftStr:
		return shiftLeft(lval, rval)
	case sqlparser.ShiftRightStr:
		return shiftRight(lval, rval)
	case sqlparser.IntDivStr:
		return intDiv(lval, rval)
	case sqlparser.ModStr:
		return mod(lval, rval)
	}

	return nil, errUnableToEval.New(lval, a.Op, rval)
}

func (a *Arithmetic) evalLeftRight(ctx *sql.Context, row sql.Row) (interface{}, interface{}, error) {
	var lval, rval interface{}
	var err error

	if i, ok := a.Left.(*Interval); ok {
		lval, err = i.EvalDelta(ctx, row)
		if err != nil {
			return nil, nil, err
		}
	} else {
		lval, err = a.Left.Eval(ctx, row)
		if err != nil {
			return nil, nil, err
		}
	}

	if i, ok := a.Right.(*Interval); ok {
		rval, err = i.EvalDelta(ctx, row)
		if err != nil {
			return nil, nil, err
		}
	} else {
		rval, err = a.Right.Eval(ctx, row)
		if err != nil {
			return nil, nil, err
		}
	}

	return lval, rval, nil
}

func (a *Arithmetic) convertLeftRight(left interface{}, right interface{}) (interface{}, interface{}, error) {
	var err error
	typ := a.Type()

	if i, ok := left.(*TimeDelta); ok {
		left = i
	} else {
		left, err = typ.Convert(left)
		if err != nil {
			return nil, nil, err
		}
	}

	if i, ok := right.(*TimeDelta); ok {
		right = i
	} else {
		right, err = typ.Convert(right)
		if err != nil {
			return nil, nil, err
		}
	}

	return left, right, nil
}

func plus(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return l + r, nil
		}

	case int64:
		switch r := rval.(type) {
		case int64:
			return l + r, nil
		}

	case float64:
		switch r := rval.(type) {
		case float64:
			return l + r, nil
		}
	case time.Time:
		switch r := rval.(type) {
		case *TimeDelta:
			return r.Add(l), nil
		}
	case *TimeDelta:
		switch r := rval.(type) {
		case time.Time:
			return l.Add(r), nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

func minus(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return l - r, nil
		}

	case int64:
		switch r := rval.(type) {
		case int64:
			return l - r, nil
		}

	case float64:
		switch r := rval.(type) {
		case float64:
			return l - r, nil
		}
	case time.Time:
		switch r := rval.(type) {
		case *TimeDelta:
			return r.Sub(l), nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

func mult(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return l * r, nil
		}

	case int64:
		switch r := rval.(type) {
		case int64:
			return l * r, nil
		}

	case float64:
		switch r := rval.(type) {
		case float64:
			return l * r, nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

func div(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return l / r, nil
		}

	case int64:
		switch r := rval.(type) {
		case int64:
			return l / r, nil
		}

	case float64:
		switch r := rval.(type) {
		case float64:
			return l / r, nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

func bitAnd(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return l & r, nil
		}

	case int64:
		switch r := rval.(type) {
		case int64:
			return l & r, nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

func bitOr(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return l | r, nil
		}

	case int64:
		switch r := rval.(type) {
		case int64:
			return l | r, nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

func bitXor(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return l ^ r, nil
		}

	case int64:
		switch r := rval.(type) {
		case int64:
			return l ^ r, nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

func shiftLeft(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return l << r, nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

func shiftRight(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return l >> r, nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

func intDiv(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return uint64(l / r), nil
		}

	case int64:
		switch r := rval.(type) {
		case int64:
			return int64(l / r), nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

func mod(lval, rval interface{}) (interface{}, error) {
	switch l := lval.(type) {
	case uint64:
		switch r := rval.(type) {
		case uint64:
			return l % r, nil
		}

	case int64:
		switch r := rval.(type) {
		case int64:
			return l % r, nil
		}
	}

	return nil, errUnableToCast.New(lval, rval)
}

// UnaryMinus is an unary minus operator.
type UnaryMinus struct {
	UnaryExpression
}

// NewUnaryMinus creates a new UnaryMinus expression node.
func NewUnaryMinus(child sql.Expression) *UnaryMinus {
	return &UnaryMinus{UnaryExpression{Child: child}}
}

// Eval implements the sql.Expression interface.
func (e *UnaryMinus) Eval(ctx *sql.Context, row sql.Row) (interface{}, error) {
	child, err := e.Child.Eval(ctx, row)
	if err != nil {
		return nil, err
	}

	if child == nil {
		return nil, nil
	}

	if !sql.IsNumber(e.Child.Type()) {
		child, err = sql.Float64.Convert(child)
		if err != nil {
			child = 0.0
		}
	}

	switch n := child.(type) {
	case float64:
		return -n, nil
	case float32:
		return -n, nil
	case int64:
		return -n, nil
	case uint64:
		return -int64(n), nil
	case int32:
		return -n, nil
	case uint32:
		return -int32(n), nil
	default:
		return nil, sql.ErrInvalidType.New(reflect.TypeOf(n))
	}
}

// Type implements the sql.Expression interface.
func (e *UnaryMinus) Type() sql.Type {
	typ := e.Child.Type()
	if !sql.IsNumber(typ) {
		return sql.Float64
	}

	if typ == sql.Uint32 {
		return sql.Int32
	}

	if typ == sql.Uint64 {
		return sql.Int64
	}

	return e.Child.Type()
}

func (e *UnaryMinus) String() string {
	return fmt.Sprintf("-%s", e.Child)
}

// TransformUp implements the sql.Expression interface.
func (e *UnaryMinus) TransformUp(f sql.TransformExprFunc) (sql.Expression, error) {
	c, err := e.Child.TransformUp(f)
	if err != nil {
		return nil, err
	}

	return f(NewUnaryMinus(c))
}
