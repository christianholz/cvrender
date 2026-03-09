package script

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStarlarkEngine_EvalBasic(t *testing.T) {
	engine := NewStarlarkEngineWithOptions(Options{
		MaxExecutionSteps: 100_000,
		Timeout:           -1,
	})

	got, err := engine.Eval(`d["n"] + 2`, map[string]any{
		"d": map[string]any{"n": int64(3)},
	})
	if err != nil {
		t.Fatalf("Eval returned error: %v", err)
	}
	if got != int64(5) {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestStarlarkEngine_StepLimit(t *testing.T) {
	engine := NewStarlarkEngineWithOptions(Options{
		MaxExecutionSteps: 1,
		Timeout:           -1,
	})

	_, err := engine.Eval(`(lambda f: f(f))(lambda f: f(f))`, nil)
	if err == nil {
		t.Fatalf("expected limit error")
	}
	if !IsKind(err, ErrorKindLimit) {
		t.Fatalf("expected ErrorKindLimit, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "step limit") {
		t.Fatalf("expected step limit message, got: %v", err)
	}
}

func TestStarlarkEngine_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)

	engine := NewStarlarkEngineWithOptions(Options{
		MaxExecutionSteps: 100_000,
		Timeout:           -1,
		Context:           ctx,
	})

	_, err := engine.Eval(`1 + 1`, nil)
	if err == nil {
		t.Fatalf("expected cancellation error")
	}
	if !IsKind(err, ErrorKindLimit) {
		t.Fatalf("expected ErrorKindLimit, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		t.Fatalf("expected canceled message, got: %v", err)
	}
}
