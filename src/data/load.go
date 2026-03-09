package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadFiles loads and merges one or more YAML/JSON files.
// Later files override earlier files.
func LoadFiles(files []string) (map[string]any, error) {
	merged := map[string]any{}
	for _, f := range files {
		v, err := LoadFile(f)
		if err != nil {
			return nil, err
		}
		mv, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("data file %q must decode to an object at top-level", f)
		}
		merged = DeepMerge(merged, mv)
	}
	return merged, nil
}

// LoadFile loads a YAML or JSON file and returns normalized values.
func LoadFile(filename string) (any, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read data file %q: %w", filename, err)
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".yaml", ".yml":
		var v any
		if err := yaml.Unmarshal(raw, &v); err != nil {
			return nil, fmt.Errorf("parse yaml %q: %w", filename, err)
		}
		return Normalize(v), nil

	case ".json":
		v, err := ParseJSONWithComments(raw)
		if err != nil {
			return nil, fmt.Errorf("parse json %q: %w", filename, err)
		}
		return Normalize(v), nil
	default:
		// Try JSON first, then YAML, so files without extension still work.
		if v, err := ParseJSONWithComments(raw); err == nil {
			return Normalize(v), nil
		}
		var y any
		if err := yaml.Unmarshal(raw, &y); err != nil {
			return nil, fmt.Errorf("unsupported data file extension for %q", filename)
		}
		return Normalize(y), nil
	}
}

// ParseJSONWithComments parses JSON while tolerating // and /* */ comments.
func ParseJSONWithComments(raw []byte) (any, error) {
	clean, err := stripJSONCommentsAndTrailingCommas(raw)
	if err != nil {
		return nil, err
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(clean))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected extra json content after top-level value")
		}
		return nil, fmt.Errorf("invalid trailing json content: %w", err)
	}
	return v, nil
}

// Normalize converts decoder outputs into a consistent map[string]any/list/scalar shape.
func Normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = Normalize(vv)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[fmt.Sprint(k)] = Normalize(vv)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = Normalize(vv)
		}
		return out
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return f
		}
		return t.String()
	default:
		return t
	}
}

// DeepMerge recursively merges maps. Scalar and list values are replaced.
func DeepMerge(dst, src map[string]any) map[string]any {
	out := make(map[string]any, len(dst))
	for k, v := range dst {
		out[k] = v
	}
	for k, v := range src {
		if cur, ok := out[k]; ok {
			cm, cok := cur.(map[string]any)
			vm, vok := v.(map[string]any)
			if cok && vok {
				out[k] = DeepMerge(cm, vm)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func stripJSONCommentsAndTrailingCommas(raw []byte) ([]byte, error) {
	s := string(raw)
	var out strings.Builder
	out.Grow(len(s))

	inString := false
	escaped := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				out.WriteByte(ch)
			}
			continue
		}
		if inBlockComment {
			if ch == '*' && i+1 < len(s) && s[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(s) {
			if s[i+1] == '/' {
				inLineComment = true
				i++
				continue
			}
			if s[i+1] == '*' {
				inBlockComment = true
				i++
				continue
			}
		}

		out.WriteByte(ch)
	}

	if inString || inBlockComment {
		return nil, fmt.Errorf("unterminated json string or block comment")
	}

	clean := out.String()
	clean = removeTrailingCommas(clean)
	return []byte(clean), nil
}

func removeTrailingCommas(s string) string {
	var out strings.Builder
	out.Grow(len(s))

	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r' || s[j] == '\n') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		out.WriteByte(ch)
	}
	return out.String()
}
