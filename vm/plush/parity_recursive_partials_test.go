package plush_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"sync"
	"testing"

	rootplush "github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/hctx"
	"github.com/gobuffalo/plush/v5/helpers/meta"
	"github.com/gobuffalo/plush/v5/templatecache/inmemory"
	vmplush "github.com/gobuffalo/plush/v5/vm/plush"
	"github.com/stretchr/testify/require"
)

type parityRecursiveNode struct {
	Label    string
	Action   string
	Enabled  bool
	Children []parityRecursiveNode
}

func parityRecursiveJSON(value interface{}) template.HTML {
	encoded, _ := json.Marshal(value)
	return template.HTML(encoded)
}

func parityRecursivePartialFeeder(partials map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		partial, ok := partials[name]
		if !ok {
			return "", fmt.Errorf("unexpected partial %q", name)
		}
		return partial, nil
	}
}

func parityRecursiveArrayPartial(next string) string {
	recurse := ""
	if next != "" {
		recurse = `<%= if (len(node.Children) > 0) { %>` +
			fmt.Sprintf(`<%%= partial(%q, {nodes: node.Children}) %%>`, next) +
			`<% } %>`
	}
	return `<%= for (_, node) in nodes { %>` +
		`<%= if (node.Enabled) { %>` +
		`<% let entry = "enter:" + node.Label %>` +
		`<% collected = collected + entry %>` +
		recurse +
		`<% let exit = "exit:" + node.Label %>` +
		`<% collected = collected + exit %>` +
		`<% } %>` +
		`<% } %>`
}

func parityRecursiveContext(nodes interface{}, partials map[string]string) hctx.Context {
	return rootplush.NewContextWith(map[string]interface{}{
		"nodes":         nodes,
		"json_encode":   parityRecursiveJSON,
		"partialFeeder": parityRecursivePartialFeeder(partials),
	})
}

func Test_Parity_Partials_Unique_Filename_Chain_Updates_Main_Array(t *testing.T) {
	partials := map[string]string{
		"level-one.plush.html":   parityRecursiveArrayPartial("level-two.plush.html"),
		"level-two.plush.html":   parityRecursiveArrayPartial("level-three.plush.html"),
		"level-three.plush.html": parityRecursiveArrayPartial("level-four.plush.html"),
		"level-four.plush.html":  parityRecursiveArrayPartial(""),
	}
	nodes := []parityRecursiveNode{
		{
			Label:   "root",
			Enabled: true,
			Children: []parityRecursiveNode{
				{
					Label:   "branch",
					Enabled: true,
					Children: []parityRecursiveNode{
						{
							Label:   "leaf",
							Enabled: true,
							Children: []parityRecursiveNode{
								{Label: "tip", Enabled: true},
							},
						},
					},
				},
				{Label: "sibling", Enabled: true},
			},
		},
	}
	input := `<% let collected = [] %><%= partial("level-one.plush.html", {nodes: nodes}) %><%= json_encode(collected) %>`
	expected := `["enter:root","enter:branch","enter:leaf","enter:tip","exit:tip","exit:leaf","exit:branch","enter:sibling","exit:sibling","exit:root"]`

	comparePlannedRender(t, input, func() hctx.Context {
		return parityRecursiveContext(nodes, partials)
	}, expected)
}

func Test_Parity_Partials_Cached_Recursive_Renders_Isolate_Sequential_Contexts(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	partialName := "cached-tree.plush.html"
	partials := map[string]string{partialName: parityRecursiveArrayPartial(partialName)}
	input := `<% let collected = [] %><%= partial("cached-tree.plush.html", {nodes: nodes}) %><%= json_encode(collected) %>`

	newContext := func(nodes []parityRecursiveNode) hctx.Context {
		ctx := parityRecursiveContext(nodes, partials)
		ctx.Set(meta.TemplateFileKey, "recursive-cache-root.plush.html")
		return ctx
	}

	firstNodes := []parityRecursiveNode{{
		Label:   "first",
		Enabled: true,
		Children: []parityRecursiveNode{
			{Label: "first-child", Enabled: true},
		},
	}}
	firstOut, firstErr := renderVMContext(t, input, newContext(firstNodes))
	require.NoError(t, firstErr)
	require.Equal(t, `["enter:first","enter:first-child","exit:first-child","exit:first"]`, firstOut)

	secondNodes := []parityRecursiveNode{{
		Label:   "second",
		Enabled: true,
		Children: []parityRecursiveNode{
			{Label: "second-child", Enabled: true},
		},
	}}
	secondCtx := newContext(secondNodes)
	secondOut, secondErr := renderVMContext(t, input, secondCtx)
	require.NoError(t, secondErr)
	require.Equal(t, `["enter:second","enter:second-child","exit:second-child","exit:second"]`, secondOut)
	require.NotContains(t, secondOut, "first")

	diagnostics, ok := rootplush.RenderDiagnosticsFromContext(secondCtx)
	require.True(t, ok)
	require.Equal(t, rootplush.VMBytecodeCacheHit, diagnostics.VMBytecodeCache)
}

func Test_Parity_Partials_Cached_Recursive_Renders_Isolate_Concurrent_Contexts(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	partialName := "concurrent-tree.plush.html"
	partials := map[string]string{partialName: parityRecursiveArrayPartial(partialName)}
	input := `<% let collected = [] %><%= partial("concurrent-tree.plush.html", {nodes: nodes}) %><%= json_encode(collected) %>`
	tmpl, err := vmplush.Compile(input)
	require.NoError(t, err)

	previousFallback := rootplush.SetVMGenericFallback(false)
	defer rootplush.SetVMGenericFallback(previousFallback)

	type result struct {
		index       int
		output      string
		err         error
		diagnostics rootplush.RenderDiagnostics
		hasDiag     bool
	}
	const renders = 16
	results := make(chan result, renders)
	var wg sync.WaitGroup
	for index := 0; index < renders; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			label := fmt.Sprintf("render-%02d", index)
			child := label + "-child"
			ctx := parityRecursiveContext([]parityRecursiveNode{{
				Label:   label,
				Enabled: true,
				Children: []parityRecursiveNode{
					{Label: child, Enabled: true},
				},
			}}, partials)
			ctx.Set(meta.TemplateFileKey, "recursive-concurrent-root.plush.html")
			rootplush.EnableRenderPartialFallbackDiagnostics(ctx)
			output, renderErr := tmpl.Render(ctx)
			diagnostics, hasDiag := rootplush.RenderDiagnosticsFromContext(ctx)
			results <- result{index: index, output: output, err: renderErr, diagnostics: diagnostics, hasDiag: hasDiag}
		}(index)
	}
	wg.Wait()
	close(results)

	for renderResult := range results {
		label := fmt.Sprintf("render-%02d", renderResult.index)
		child := label + "-child"
		expected := fmt.Sprintf(`["enter:%s","enter:%s","exit:%s","exit:%s"]`, label, child, child, label)
		require.NoError(t, renderResult.err)
		require.Equal(t, expected, renderResult.output)
		require.True(t, renderResult.hasDiag)
		require.NotEqual(t, rootplush.RenderFastPathInterpreterFallback, renderResult.diagnostics.FastPath)
		require.Zero(t, renderResult.diagnostics.PartialFallbacks.Calls)
	}
}

func Test_Parity_Partials_Shadowed_Accumulator_Is_Isolated(t *testing.T) {
	partialName := "append.plush.html"
	partials := map[string]string{
		partialName: `<% collected = collected + label %><%= json_encode(collected) %>`,
	}
	input := `<% let collected = ["main"] %>` +
		`<%= partial("append.plush.html", {label: "inherited"}) %>|` +
		`<%= partial("append.plush.html", {label: "local", collected: []}) %>|` +
		`<%= json_encode(collected) %>`
	expected := `["main","inherited"]|["local"]|["main","inherited"]`

	comparePlannedRender(t, input, func() hctx.Context {
		return parityRecursiveContext(nil, partials)
	}, expected)
}

func Test_Parity_Partials_Recursive_Update_Survives_Continue_And_Break(t *testing.T) {
	partialName := "control-tree.plush.html"
	partialSource := `<%= for (_, node) in nodes { %>` +
		`<%= if (node.Enabled) { %>` +
		`<% let entry = "enter:" + node.Label %>` +
		`<% collected = collected + entry %>` +
		`<%= if (len(node.Children) > 0) { %>` +
		`<%= partial("control-tree.plush.html", {nodes: node.Children}) %>` +
		`<% } %>` +
		`<%= if (node.Action == "continue") { %><%= continue %><% } %>` +
		`<%= if (node.Action == "break") { %><%= break %><% } %>` +
		`<% let after = "after:" + node.Label %>` +
		`<% collected = collected + after %>` +
		`<% } %>` +
		`<% } %>`
	partials := map[string]string{partialName: partialSource}
	nodes := []parityRecursiveNode{
		{
			Label:   "continue",
			Action:  "continue",
			Enabled: true,
			Children: []parityRecursiveNode{
				{Label: "continue-child", Enabled: true},
			},
		},
		{Label: "normal", Enabled: true},
		{
			Label:   "break",
			Action:  "break",
			Enabled: true,
			Children: []parityRecursiveNode{
				{Label: "break-child", Enabled: true},
			},
		},
		{Label: "after-break", Enabled: true},
	}
	input := `<% let collected = [] %><%= partial("control-tree.plush.html", {nodes: nodes}) %><%= json_encode(collected) %>`
	expected := `["enter:continue","enter:continue-child","after:continue-child","enter:normal","after:normal","enter:break","enter:break-child","after:break-child"]`

	comparePlannedRender(t, input, func() hctx.Context {
		return parityRecursiveContext(nodes, partials)
	}, expected)
}

func Test_Parity_Partials_Recursive_Error_Trace_Recovers_With_Cached_Bytecode(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	partials := map[string]string{
		"error-one.plush.html":   `<%= partial("error-two.plush.html") %>`,
		"error-two.plush.html":   `<%= partial("error-three.plush.html") %>`,
		"error-three.plush.html": `<%= partial("error-four.plush.html") %>`,
		"error-four.plush.html":  `<%= fail_deep() %>`,
	}
	input := `<%= partial("error-one.plush.html") %>`
	newContext := func(fail bool) hctx.Context {
		ctx := rootplush.NewContextWith(map[string]interface{}{
			"partialFeeder": parityRecursivePartialFeeder(partials),
			"fail_deep": func() (string, error) {
				if fail {
					return "", errors.New("deep recursive failure")
				}
				return "recovered", nil
			},
		})
		ctx.Set(meta.TemplateFileKey, "recursive-error-root.plush.html")
		return ctx
	}

	_, interpreterErr := rootplush.Render(input, newContext(true))
	_, vmErr := renderVMContext(t, input, newContext(true))
	require.Error(t, interpreterErr)
	require.Error(t, vmErr)
	for _, fragment := range []string{
		"deep recursive failure",
		"error-one.plush.html",
		"error-two.plush.html",
		"error-three.plush.html",
		"error-four.plush.html",
	} {
		require.Contains(t, interpreterErr.Error(), fragment)
		require.Contains(t, vmErr.Error(), fragment)
	}

	recoveryCtx := newContext(false)
	recovered, recoveryErr := renderVMContext(t, input, recoveryCtx)
	require.NoError(t, recoveryErr)
	require.Equal(t, "recovered", recovered)
	diagnostics, ok := rootplush.RenderDiagnosticsFromContext(recoveryCtx)
	require.True(t, ok)
	require.Equal(t, rootplush.VMBytecodeCacheHit, diagnostics.VMBytecodeCache)
}

func Test_Parity_Partials_Recursive_Indexed_Mutation_Updates_Main_Map(t *testing.T) {
	partialName := "indexed-tree.plush.html"
	partialSource := `<%= for (_, node) in nodes { %>` +
		`<%= if (node.Enabled) { %>` +
		`<% collected[node.Label] = "seen:" + node.Label %>` +
		`<%= if (len(node.Children) > 0) { %>` +
		`<%= partial("indexed-tree.plush.html", {nodes: node.Children}) %>` +
		`<% } %>` +
		`<% } %>` +
		`<% } %>`
	partials := map[string]string{partialName: partialSource}
	nodes := []parityRecursiveNode{{
		Label:   "root",
		Enabled: true,
		Children: []parityRecursiveNode{
			{
				Label:   "branch",
				Enabled: true,
				Children: []parityRecursiveNode{
					{Label: "leaf", Enabled: true},
				},
			},
		},
	}}
	input := `<% let collected = {} %><%= partial("indexed-tree.plush.html", {nodes: nodes}) %><%= json_encode(collected) %>`
	expected := `{"branch":"seen:branch","leaf":"seen:leaf","root":"seen:root"}`

	comparePlannedRender(t, input, func() hctx.Context {
		return parityRecursiveContext(nodes, partials)
	}, expected)
}

func Test_Parity_Partials_Recursive_Update_Syncs_Multiple_Accumulators(t *testing.T) {
	partialName := "multi-tree.plush.html"
	partialSource := `<%= for (_, node) in nodes { %>` +
		`<%= if (node.Enabled) { %>` +
		`<% let entry = "enter:" + node.Label %>` +
		`<% collected = collected + entry %>` +
		`<% count = count + 1 %>` +
		`<% lookup[entry] = count %>` +
		`<%= if (len(node.Children) > 0) { %>` +
		`<%= partial("multi-tree.plush.html", {nodes: node.Children}) %>` +
		`<% } %>` +
		`<% let exit = "exit:" + node.Label %>` +
		`<% collected = collected + exit %>` +
		`<% count = count + 1 %>` +
		`<% lookup[exit] = count %>` +
		`<% } %>` +
		`<% } %>`
	partials := map[string]string{partialName: partialSource}
	nodes := []parityRecursiveNode{{
		Label:   "root",
		Enabled: true,
		Children: []parityRecursiveNode{
			{
				Label:   "branch",
				Enabled: true,
				Children: []parityRecursiveNode{
					{
						Label:   "leaf",
						Enabled: true,
						Children: []parityRecursiveNode{
							{Label: "tip", Enabled: true},
						},
					},
				},
			},
		},
	}}
	input := `<% let collected = [] %><% let count = 0 %><% let lookup = {} %>` +
		`<%= partial("multi-tree.plush.html", {nodes: nodes}) %>` +
		`<%= json_encode(collected) %>|<%= count %>|` +
		`<%= lookup["enter:root"] %>|<%= lookup["enter:tip"] %>|` +
		`<%= lookup["exit:tip"] %>|<%= lookup["exit:root"] %>`
	expected := `["enter:root","enter:branch","enter:leaf","enter:tip","exit:tip","exit:leaf","exit:branch","exit:root"]|8|1|4|5|8`

	comparePlannedRender(t, input, func() hctx.Context {
		return parityRecursiveContext(nodes, partials)
	}, expected)
}

func Test_Parity_Partials_Recursive_Children_Edge_Matrix(t *testing.T) {
	partialName := "children-tree.plush.html"
	partialSource := `<% calls = calls + 1 %>` +
		`<%= for (_, node) in nodes { %>` +
		`<%= if (node.Enabled) { %>` +
		`<% let entry = "enter:" + node.Label %>` +
		`<% collected = collected + entry %>` +
		`<%= if (len(node.Children) > 0) { %>` +
		`<%= partial("children-tree.plush.html", {nodes: node.Children}) %>` +
		`<% } %>` +
		`<% let exit = "exit:" + node.Label %>` +
		`<% collected = collected + exit %>` +
		`<% } %>` +
		`<% } %>`
	mapPartialSource := `<% calls = calls + 1 %>` +
		`<%= for (_, node) in nodes { %>` +
		`<%= if (node["Enabled"]) { %>` +
		`<% let entry = "enter:" + node["Label"] %>` +
		`<% collected = collected + entry %>` +
		`<%= if (len(node["Children"]) > 0) { %>` +
		`<%= partial("children-tree.plush.html", {nodes: node["Children"]}) %>` +
		`<% } %>` +
		`<% let exit = "exit:" + node["Label"] %>` +
		`<% collected = collected + exit %>` +
		`<% } %>` +
		`<% } %>`
	input := `<% let collected = [] %><% let calls = 0 %>` +
		`<%= partial("children-tree.plush.html", {nodes: nodes}) %>` +
		`<%= json_encode(collected) %>|<%= calls %>`

	tests := []struct {
		name     string
		nodes    interface{}
		source   string
		expected string
	}{
		{
			name:     "nil typed children",
			nodes:    []parityRecursiveNode{{Label: "nil", Enabled: true, Children: nil}},
			expected: `["enter:nil","exit:nil"]|1`,
		},
		{
			name:     "empty typed children",
			nodes:    []parityRecursiveNode{{Label: "empty", Enabled: true, Children: []parityRecursiveNode{}}},
			expected: `["enter:empty","exit:empty"]|1`,
		},
		{
			name: "missing map children key",
			nodes: []map[string]interface{}{
				{"Label": "missing", "Enabled": true},
			},
			source:   mapPartialSource,
			expected: `["enter:missing","exit:missing"]|1`,
		},
		{
			name: "disabled child does not recurse into its children",
			nodes: []parityRecursiveNode{{
				Label:   "root",
				Enabled: true,
				Children: []parityRecursiveNode{{
					Label:   "disabled",
					Enabled: false,
					Children: []parityRecursiveNode{
						{Label: "never", Enabled: true},
					},
				}},
			}},
			expected: `["enter:root","exit:root"]|2`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := partialSource
			if tt.source != "" {
				source = tt.source
			}
			partials := map[string]string{partialName: source}
			comparePlannedRender(t, input, func() hctx.Context {
				return parityRecursiveContext(tt.nodes, partials)
			}, tt.expected)
		})
	}
}

func Test_Parity_Partials_Recursive_Update_Escapes_Helper_Block(t *testing.T) {
	partialName := "helper-tree.plush.html"
	partialSource := `<%= for (_, node) in nodes { %>` +
		`<%= if (node.Enabled) { %>` +
		`<% let item = helperLabel + ":" + node.Label %>` +
		`<% collected = collected + item %>` +
		`<%= if (len(node.Children) > 0) { %>` +
		`<%= partial("helper-tree.plush.html", {nodes: node.Children}) %>` +
		`<% } %>` +
		`<% } %>` +
		`<% } %>`
	partials := map[string]string{partialName: partialSource}
	nodes := []parityRecursiveNode{{
		Label:   "root",
		Enabled: true,
		Children: []parityRecursiveNode{
			{Label: "child", Enabled: true},
		},
	}}
	input := `<% let collected = [] %>` +
		`<%= scope() { %>` +
		`<%= partial("helper-tree.plush.html", {nodes: nodes}) %>` +
		`<%= helperLabel %>:<%= json_encode(collected) %>` +
		`<% } %>|<%= json_encode(collected) %>`
	expected := `[scope:["scope:root","scope:child"]]|["scope:root","scope:child"]`

	comparePlannedRender(t, input, contextWith(map[string]interface{}{
		"nodes":         nodes,
		"json_encode":   parityRecursiveJSON,
		"partialFeeder": parityRecursivePartialFeeder(partials),
		"scope": func(help rootplush.HelperContext) (template.HTML, error) {
			help.Set("helperLabel", "scope")
			rendered, err := help.Block()
			return template.HTML("[" + rendered + "]"), err
		},
	}), expected)
}
