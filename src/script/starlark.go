package script

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

func init() {
	// Preserve dynamic behavior expected by template expressions.
	resolve.AllowSet = true
	resolve.AllowLambda = true
	resolve.AllowRecursion = true
	resolve.AllowGlobalReassign = true
}

// ErrorKind categorizes script evaluation failures.
type ErrorKind string

const (
	ErrorKindSyntax ErrorKind = "SyntaxError"
	ErrorKindName   ErrorKind = "NameError"
	ErrorKindKey    ErrorKind = "KeyError"
	ErrorKindType   ErrorKind = "TypeError"
	ErrorKindLimit  ErrorKind = "LimitError"
	ErrorKindOther  ErrorKind = "ScriptError"
)

const (
	DefaultMaxExecutionSteps = uint64(5_000_000)
	DefaultTimeout           = 2 * time.Second
)

// EvalError reports expression evaluation errors.
type EvalError struct {
	Kind    ErrorKind
	Message string
	Expr    string
}

func (e *EvalError) Error() string {
	return fmt.Sprintf("%s: %s (expr: %s)", e.Kind, e.Message, e.Expr)
}

// Engine evaluates embedded script expressions.
type Engine interface {
	Eval(expr string, bindings map[string]any) (any, error)
}

// Options configures execution guardrails.
type Options struct {
	MaxExecutionSteps uint64
	Timeout           time.Duration
	Context           context.Context
}

// StarlarkEngine evaluates expressions in pure Go via starlark-go.
type StarlarkEngine struct {
	maxExecutionSteps uint64
	timeout           time.Duration
	ctx               context.Context
}

// NewStarlarkEngine creates a Go-only embedded scripting engine.
func NewStarlarkEngine() *StarlarkEngine {
	return NewStarlarkEngineWithOptions(Options{})
}

// NewStarlarkEngineWithOptions creates a script engine with guardrails.
func NewStarlarkEngineWithOptions(opts Options) *StarlarkEngine {
	maxSteps := opts.MaxExecutionSteps
	if maxSteps == 0 {
		maxSteps = DefaultMaxExecutionSteps
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	return &StarlarkEngine{
		maxExecutionSteps: maxSteps,
		timeout:           timeout,
		ctx:               opts.Context,
	}
}

// Eval evaluates one expression using current bindings.
func (e *StarlarkEngine) Eval(expr string, bindings map[string]any) (any, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return "", nil
	}

	thread := &starlark.Thread{Name: "cvrender-script"}
	stopGuards := e.installGuards(thread)
	defer stopGuards()

	predeclared := starlark.StringDict{}
	for k, v := range bindings {
		sv, err := toStarlark(v)
		if err != nil {
			return nil, &EvalError{
				Kind:    ErrorKindType,
				Message: fmt.Sprintf("cannot bind %q: %v", k, err),
				Expr:    expr,
			}
		}
		predeclared[k] = sv
	}

	val, err := starlark.Eval(thread, "<expr>", expr, predeclared)
	if err != nil {
		return nil, &EvalError{
			Kind:    classifyErr(err),
			Message: err.Error(),
			Expr:    expr,
		}
	}
	out, convErr := fromStarlark(val)
	if convErr != nil {
		return nil, &EvalError{
			Kind:    ErrorKindType,
			Message: convErr.Error(),
			Expr:    expr,
		}
	}
	return out, nil
}

func (e *StarlarkEngine) installGuards(thread *starlark.Thread) func() {
	done := make(chan struct{})

	if e.maxExecutionSteps > 0 {
		thread.SetMaxExecutionSteps(e.maxExecutionSteps)
		thread.OnMaxSteps = func(thread *starlark.Thread) {
			thread.Cancel(fmt.Sprintf("execution step limit exceeded (%d steps)", e.maxExecutionSteps))
		}
	}

	var timer *time.Timer
	if e.timeout > 0 {
		timeoutReason := fmt.Sprintf("execution timeout after %s", e.timeout)
		timer = time.AfterFunc(e.timeout, func() {
			thread.Cancel(timeoutReason)
		})
	}

	if e.ctx != nil {
		if err := e.ctx.Err(); err != nil {
			thread.Cancel("execution canceled: " + err.Error())
		} else {
			go func() {
				select {
				case <-done:
					return
				case <-e.ctx.Done():
					thread.Cancel("execution canceled: " + e.ctx.Err().Error())
				}
			}()
		}
	}

	return func() {
		close(done)
		if timer != nil {
			timer.Stop()
		}
	}
}

func IsKind(err error, kind ErrorKind) bool {
	var ee *EvalError
	if !errors.As(err, &ee) {
		return false
	}
	return ee.Kind == kind
}

func classifyErr(err error) ErrorKind {
	switch err.(type) {
	case *syntax.Error:
		return ErrorKindSyntax
	case syntax.Error:
		return ErrorKindSyntax
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "starlark computation cancelled"):
		return ErrorKindLimit
	case strings.Contains(msg, "undefined"):
		return ErrorKindName
	case strings.Contains(msg, "key ") && strings.Contains(msg, " not in dict"):
		return ErrorKindKey
	case strings.Contains(msg, "index ") && strings.Contains(msg, " out of range"):
		return ErrorKindKey
	case strings.Contains(msg, "got ") || strings.Contains(msg, "cannot") || strings.Contains(msg, "invalid"):
		return ErrorKindType
	default:
		return ErrorKindOther
	}
}

func toStarlark(v any) (starlark.Value, error) {
	switch t := v.(type) {
	case nil:
		return starlark.None, nil
	case starlark.Value:
		return t, nil
	case bool:
		return starlark.Bool(t), nil
	case string:
		return starlark.String(t), nil
	case int:
		return starlark.MakeInt(t), nil
	case int64:
		return starlark.MakeInt64(t), nil
	case int32:
		return starlark.MakeInt64(int64(t)), nil
	case uint:
		return starlark.MakeUint64(uint64(t)), nil
	case uint64:
		return starlark.MakeUint64(t), nil
	case float64:
		return starlark.Float(t), nil
	case float32:
		return starlark.Float(float64(t)), nil
	case []any:
		out := make([]starlark.Value, len(t))
		for i, vv := range t {
			cv, err := toStarlark(vv)
			if err != nil {
				return nil, err
			}
			out[i] = cv
		}
		return starlark.NewList(out), nil
	case map[string]any:
		d := starlark.NewDict(len(t))
		for k, vv := range t {
			cv, err := toStarlark(vv)
			if err != nil {
				return nil, err
			}
			if err := d.SetKey(starlark.String(k), cv); err != nil {
				return nil, err
			}
		}
		return d, nil
	default:
		return starlark.String(fmt.Sprint(v)), nil
	}
}

func fromStarlark(v starlark.Value) (any, error) {
	switch t := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(t), nil
	case starlark.String:
		return t.GoString(), nil
	case starlark.Int:
		if i, ok := t.Int64(); ok {
			return i, nil
		}
		return t.String(), nil
	case starlark.Float:
		f := float64(t)
		if math.Trunc(f) == f && f >= math.MinInt64 && f <= math.MaxInt64 {
			return int64(f), nil
		}
		return f, nil
	case *starlark.List:
		out := make([]any, t.Len())
		for i := 0; i < t.Len(); i++ {
			cv, err := fromStarlark(t.Index(i))
			if err != nil {
				return nil, err
			}
			out[i] = cv
		}
		return out, nil
	case starlark.Tuple:
		out := make([]any, len(t))
		for i := 0; i < len(t); i++ {
			cv, err := fromStarlark(t[i])
			if err != nil {
				return nil, err
			}
			out[i] = cv
		}
		return out, nil
	case *starlark.Dict:
		out := make(map[string]any, t.Len())
		for _, it := range t.Items() {
			k := it[0]
			vv := it[1]
			cv, err := fromStarlark(vv)
			if err != nil {
				return nil, err
			}
			out[toComparableString(k)] = cv
		}
		return out, nil
	default:
		return t.String(), nil
	}
}

func toComparableString(v starlark.Value) string {
	switch k := v.(type) {
	case starlark.String:
		return k.GoString()
	default:
		return k.String()
	}
}
