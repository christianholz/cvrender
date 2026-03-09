package runtime

import "cvrender/script"

// QueryPath resolves one runtime path expression against the root data.
func QueryPath(root map[string]any, expr string, prefix string, scriptEngine script.Engine) (any, error) {
	interp := New(scriptEngine, "", false)
	rt := &runtime{
		interp: interp,
	}
	ctx := &execCtx{
		current: root,
		full:    root,
		prefix:  prefix,
	}
	return rt.resolvePath(expr, ctx, false)
}
