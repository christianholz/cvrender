package xmlast

import "testing"

func TestParseString_ExtractsFunctionsAndTopLevelNodes(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
<html><body><cv:ex fn="hello"/></body></html>
<cv:func id="hello"><span>Hello</span></cv:func>`

	doc, err := ParseString(src)
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}
	if len(doc.Prolog) != 2 {
		t.Fatalf("expected 2 prolog entries, got %d", len(doc.Prolog))
	}
	if len(doc.Nodes) != 1 {
		t.Fatalf("expected 1 top-level node, got %d", len(doc.Nodes))
	}
	if len(doc.Funcs) != 1 {
		t.Fatalf("expected 1 function, got %d", len(doc.Funcs))
	}
	if _, ok := doc.Funcs["hello"]; !ok {
		t.Fatalf("expected function id 'hello' to be extracted")
	}
}

func TestParseString_MalformedXML(t *testing.T) {
	src := `<html><body><div></body></html>`
	if _, err := ParseString(src); err == nil {
		t.Fatalf("expected parse error for malformed xml")
	}
}
