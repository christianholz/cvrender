package xmlast

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const wrapperTag = "__cv_root__"

// ParseFile parses a transformation XML file into an AST document.
func ParseFile(filename string) (*Document, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read template %q: %w", filename, err)
	}
	doc, err := ParseString(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", filename, err)
	}
	return doc, nil
}

// ParseString parses a transformation XML string into an AST document.
func ParseString(content string) (*Document, error) {
	prolog, body, err := splitProlog(content)
	if err != nil {
		return nil, err
	}

	// The source uses "cv:*" tags without namespace declarations.
	body = strings.NewReplacer(
		"<cv:", "<cv_",
		"</cv:", "</cv_",
	).Replace(body)
	body = sanitizeAttributeAngles(body)

	wrapped := "<" + wrapperTag + ">" + body + "</" + wrapperTag + ">"
	dec := xml.NewDecoder(strings.NewReader(wrapped))
	dec.Strict = true
	dec.Entity = map[string]string{
		"nbsp": "\u00A0",
	}

	root := &Node{Kind: KindElement, Name: wrapperTag}
	stack := []*Node{root}

	for {
		tok, tokenErr := dec.Token()
		if tokenErr != nil {
			if errors.Is(tokenErr, io.EOF) {
				break
			}
			line, _ := dec.InputPos()
			return nil, fmt.Errorf("xml token error at line %d: %w", line, tokenErr)
		}
		line, _ := dec.InputPos()

		switch t := tok.(type) {
		case xml.StartElement:
			name := restoreName(t.Name.Local)
			if len(stack) == 1 && name == wrapperTag {
				continue
			}
			n := &Node{
				Kind: KindElement,
				Name: name,
				Line: line,
			}
			for _, a := range t.Attr {
				n.Attrs = append(n.Attrs, Attr{
					Name:  restoreName(a.Name.Local),
					Value: a.Value,
				})
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, n)
			stack = append(stack, n)

		case xml.EndElement:
			name := restoreName(t.Name.Local)
			if len(stack) == 1 && name == wrapperTag {
				continue
			}
			if len(stack) <= 1 {
				return nil, fmt.Errorf("unexpected closing tag %q at line %d", name, line)
			}
			top := stack[len(stack)-1]
			if top.Name != name {
				return nil, fmt.Errorf("mismatched closing tag %q at line %d (expected %q)", name, line, top.Name)
			}
			stack = stack[:len(stack)-1]

		case xml.CharData:
			if len(t) == 0 {
				continue
			}
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, &Node{
				Kind: KindText,
				Text: string(t),
				Line: line,
			})

		case xml.Comment:
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, &Node{
				Kind: KindComment,
				Text: string(t),
				Line: line,
			})

		case xml.Directive:
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, &Node{
				Kind: KindDirective,
				Text: string(t),
				Line: line,
			})

		case xml.ProcInst:
			parent := stack[len(stack)-1]
			parent.Children = append(parent.Children, &Node{
				Kind: KindProcInst,
				Name: t.Target,
				Text: string(t.Inst),
				Line: line,
			})
		}
	}

	if len(stack) != 1 {
		return nil, fmt.Errorf("malformed xml: unbalanced tags")
	}

	funcs := map[string]*Node{}
	nodes := make([]*Node, 0, len(root.Children))
	for _, child := range root.Children {
		if child.Kind == KindText && strings.TrimSpace(child.Text) == "" {
			continue
		}
		if child.Kind == KindElement && child.Name == "cv:func" {
			if id, ok := child.AttrValue("id"); ok && id != "" {
				funcs[id] = child
			}
			continue
		}
		nodes = append(nodes, child)
	}

	return &Document{
		Prolog: prolog,
		Nodes:  nodes,
		Funcs:  funcs,
	}, nil
}

// sanitizeAttributeAngles escapes raw '<' and '>' characters that appear inside
// quoted attribute values. The legacy templates rely on this non-XML-safe form.
func sanitizeAttributeAngles(s string) string {
	var out strings.Builder
	out.Grow(len(s))

	inTag := false
	inComment := false
	quote := byte(0)

	for i := 0; i < len(s); i++ {
		if inComment {
			if i+2 < len(s) && s[i] == '-' && s[i+1] == '-' && s[i+2] == '>' {
				out.WriteString("-->")
				i += 2
				inComment = false
				continue
			}
			out.WriteByte(s[i])
			continue
		}

		if !inTag {
			if i+3 < len(s) && s[i] == '<' && s[i+1] == '!' && s[i+2] == '-' && s[i+3] == '-' {
				inComment = true
				out.WriteString("<!--")
				i += 3
				continue
			}
			if s[i] == '<' {
				inTag = true
			}
			out.WriteByte(s[i])
			continue
		}

		ch := s[i]
		if quote != 0 {
			switch ch {
			case '<':
				out.WriteString("&lt;")
			case '>':
				out.WriteString("&gt;")
			default:
				out.WriteByte(ch)
				if ch == quote {
					quote = 0
				}
			}
			continue
		}

		if ch == '"' || ch == '\'' {
			quote = ch
			out.WriteByte(ch)
			continue
		}
		if ch == '>' {
			inTag = false
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func restoreName(name string) string {
	if strings.HasPrefix(name, "cv_") {
		return "cv:" + strings.TrimPrefix(name, "cv_")
	}
	return name
}

func splitProlog(content string) ([]string, string, error) {
	s := strings.TrimPrefix(content, "\uFEFF")
	out := []string{}

	for {
		trimmed := strings.TrimLeft(s, " \t\r\n")

		if strings.HasPrefix(trimmed, "<?xml") {
			end := strings.Index(trimmed, "?>")
			if end < 0 {
				return nil, "", fmt.Errorf("unterminated xml declaration")
			}
			out = append(out, trimmed[:end+2])
			s = trimmed[end+2:]
			continue
		}

		if strings.HasPrefix(trimmed, "<!DOCTYPE") {
			end, err := findDoctypeEnd(trimmed)
			if err != nil {
				return nil, "", err
			}
			out = append(out, trimmed[:end+1])
			s = trimmed[end+1:]
			continue
		}

		return out, trimmed, nil
	}
}

func findDoctypeEnd(s string) (int, error) {
	inQuote := byte(0)
	bracketDepth := 0
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inQuote != 0 {
			if ch == inQuote {
				inQuote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			inQuote = ch
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '>':
			if bracketDepth == 0 {
				return i, nil
			}
		}
	}
	return -1, fmt.Errorf("unterminated doctype directive")
}
