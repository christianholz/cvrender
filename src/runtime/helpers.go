package runtime

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"cvrender/xmlast"
)

var interpolationRE = regexp.MustCompile(`\{\{([^\{]+?)\}\}`)

func conditionExpr(n *xmlast.Node) (expr string, wantTrue bool, err error) {
	if t, ok := n.AttrValue("t"); ok {
		return t, true, nil
	}
	if f, ok := n.AttrValue("n"); ok {
		return f, false, nil
	}
	return "", false, fmt.Errorf("attribute 't' or 'n' missing")
}

func normalizeLoopItems(v any) ([]any, error) {
	if v == nil {
		return []any{}, nil
	}
	if s, ok := v.(string); ok {
		return []any{s}, nil
	}
	if arr, ok := toSlice(v); ok {
		return arr, nil
	}
	if _, ok := v.(map[string]any); ok {
		return []any{}, nil
	}
	return nil, fmt.Errorf("cv:for expects list/dict/string, got %T", v)
}

func loopItemMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		out := make(map[string]any, len(m)+4)
		for k, vv := range m {
			out[k] = vv
		}
		return out
	}
	return map[string]any{".": v}
}

func toSlice(v any) ([]any, bool) {
	switch t := v.(type) {
	case []any:
		return t, true
	case []string:
		out := make([]any, len(t))
		for i, v := range t {
			out[i] = v
		}
		return out, true
	case []int:
		out := make([]any, len(t))
		for i, v := range t {
			out[i] = v
		}
		return out, true
	case []int64:
		out := make([]any, len(t))
		for i, v := range t {
			out[i] = v
		}
		return out, true
	case []float64:
		out := make([]any, len(t))
		for i, v := range t {
			out[i] = v
		}
		return out, true
	default:
		return nil, false
	}
}

func keyStep(cur any, key string) (any, error) {
	if arr, ok := toSlice(cur); ok {
		out := make([]any, 0, len(arr))
		allLists := true
		for _, el := range arr {
			m, ok := el.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("key mismatch for %q on non-map list item (%T)", key, el)
			}
			v, ok := m[key]
			if !ok {
				return nil, fmt.Errorf("no such key %q", key)
			}
			out = append(out, v)
			if _, isList := toSlice(v); !isList {
				allLists = false
			}
		}
		if allLists {
			flat := []any{}
			for _, v := range out {
				l, _ := toSlice(v)
				flat = append(flat, l...)
			}
			return flat, nil
		}
		return out, nil
	}

	m, ok := cur.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("key mismatch for %q on %T", key, cur)
	}
	v, ok := m[key]
	if !ok {
		return nil, fmt.Errorf("no such key %q", key)
	}
	return v, nil
}

func multiSelectStep(cur any, keySpec string) (any, error) {
	keys := strings.Split(keySpec, ",")
	out := []any{}

	if arr, ok := toSlice(cur); ok {
		for _, k := range keys {
			k = strings.TrimSpace(k)
			for _, el := range arr {
				m, ok := el.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("key mismatch for %q on non-map list item (%T)", k, el)
				}
				v, ok := m[k]
				if !ok {
					return nil, fmt.Errorf("no such key %q", k)
				}
				if l, ok := toSlice(v); ok {
					out = append(out, l...)
				} else {
					out = append(out, v)
				}
			}
		}
		return out, nil
	}

	m, ok := cur.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("key mismatch for %q on %T", keySpec, cur)
	}
	for _, k := range keys {
		k = strings.TrimSpace(k)
		v, ok := m[k]
		if !ok {
			return nil, fmt.Errorf("no such key %q", k)
		}
		if l, ok := toSlice(v); ok {
			out = append(out, l...)
		} else {
			out = append(out, v)
		}
	}
	return out, nil
}

func wildcardStep(cur any, key string) (any, error) {
	if arr, ok := toSlice(cur); ok {
		out := []any{}
		for _, el := range arr {
			m, ok := el.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("wildcard step on non-map item (%T)", el)
			}
			v, ok := m[key]
			if !ok {
				return nil, fmt.Errorf("wildcard missing key %q", key)
			}
			if l, ok := toSlice(v); ok {
				out = append(out, l...)
			} else {
				out = append(out, v)
			}
		}
		return out, nil
	}
	m, ok := cur.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("wildcard key mismatch on %T", cur)
	}
	out := []any{}
	keys := sortedMapKeys(m)
	for _, k := range keys {
		mv, ok := m[k].(map[string]any)
		if !ok {
			continue
		}
		v, ok := mv[key]
		if !ok {
			continue
		}
		if l, ok := toSlice(v); ok {
			out = append(out, l...)
		} else {
			out = append(out, v)
		}
	}
	return out, nil
}

func filterStep(cur any, spec string) (any, error) {
	q := strings.TrimPrefix(spec, "@")
	parts := strings.SplitN(q, "=", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid filter spec %q", spec)
	}
	k := strings.TrimSpace(parts[0])
	v := strings.TrimSpace(parts[1])
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		v = v[1 : len(v)-1]
	}

	var src []any
	if m, ok := cur.(map[string]any); ok {
		src = []any{m}
	} else if arr, ok := toSlice(cur); ok {
		src = arr
	} else {
		return nil, fmt.Errorf("filter step expects list/dict, got %T", cur)
	}

	out := []any{}
	for _, el := range src {
		m, ok := el.(map[string]any)
		if !ok {
			continue
		}
		if toString(m[k]) == v {
			out = append(out, m)
		}
	}
	return out, nil
}

func isIntSegment(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, false
		}
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return i, true
}

func reverseInts(v []int) {
	for i := 0; i < len(v)/2; i++ {
		j := len(v) - 1 - i
		v[i], v[j] = v[j], v[i]
	}
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func compareValues(a, b any) int {
	if af, ok := asFloat(a); ok {
		if bf, ok := asFloat(b); ok {
			switch {
			case af < bf:
				return -1
			case af > bf:
				return 1
			default:
				return 0
			}
		}
	}
	as := toString(a)
	bs := toString(b)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case int32:
		return float64(t), true
	case float32:
		return float64(t), true
	case float64:
		return t, true
	case uint:
		return float64(t), true
	case uint64:
		return float64(t), true
	case uint32:
		return float64(t), true
	default:
		return 0, false
	}
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	default:
		return true
	}
}

func toString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		if float64(int64(t)) == t {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, v := range t {
			parts = append(parts, toString(v))
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprint(v)
	}
}

func exprLiteral(v any) string {
	switch t := v.(type) {
	case nil:
		return "None"
	case string:
		return "'" + strings.ReplaceAll(strings.ReplaceAll(t, `\`, `\\`), `'`, `\'`) + "'"
	case bool:
		if t {
			return "True"
		}
		return "False"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, v := range t {
			parts = append(parts, exprLiteral(v))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := sortedMapKeys(t)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, exprLiteral(k)+": "+exprLiteral(t[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return exprLiteral(fmt.Sprint(v))
	}
}

func escapeAttr(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		`"`, "&quot;",
		"<", "&lt;",
		">", "&gt;",
	)
	return r.Replace(s)
}

func isVoidHTMLTag(name string) bool {
	switch strings.ToLower(name) {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	default:
		return false
	}
}

func joinPathNoClean(prefix, rel string) string {
	if strings.HasPrefix(rel, "/") {
		return rel
	}
	if prefix == "" {
		return rel
	}
	if rel == "" {
		return prefix
	}
	return strings.TrimSuffix(prefix, "/") + "/" + rel
}

func containsDotDot(parts []string) bool {
	for _, p := range parts {
		if p == ".." {
			return true
		}
	}
	return false
}
