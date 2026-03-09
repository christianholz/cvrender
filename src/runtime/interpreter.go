package runtime

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"cvrender/data"
	"cvrender/script"
	"cvrender/xmlast"
)

// Interpreter evaluates a cvrender XML transformation document.
type Interpreter struct {
	script         script.Engine
	baseDir        string
	preserveFormat bool
}

// New creates a new interpreter.
func New(scriptEngine script.Engine, baseDir string, preserveFormat bool) *Interpreter {
	return &Interpreter{
		script:         scriptEngine,
		baseDir:        baseDir,
		preserveFormat: preserveFormat,
	}
}

// Execute renders one document with one root data object.
func (i *Interpreter) Execute(doc *xmlast.Document, root map[string]any, w io.Writer) error {
	bw := bufio.NewWriter(w)

	for _, p := range doc.Prolog {
		if _, err := bw.WriteString(p); err != nil {
			return err
		}
		if !strings.HasSuffix(p, "\n") {
			if err := bw.WriteByte('\n'); err != nil {
				return err
			}
		}
	}

	funcs := map[string]*xmlast.Node{}
	for k, v := range doc.Funcs {
		funcs[k] = v
	}
	ctx := &execCtx{
		current: root,
		full:    root,
		prefix:  "",
	}
	rt := &runtime{
		interp: i,
		out:    bw,
	}

	for _, n := range doc.Nodes {
		if err := rt.renderNode(n, ctx, funcs); err != nil {
			return err
		}
	}
	return bw.Flush()
}

type runtime struct {
	interp *Interpreter
	out    *bufio.Writer
}

type execCtx struct {
	current any
	full    map[string]any
	prefix  string
}

type replaceOptions struct {
	silent      bool
	pad         bool
	sep         string
	evalWhole   bool
	evalPartial bool
}

func (r *runtime) renderNode(n *xmlast.Node, ctx *execCtx, funcs map[string]*xmlast.Node) error {
	switch n.Kind {
	case xmlast.KindText:
		v, err := r.replaceData(n.Text, ctx, replaceOptions{
			evalWhole:   false,
			evalPartial: true,
		})
		if err != nil {
			return wrapNodeErr(n, err)
		}
		s := toString(v)
		if !r.interp.preserveFormat && strings.TrimSpace(s) == "" {
			return nil
		}
		_, err = r.out.WriteString(s)
		return err

	case xmlast.KindComment:
		if !r.interp.preserveFormat {
			return nil
		}
		_, err := r.out.WriteString("<!--" + n.Text + "-->")
		return err

	case xmlast.KindDirective:
		_, err := r.out.WriteString("<!" + n.Text + ">")
		return err

	case xmlast.KindProcInst:
		_, err := r.out.WriteString("<?" + n.Name + " " + n.Text + "?>")
		return err

	case xmlast.KindElement:
		if strings.HasPrefix(strings.ToLower(n.Name), "cv:") {
			if err := r.renderDirective(n, ctx, funcs); err != nil {
				return wrapNodeErr(n, err)
			}
			return nil
		}
		return r.renderElement(n, ctx, funcs)

	default:
		return nil
	}
}

func (r *runtime) renderElement(n *xmlast.Node, ctx *execCtx, funcs map[string]*xmlast.Node) error {
	if _, err := r.out.WriteString("<" + n.Name); err != nil {
		return err
	}
	for _, a := range n.Attrs {
		v, err := r.replaceData(a.Value, ctx, replaceOptions{
			evalWhole:   false,
			evalPartial: true,
		})
		if err != nil {
			return err
		}
		_, err = r.out.WriteString(` ` + a.Name + `="` + escapeAttr(toString(v)) + `"`)
		if err != nil {
			return err
		}
	}
	if len(n.Children) == 0 {
		if isVoidHTMLTag(n.Name) {
			_, err := r.out.WriteString("/>")
			return err
		}
		if _, err := r.out.WriteString("></" + n.Name + ">"); err != nil {
			return err
		}
		return nil
	}
	if _, err := r.out.WriteString(">"); err != nil {
		return err
	}
	for _, c := range n.Children {
		if err := r.renderNode(c, ctx, funcs); err != nil {
			return err
		}
	}
	_, err := r.out.WriteString("</" + n.Name + ">")
	return err
}

func (r *runtime) renderDirective(n *xmlast.Node, ctx *execCtx, funcs map[string]*xmlast.Node) error {
	tag := strings.ToLower(strings.TrimPrefix(n.Name, "cv:"))
	switch tag {
	case "val":
		return r.directiveVal(n, ctx)
	case "if":
		return r.directiveIf(n, ctx, funcs)
	case "ite":
		return r.directiveITE(n, ctx, funcs)
	case "for":
		return r.directiveFor(n, ctx, funcs)
	case "text":
		return r.directiveText(n, ctx)
	case "ex":
		return r.directiveEx(n, ctx, funcs)
	case "set":
		return r.directiveSet(n, ctx)
	case "json":
		return r.directiveJSON(n, ctx)
	case "incl":
		return r.directiveInclude(n, ctx, funcs)
	case "func", "true", "false":
		// Function definitions are extracted at parse time. true/false are consumed by ite.
		return nil
	default:
		return fmt.Errorf("invalid directive <%s>", n.Name)
	}
}

func (r *runtime) directiveVal(n *xmlast.Node, ctx *execCtx) error {
	if of, ok := n.AttrValue("of"); ok {
		nt, err := r.replaceData(of, ctx, replaceOptions{
			pad:       true,
			evalWhole: true,
		})
		if err != nil {
			return err
		}
		if proc, ok := n.AttrValue("proc"); ok {
			out, evalErr := r.interp.script.Eval(strings.ReplaceAll(proc, "{{}}", "nt"), scriptBindings(ctx, map[string]any{
				"nt": nt,
			}))
			if evalErr != nil {
				return fmt.Errorf("cv:val proc eval failed: %w", evalErr)
			}
			nt = out
		}
		s := toString(nt)
		if sf, ok := n.AttrValue("sf"); ok {
			s += sf
		}
		if pf, ok := n.AttrValue("pf"); ok {
			s = pf + s
		}
		_, err = r.out.WriteString(s)
		return err
	}

	if ifExpr, ok := n.AttrValue("if"); ok {
		nt, err := r.replaceData(ifExpr, ctx, replaceOptions{
			silent:    true,
			pad:       true,
			evalWhole: true,
		})
		if err != nil {
			return err
		}

		if truthy(nt) {
			if proc, ok := n.AttrValue("proc"); ok {
				out, evalErr := r.interp.script.Eval(strings.ReplaceAll(proc, "{{}}", "nt"), scriptBindings(ctx, map[string]any{
					"nt": nt,
				}))
				if evalErr != nil {
					return fmt.Errorf("cv:val proc eval failed: %w", evalErr)
				}
				nt = out
			}
			s := toString(nt)
			if sf, ok := n.AttrValue("sf"); ok {
				s += sf
			}
			if pf, ok := n.AttrValue("pf"); ok {
				s = pf + s
			}
			_, err = r.out.WriteString(s)
			return err
		}

		elseExpr, ok := n.AttrValue("else")
		if !ok {
			return nil
		}
		el, err := r.replaceData(elseExpr, ctx, replaceOptions{
			silent:    true,
			pad:       true,
			evalWhole: true,
		})
		if err != nil {
			return err
		}
		if proc, ok := n.AttrValue("proc"); ok {
			out, evalErr := r.interp.script.Eval("el."+proc, scriptBindings(ctx, map[string]any{
				"el": el,
			}))
			if evalErr != nil {
				return fmt.Errorf("cv:val else proc eval failed: %w", evalErr)
			}
			el = out
		}
		s := toString(el)
		if sf, ok := n.AttrValue("sf"); ok {
			s += sf
		}
		if pf, ok := n.AttrValue("pf"); ok {
			s = pf + s
		}
		_, err = r.out.WriteString(s)
		return err
	}

	return nil
}

func (r *runtime) directiveIf(n *xmlast.Node, ctx *execCtx, funcs map[string]*xmlast.Node) error {
	p, wantTrue, err := conditionExpr(n)
	if err != nil {
		return err
	}
	raw, err := r.replaceData(p, ctx, replaceOptions{
		silent:    true,
		pad:       true,
		evalWhole: true,
	})
	if err != nil {
		return err
	}
	cond := truthy(raw) == wantTrue

	if len(n.Children) == 0 {
		if cond {
			if v, ok := n.AttrValue("v"); ok {
				out, err := r.replaceData(v, ctx, replaceOptions{evalWhole: true})
				if err != nil {
					return err
				}
				s := toString(out)
				if strings.TrimSpace(s) != "" {
					_, err := r.out.WriteString(s)
					return err
				}
			}
			return nil
		}
		if ev, ok := n.AttrValue("e"); ok {
			out, err := r.replaceData(ev, ctx, replaceOptions{evalWhole: true})
			if err != nil {
				return err
			}
			s := toString(out)
			if strings.TrimSpace(s) != "" {
				_, err := r.out.WriteString(s)
				return err
			}
		}
		return nil
	}

	if !cond {
		return nil
	}
	return r.renderChildren(n.Children, ctx, funcs, true)
}

func (r *runtime) directiveITE(n *xmlast.Node, ctx *execCtx, funcs map[string]*xmlast.Node) error {
	p, wantTrue, err := conditionExpr(n)
	if err != nil {
		return err
	}
	raw, err := r.replaceData(p, ctx, replaceOptions{
		silent:    true,
		pad:       true,
		evalWhole: true,
	})
	if err != nil {
		return err
	}
	selectTrue := truthy(raw) == wantTrue

	var branch *xmlast.Node
	for _, c := range n.Children {
		if c.Kind != xmlast.KindElement {
			continue
		}
		name := strings.ToLower(c.Name)
		if selectTrue && name == "cv:true" {
			branch = c
			break
		}
		if !selectTrue && name == "cv:false" {
			branch = c
			break
		}
	}
	if branch == nil {
		return nil
	}
	return r.renderChildren(branch.Children, ctx, funcs, true)
}

func (r *runtime) directiveFor(n *xmlast.Node, ctx *execCtx, funcs map[string]*xmlast.Node) error {
	inExpr, ok := n.AttrValue("in")
	if !ok || strings.TrimSpace(inExpr) == "" {
		return fmt.Errorf("cv:for missing required attribute 'in'")
	}

	iset, err := r.resolvePath(inExpr, ctx, false)
	if err != nil {
		return err
	}
	items, err := normalizeLoopItems(iset)
	if err != nil {
		return err
	}

	if rawMap, ok := iset.(map[string]any); ok {
		// Dict loop: convert to grouped items.
		keys := sortedMapKeys(rawMap)
		items = make([]any, 0, len(keys))
		for _, k := range keys {
			items = append(items, map[string]any{
				"$group": k,
				"$items": rawMap[k],
			})
		}
	}

	if groupExpr, ok := n.AttrValue("group"); ok && strings.TrimSpace(groupExpr) != "" {
		grouped, groupErr := r.groupLoopItems(items, groupExpr)
		if groupErr != nil {
			return groupErr
		}
		items = grouped
		sortMode := strings.ToLower(strings.TrimSpace(n.MustAttr("sort")))
		sort.Slice(items, func(i, j int) bool {
			mi, _ := items[i].(map[string]any)
			mj, _ := items[j].(map[string]any)
			ai := toString(mi["$group"])
			aj := toString(mj["$group"])
			if sortMode == "desc" {
				return ai > aj
			}
			return ai < aj
		})
	}

	indices, err := r.computeLoopOrder(n, items, ctx)
	if err != nil {
		return err
	}

	total := len(items)
	for j, idx := range indices {
		if idx < 0 || idx >= len(items) {
			continue
		}
		item := loopItemMap(items[idx])
		item["$first"] = int64(1)
		item["$index"] = int64(j + 1)
		item["$last"] = int64(total)
		item["$path"] = path.Join(ctx.prefix, inExpr, strconv.Itoa(idx))

		childCtx := &execCtx{
			current: item,
			full:    ctx.full,
			prefix:  "",
		}
		if err := r.renderChildren(n.Children, childCtx, funcs, true); err != nil {
			return err
		}
	}
	return nil
}

func (r *runtime) directiveText(n *xmlast.Node, ctx *execCtx) error {
	v, ok := n.AttrValue("v")
	if !ok {
		return nil
	}
	out, err := r.replaceData(v, ctx, replaceOptions{
		evalWhole:   false,
		evalPartial: false,
	})
	if err != nil {
		return err
	}
	s := toString(out)
	if !r.interp.preserveFormat && strings.TrimSpace(s) == "" {
		return nil
	}
	_, err = r.out.WriteString(s)
	return err
}

func (r *runtime) directiveEx(n *xmlast.Node, ctx *execCtx, funcs map[string]*xmlast.Node) error {
	fn, ok := n.AttrValue("fn")
	if !ok || strings.TrimSpace(fn) == "" {
		return fmt.Errorf("cv:ex missing required attribute 'fn'")
	}
	fnode, ok := funcs[fn]
	if !ok {
		return fmt.Errorf("unresolved cv:func %q", fn)
	}
	for _, c := range fnode.Children {
		if err := r.renderNode(c, ctx, funcs); err != nil {
			return err
		}
	}
	return nil
}

func (r *runtime) directiveSet(n *xmlast.Node, ctx *execCtx) error {
	k, ok := n.AttrValue("k")
	if !ok || k == "" {
		return fmt.Errorf("cv:set missing required attribute 'k'")
	}
	vExpr, ok := n.AttrValue("v")
	if !ok {
		return fmt.Errorf("cv:set missing required attribute 'v'")
	}

	cur, ok := ctx.current.(map[string]any)
	if !ok {
		return fmt.Errorf("cv:set current context is not a map")
	}
	v, err := r.replaceData(vExpr, ctx, replaceOptions{
		evalWhole:   false,
		evalPartial: true,
	})
	if err != nil {
		return err
	}
	cur[k] = toString(v)
	return nil
}

func (r *runtime) directiveJSON(n *xmlast.Node, ctx *execCtx) error {
	fnExpr, ok := n.AttrValue("fn")
	if !ok || strings.TrimSpace(fnExpr) == "" {
		return fmt.Errorf("cv:json missing required attribute 'fn'")
	}
	fn, err := r.replaceData(fnExpr, ctx, replaceOptions{
		silent:    true,
		pad:       true,
		evalWhole: true,
	})
	if err != nil {
		return err
	}
	filename := toString(fn)
	if filename == "" {
		return nil
	}
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(r.interp.baseDir, filename)
	}
	v, err := data.LoadFile(filename)
	if err != nil {
		return err
	}
	mv, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("cv:json file %q must decode to an object", filename)
	}
	cur, ok := ctx.current.(map[string]any)
	if !ok {
		return fmt.Errorf("cv:json current context is not a map")
	}
	for k, vv := range mv {
		cur[k] = vv
	}
	return nil
}

func (r *runtime) directiveInclude(n *xmlast.Node, ctx *execCtx, funcs map[string]*xmlast.Node) error {
	fnExpr, ok := n.AttrValue("fn")
	if !ok || strings.TrimSpace(fnExpr) == "" {
		return fmt.Errorf("cv:incl missing required attribute 'fn'")
	}
	fn, err := r.replaceData(fnExpr, ctx, replaceOptions{
		silent:    true,
		pad:       true,
		evalWhole: true,
	})
	if err != nil {
		return err
	}
	filename := toString(fn)
	if filename == "" {
		return nil
	}
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(r.interp.baseDir, filename)
	}
	doc, err := xmlast.ParseFile(filename)
	if err != nil {
		return err
	}
	for id, fnNode := range doc.Funcs {
		funcs[id] = fnNode
	}
	for _, child := range doc.Nodes {
		if err := r.renderNode(child, ctx, funcs); err != nil {
			return err
		}
	}
	return nil
}

func (r *runtime) groupLoopItems(items []any, key string) ([]any, error) {
	type bucket struct {
		group string
		items []any
	}

	order := []string{}
	m := map[string]*bucket{}
	for _, it := range items {
		mv, ok := it.(map[string]any)
		if !ok {
			continue
		}
		v, ok := mv[key]
		if !ok {
			continue
		}
		g := toString(v)
		if _, exists := m[g]; !exists {
			m[g] = &bucket{group: g}
			order = append(order, g)
		}
		m[g].items = append(m[g].items, it)
	}
	out := make([]any, 0, len(order))
	for _, g := range order {
		out = append(out, map[string]any{
			"$group": g,
			"$items": m[g].items,
		})
	}
	return out, nil
}

func (r *runtime) computeLoopOrder(n *xmlast.Node, items []any, ctx *execCtx) ([]int, error) {
	indices := make([]int, len(items))
	for i := range items {
		indices[i] = i
	}

	sortExpr, ok := n.AttrValue("sort")
	if !ok || strings.TrimSpace(sortExpr) == "" {
		return indices, nil
	}
	sortExpr = strings.TrimSpace(sortExpr)
	if sortExpr == "reverse" {
		reverseInts(indices)
		return indices, nil
	}

	parts := strings.Split(sortExpr, ",")
	reverse := false
	if len(parts) > 1 {
		last := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
		if last == "desc" || last == "asc" {
			reverse = last == "desc"
			parts = parts[:len(parts)-1]
		}
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) == 0 {
		return indices, nil
	}

	sort.SliceStable(indices, func(ai, bi int) bool {
		a := items[indices[ai]]
		b := items[indices[bi]]

		for _, key := range parts {
			av := extractSortValue(a, key)
			bv := extractSortValue(b, key)
			cmp := compareValues(av, bv)
			if cmp == 0 {
				continue
			}
			if reverse {
				return cmp > 0
			}
			return cmp < 0
		}
		return indices[ai] < indices[bi]
	})

	return indices, nil
}

func extractSortValue(v any, key string) any {
	if key == "" {
		return nil
	}
	mv, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	if !strings.Contains(key, "/") {
		return mv[key]
	}
	cur := any(mv)
	for _, p := range strings.Split(key, "/") {
		if p == "" {
			continue
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	return cur
}

func (r *runtime) resolvePath(pathExpr string, ctx *execCtx, silent bool) (any, error) {
	if pathExpr == "" {
		if silent {
			return "", nil
		}
		return nil, fmt.Errorf("empty path")
	}
	if strings.HasPrefix(pathExpr, "%") {
		val, err := r.interp.script.Eval(pathExpr[1:], scriptBindings(ctx, nil))
		if err != nil {
			if silent {
				return "", nil
			}
			return nil, err
		}
		return val, nil
	}

	if s, ok := ctx.current.(string); ok && s != "" {
		if silent {
			return "", nil
		}
		return nil, fmt.Errorf("current context is string, cannot resolve path %q", pathExpr)
	}

	if pathExpr == "." {
		if m, ok := ctx.current.(map[string]any); ok {
			if v, ok := m["."]; ok {
				return v, nil
			}
		}
		return ctx.current, nil
	}

	cur := ctx.current
	p := pathExpr
	if strings.HasPrefix(pathExpr, "/") {
		cur = ctx.full
		p = strings.TrimPrefix(pathExpr, "/")
	} else {
		p = joinPathNoClean(ctx.prefix, pathExpr)
	}

	subs := []string{}
	if p != "" {
		subs = strings.Split(p, "/")
		if containsDotDot(subs) {
			reduced := make([]string, 0, len(subs))
			for _, s := range subs {
				if s == ".." {
					if len(reduced) == 0 {
						if silent {
							return "", nil
						}
						return nil, fmt.Errorf("invalid parent traversal in path %q", pathExpr)
					}
					reduced = reduced[:len(reduced)-1]
					continue
				}
				reduced = append(reduced, s)
			}
			subs = reduced
		}
	}

	if len(subs) > 0 && subs[len(subs)-1] == "" {
		if silent {
			return "", nil
		}
		return nil, fmt.Errorf("malformed key path %q", pathExpr)
	}

	for j := 0; j < len(subs); j++ {
		sub := subs[j]
		if j < len(subs)-1 && subs[j+1] == ".." {
			j++
			continue
		}
		if sub == "" {
			continue
		}

		if sub == "*" {
			j++
			if j >= len(subs) {
				if silent {
					return "", nil
				}
				return nil, fmt.Errorf("wildcard requires following key in path %q", pathExpr)
			}
			key := subs[j]
			next, err := wildcardStep(cur, key)
			if err != nil {
				if silent {
					return "", nil
				}
				return nil, err
			}
			cur = next
			continue
		}

		if strings.HasPrefix(sub, "@") {
			next, err := filterStep(cur, sub)
			if err != nil {
				if silent {
					return "", nil
				}
				return nil, err
			}
			cur = next
			continue
		}

		if strings.Contains(sub, ",") {
			next, err := multiSelectStep(cur, sub)
			if err != nil {
				if silent {
					return "", nil
				}
				return nil, err
			}
			cur = next
			continue
		}

		if idx, ok := isIntSegment(sub); ok {
			lst, ok := toSlice(cur)
			if !ok || idx < 0 || idx >= len(lst) {
				if silent {
					return "", nil
				}
				return nil, fmt.Errorf("list index out of range at %q", sub)
			}
			cur = lst[idx]
			continue
		}

		next, err := keyStep(cur, sub)
		if err != nil {
			if silent {
				return "", nil
			}
			return nil, err
		}
		cur = next
	}

	return cur, nil
}

func (r *runtime) replaceData(in any, ctx *execCtx, opt replaceOptions) (any, error) {
	if in == nil {
		return "", nil
	}
	s, ok := in.(string)
	if !ok {
		s = toString(in)
	}

	if opt.pad && !strings.Contains(s, "{{") {
		s = "{{" + s + "}}"
	}

	for {
		loc := interpolationRE.FindStringSubmatchIndex(s)
		if loc == nil {
			break
		}
		sub := s[loc[2]:loc[3]]
		val, err := r.resolvePath(sub, ctx, opt.silent)
		if err != nil {
			return nil, err
		}

		var repl string
		if opt.evalPartial || loc[0] > 0 {
			repl = toString(val)
		} else {
			if arr, ok := toSlice(val); ok {
				parts := make([]string, 0, len(arr))
				for _, item := range arr {
					parts = append(parts, toString(item))
				}
				repl = strings.Join(parts, opt.sep)
			} else {
				repl = exprLiteral(val)
			}
		}
		s = s[:loc[0]] + repl + s[loc[1]:]
	}

	if opt.evalWhole {
		val, err := r.interp.script.Eval(s, scriptBindings(ctx, nil))
		if err != nil {
			if opt.silent && (script.IsKind(err, script.ErrorKindKey) || script.IsKind(err, script.ErrorKindName)) {
				return false, nil
			}
			if script.IsKind(err, script.ErrorKindSyntax) {
				return s, nil
			}
			return nil, err
		}
		return val, nil
	}

	return s, nil
}

func scriptBindings(ctx *execCtx, extra map[string]any) map[string]any {
	m := map[string]any{
		"data":      ctx.current,
		"d":         ctx.current,
		"full_data": ctx.full,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func wrapNodeErr(n *xmlast.Node, err error) error {
	return fmt.Errorf("line %d, node <%s>: %w", n.Line, n.Name, err)
}

func (r *runtime) renderChildren(children []*xmlast.Node, ctx *execCtx, funcs map[string]*xmlast.Node, branchBoundary bool) error {
	for idx, child := range children {
		n := child
		if branchBoundary && r.interp.preserveFormat && child.Kind == xmlast.KindText {
			if idx == 0 && isBoundaryWhitespace(child.Text) {
				continue
			}
			if idx == len(children)-1 && isBoundaryWhitespace(child.Text) {
				continue
			}

			text := child.Text
			if idx == 0 {
				text = trimLeadingIndentLine(text)
			}
			if idx == len(children)-1 {
				text = trimTrailingIndentLine(text)
			}
			if text == "" {
				continue
			}
			if text != child.Text {
				cp := *child
				cp.Text = text
				n = &cp
			}
		}
		if err := r.renderNode(n, ctx, funcs); err != nil {
			return err
		}
	}
	return nil
}

func trimLeadingIndentLine(s string) string {
	i := 0
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\r':
			i++
		case '\n':
			return s[i+1:]
		default:
			return s
		}
	}
	return s
}

func trimTrailingIndentLine(s string) string {
	i := len(s) - 1
	for i >= 0 {
		switch s[i] {
		case ' ', '\t', '\r':
			i--
		case '\n':
			return s[:i]
		default:
			return s
		}
	}
	return s
}

func isBoundaryWhitespace(s string) bool {
	return strings.Contains(s, "\n") && strings.TrimSpace(s) == ""
}
