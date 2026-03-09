package runtime

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cvrender/data"
	"cvrender/script"
	"cvrender/xmlast"
)

func TestInterpreter_DirectivesAndExpressions(t *testing.T) {
	tests := []struct {
		name     string
		template string
		data     map[string]any
		want     string
	}{
		{
			name:     "value extraction and interpolation",
			template: `<root><cv:val of="name"/> {{name}}</root>`,
			data:     map[string]any{"name": "Ada"},
			want:     `<root>Ada Ada</root>`,
		},
		{
			name:     "ite true branch",
			template: `<root><cv:ite t="flag"><cv:true>T</cv:true><cv:false>F</cv:false></cv:ite></root>`,
			data:     map[string]any{"flag": true},
			want:     `<root>T</root>`,
		},
		{
			name:     "function registration and call",
			template: `<root><cv:ex fn="hello"/></root><cv:func id="hello"><span>Hello</span></cv:func>`,
			data:     map[string]any{},
			want:     `<root><span>Hello</span></root>`,
		},
		{
			name:     "for loop with loop variables",
			template: `<root><cv:for in="items"><cv:val of="{{$index}}"/>:<cv:val of="."/><cv:if t="{{$index}}!={{$last}}">,</cv:if></cv:for></root>`,
			data:     map[string]any{"items": []any{"a", "b", "c"}},
			want:     `<root>1:a,2:b,3:c</root>`,
		},
		{
			name:     "script expression boundary",
			template: `<root><cv:ite t="%'-' in d['pages']"><cv:true>range</cv:true><cv:false>count</cv:false></cv:ite></root>`,
			data:     map[string]any{"pages": "10-20"},
			want:     `<root>range</root>`,
		},
		{
			name:     "attribute escaping",
			template: `<root><a href="{{url}}">x</a></root>`,
			data:     map[string]any{"url": "https://example.test?a=1&b=2"},
			want:     `<root><a href="https://example.test?a=1&amp;b=2">x</a></root>`,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			doc, err := xmlast.ParseString(tc.template)
			if err != nil {
				t.Fatalf("ParseString returned error: %v", err)
			}
			engine := script.NewStarlarkEngine()
			interp := New(engine, ".", false)

			var out bytes.Buffer
			if err := interp.Execute(doc, tc.data, &out); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			got := strings.TrimSpace(out.String())
			if got != tc.want {
				t.Fatalf("unexpected output\nwant: %s\ngot:  %s", tc.want, got)
			}
		})
	}
}

func TestInterpreter_ErrorCases(t *testing.T) {
	engine := script.NewStarlarkEngine()
	interp := New(engine, ".", false)

	t.Run("unresolved function call", func(t *testing.T) {
		doc, err := xmlast.ParseString(`<root><cv:ex fn="missing"/></root>`)
		if err != nil {
			t.Fatalf("ParseString returned error: %v", err)
		}
		var out bytes.Buffer
		err = interp.Execute(doc, map[string]any{}, &out)
		if err == nil {
			t.Fatalf("expected unresolved function error")
		}
		if !strings.Contains(err.Error(), "unresolved cv:func") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("missing for attribute", func(t *testing.T) {
		doc, err := xmlast.ParseString(`<root><cv:for><x/></cv:for></root>`)
		if err != nil {
			t.Fatalf("ParseString returned error: %v", err)
		}
		var out bytes.Buffer
		err = interp.Execute(doc, map[string]any{}, &out)
		if err == nil {
			t.Fatalf("expected cv:for error")
		}
		if !strings.Contains(err.Error(), "missing required attribute 'in'") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestInterpreter_ScriptLimitError(t *testing.T) {
	doc, err := xmlast.ParseString(`<root><cv:if t="% (lambda f: f(f))(lambda f: f(f))">x</cv:if></root>`)
	if err != nil {
		t.Fatalf("ParseString returned error: %v", err)
	}

	engine := script.NewStarlarkEngineWithOptions(script.Options{
		MaxExecutionSteps: 1,
		Timeout:           -1,
	})
	interp := New(engine, ".", false)

	var out bytes.Buffer
	err = interp.Execute(doc, map[string]any{}, &out)
	if err == nil {
		t.Fatalf("expected script limit error")
	}
	if !strings.Contains(err.Error(), string(script.ErrorKindLimit)) {
		t.Fatalf("expected limit error, got: %v", err)
	}
}

func TestInterpreter_RepresentativeFixture(t *testing.T) {
	templatePath := filepath.Join("..", "..", "cv-template.xml")
	dataPath := filepath.Join("..", "..", "cv-data.yaml")
	if _, err := os.Stat(templatePath); err != nil {
		t.Skipf("fixture template missing: %v", err)
	}
	if _, err := os.Stat(dataPath); err != nil {
		t.Skipf("fixture data missing: %v", err)
	}

	root, err := data.LoadFiles([]string{dataPath})
	if err != nil {
		t.Fatalf("LoadFiles returned error: %v", err)
	}
	doc, err := xmlast.ParseFile(templatePath)
	if err != nil {
		t.Fatalf("ParseFile returned error: %v", err)
	}

	engine := script.NewStarlarkEngine()
	interp := New(engine, filepath.Dir(templatePath), false)

	var out bytes.Buffer
	if err := interp.Execute(doc, root, &out); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	got := out.String()

	if strings.Contains(got, "cv:") {
		t.Fatalf("unexpected directive tag leaked into output")
	}
	if !strings.Contains(got, "<h1>Sample Curriculum Vitae</h1>") {
		t.Fatalf("expected rendered title in output")
	}
	if !strings.Contains(got, "Proceedings of") {
		t.Fatalf("expected publication rendering in output")
	}
}

func TestInterpreter_PreserveFormatBoundaryHandling(t *testing.T) {
	tests := []struct {
		name       string
		template   string
		data       map[string]any
		preserve   bool
		wantOutput string
	}{
		{
			name: "if true preserve false compacts whitespace",
			template: `<root>
  <cv:if t="show">
    <span>X</span>
  </cv:if>
</root>`,
			data:       map[string]any{"show": true},
			preserve:   false,
			wantOutput: "<root><span>X</span></root>",
		},
		{
			name: "if true preserve format keeps outer formatting but trims branch boundaries",
			template: `<root>
  <cv:if t="show">
    <span>X</span>
  </cv:if>
</root>`,
			data:       map[string]any{"show": true},
			preserve:   true,
			wantOutput: "<root>\n  <span>X</span>\n</root>",
		},
		{
			name: "ite true branch preserve format",
			template: `<root>
  <cv:ite t="flag">
    <cv:true>
      <span>T</span>
    </cv:true>
    <cv:false>
      <span>F</span>
    </cv:false>
  </cv:ite>
</root>`,
			data:       map[string]any{"flag": true},
			preserve:   true,
			wantOutput: "<root>\n  <span>T</span>\n</root>",
		},
		{
			name: "for loop preserve format keeps stable boundary whitespace",
			template: `<root>
  <cv:for in="items">
    <span><cv:val of="."/></span>
  </cv:for>
</root>`,
			data: map[string]any{
				"items": []any{"a", "b"},
			},
			preserve:   true,
			wantOutput: "<root>\n  <span>a</span><span>b</span>\n</root>",
		},
	}

	engine := script.NewStarlarkEngine()
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			doc, err := xmlast.ParseString(tc.template)
			if err != nil {
				t.Fatalf("ParseString returned error: %v", err)
			}
			var out bytes.Buffer
			if err := New(engine, ".", tc.preserve).Execute(doc, tc.data, &out); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if got := out.String(); got != tc.wantOutput {
				t.Fatalf("unexpected output\nwant: %q\ngot:  %q", tc.wantOutput, got)
			}
		})
	}
}
