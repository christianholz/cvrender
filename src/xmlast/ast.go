package xmlast

// Kind identifies the XML AST node kind.
type Kind int

const (
	KindElement Kind = iota
	KindText
	KindComment
	KindDirective
	KindProcInst
)

// Attr is an XML attribute.
type Attr struct {
	Name  string
	Value string
}

// Node is a parsed XML AST node.
type Node struct {
	Kind     Kind
	Name     string
	Attrs    []Attr
	Children []*Node
	Text     string
	Line     int
}

// AttrValue looks up an attribute by name.
func (n *Node) AttrValue(name string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name == name {
			return a.Value, true
		}
	}
	return "", false
}

// MustAttr returns an attribute value or an empty string when absent.
func (n *Node) MustAttr(name string) string {
	v, _ := n.AttrValue(name)
	return v
}

// Document is a parsed transformation document.
type Document struct {
	Prolog []string
	Nodes  []*Node
	Funcs  map[string]*Node
}
