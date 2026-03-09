package runtime

import (
	"bytes"
	"strings"
	"testing"

	"cvrender/script"
	"cvrender/xmlast"
)

func TestQueryPath(t *testing.T) {
	root := map[string]any{
		"publications": map[string]any{
			"journals": []any{
				map[string]any{"title": "J1"},
			},
			"papers": []any{
				map[string]any{"title": "P1"},
			},
			"notes": []any{
				map[string]any{"title": "N1"},
			},
		},
	}

	v, err := QueryPath(root, "publications/journals,papers,notes/0/title", "", script.NewStarlarkEngine())
	if err != nil {
		t.Fatalf("QueryPath returned error: %v", err)
	}
	if got := toString(v); got != "J1" {
		t.Fatalf("unexpected query result: %v", v)
	}
}

func TestPreserveFormatFlag(t *testing.T) {
	doc, err := xmlast.ParseString("<root>\n  \n</root>")
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}

	engine := script.NewStarlarkEngine()

	var compact bytes.Buffer
	if err := New(engine, ".", false).Execute(doc, map[string]any{}, &compact); err != nil {
		t.Fatalf("Execute compact returned error: %v", err)
	}
	if strings.Contains(compact.String(), "\n") {
		t.Fatalf("compact output unexpectedly preserved whitespace: %q", compact.String())
	}

	var preserved bytes.Buffer
	if err := New(engine, ".", true).Execute(doc, map[string]any{}, &preserved); err != nil {
		t.Fatalf("Execute preserve returned error: %v", err)
	}
	if !strings.Contains(preserved.String(), "\n") {
		t.Fatalf("preserve output did not keep whitespace: %q", preserved.String())
	}
}
