package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseJSONWithComments(t *testing.T) {
	raw := []byte(`{
  // line comment
  "a": 1,
  "b": [1, 2,],
  /* block comment */
  "c": {"x": true,},
}`)

	v, err := ParseJSONWithComments(raw)
	if err != nil {
		t.Fatalf("ParseJSONWithComments returned error: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected object, got %T", v)
	}
	if got := Normalize(m["a"]); got != int64(1) {
		t.Fatalf("expected a=1, got %#v", m["a"])
	}
}

func TestParseJSONWithComments_RejectsTrailingContent(t *testing.T) {
	_, err := ParseJSONWithComments([]byte(`{"a":1} trailing`))
	if err == nil {
		t.Fatalf("expected trailing content error")
	}
}

func TestLoadFiles_MergeAndNormalize(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.yaml")
	f2 := filepath.Join(dir, "b.json")

	if err := os.WriteFile(f1, []byte(`
root:
  x: 1
  nested:
    a: one
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte(`{"root":{"y":2,"nested":{"b":"two"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	merged, err := LoadFiles([]string{f1, f2})
	if err != nil {
		t.Fatalf("LoadFiles returned error: %v", err)
	}
	root, ok := merged["root"].(map[string]any)
	if !ok {
		t.Fatalf("expected root object, got %T", merged["root"])
	}
	if root["x"] != 1 {
		t.Fatalf("expected root.x=1, got %#v", root["x"])
	}
	if root["y"] != int64(2) {
		t.Fatalf("expected root.y=2, got %#v", root["y"])
	}
	nested, ok := root["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested object, got %T", root["nested"])
	}
	if nested["a"] != "one" || nested["b"] != "two" {
		t.Fatalf("unexpected nested content: %#v", nested)
	}
}
