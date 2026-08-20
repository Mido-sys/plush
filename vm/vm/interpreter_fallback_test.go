package vm

import (
	"fmt"
	"html/template"
	"strings"
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/hctx"
	"github.com/gobuffalo/plush/v5/templatecache/inmemory"
	"github.com/gobuffalo/tags/v3"
	"github.com/stretchr/testify/require"
)

type partialFormDocument struct {
	Rows []partialFormRow
}

type partialFormRow struct {
	ID      string
	Icon    string
	Name    string
	Tags    []string
	Rank    int
	Mode    string
	Enabled bool
}

const partialFormDocumentSource = `<%= if (document) { %>
<%= for (line, row) in document.Rows { %>
<%= if (row.Icon) { %><%= iconTag(row.Icon) %><% } else { %>no icon<% } %>
<a href="<%= rowPath({id: row.ID}) %>"><%= row.Name %></a>
<%= if (row.Tags) { %><%= for (line, tag) in row.Tags { %><%= tag %><% } %><% } %>
<%= if (row.Enabled) { %>
<%= form({action: documentPath(), method: "POST", class: "document"}) { %>
<%= f.InputTag({name:"Rows["+line+"].Rank", value:row.Rank, type:"hidden"}) %>
<%= f.InputTag({name:"Rows["+line+"].ID", value:row.ID, type:"hidden"}) %>
<%= f.InputTag({name:"Rows["+line+"].Mode", value:row.Mode, type:"hidden"}) %>
<% } %>
<% } %>
<% } %>
<% } else { %>empty<% } %>`

func newPartialFormDocumentContext(partialName, source, formName string) *plush.Context {
	values := map[string]interface{}{
		"document": &partialFormDocument{Rows: []partialFormRow{
			{ID: "first", Icon: "/first.png", Name: "First", Tags: []string{"Alpha", "Beta"}, Rank: 1, Mode: "first-mode", Enabled: true},
			{ID: "second", Name: "Second", Tags: []string{"Gamma"}, Rank: 2, Mode: "second-mode", Enabled: true},
		}},
		"documentPath": func() string { return "/document" },
		"rowPath": func(data map[string]interface{}) string {
			return "/rows/" + fmt.Sprint(data["id"])
		},
		"iconTag": func(source string) template.HTML {
			return template.HTML(`<img src="` + source + `">`)
		},
		"partialFeeder": func(name string) (string, error) {
			if name != partialName {
				return "", fmt.Errorf("unexpected partial %q", name)
			}
			return source, nil
		},
	}
	values[formName] = func(options tags.Options, help hctx.HelperContext) (template.HTML, error) {
		help.Set("f", fastScriptPlanTagFormBuilder{})
		body, err := help.Block()
		if err != nil {
			return "", err
		}
		return template.HTML(`<form action="` + fmt.Sprint(options["action"]) + `" method="POST">` + body + `</form>`), nil
	}
	return plush.NewContextWith(values)
}

func Test_Render_Interpreter_Fallback_With_Partial_Overlay_Context(t *testing.T) {
	parent := plush.NewContext()
	parent.Set("partial", vmPartialHelper)

	ctx := newPartialOverlayContext(parent)
	ctx.Set("show", true)
	require.True(t, ctx.Has("show"))
	require.Equal(t, true, ctx.Value("show"))

	out, err := renderInterpreterFallback(`<%= if (show) { %>Mido<% } %>`, ctx, "partial.plush.html")
	require.NoError(t, err)
	require.Equal(t, "Mido", out)
}

func Test_VM_Partial_Helper_Delegates_During_Interpreter_Render(t *testing.T) {
	ctx := plush.NewContext()
	ctx.Set("partial", vmPartialHelper)
	ctx.Set("partialFeeder", func(name string) (string, error) {
		require.Equal(t, "row.plush", name)
		return `<% let title = "Row" %><%= title %>`, nil
	})
	plush.SetRenderDiagnostics(ctx, plush.RenderDiagnostics{})

	out, err := plush.RenderInterpreter(`<%= partial("row.plush", {}) %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "Row", out)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Zero(t, diagnostics.VMHotspots.PartialCalls)
	require.Zero(t, diagnostics.PartialFallbacks.Calls)
}

func Test_VM_Partial_Helper_Interpreter_Render_With_Overlay_If(t *testing.T) {
	parent := plush.NewContext()
	parent.Set("partial", vmPartialHelper)
	parent.Set("partialFeeder", func(name string) (string, error) {
		require.Equal(t, "row.plush", name)
		return `<%= if (show) { %><%= name %><% } else { %>Hidden<% } %>`, nil
	})

	ctx := newPartialOverlayContext(parent)
	ctx.Set("show", true)
	ctx.Set("name", "Mido")

	out, err := plush.RenderInterpreter(`<%= partial("row.plush", {}) %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "Mido", out)
}

func Test_VM_Partial_Fallback_Diagnostics_Classify_Block_And_Inherited_Partials(t *testing.T) {
	previousFallback := plush.SetVMGenericFallback(true)
	defer plush.SetVMGenericFallback(previousFallback)

	ctx := plush.NewContextWith(map[string]interface{}{
		"wrap": func(_ map[string]interface{}, help plush.HelperContext) (template.HTML, error) {
			body, err := help.Block()
			return template.HTML(body), err
		},
		"partialFeeder": func(name string) (string, error) {
			switch name {
			case "outer.plush":
				return `<%= wrap({}) { %>outer:<%= partial("inner.plush") %><% } %>`, nil
			case "inner.plush":
				return "inner", nil
			default:
				return "", fmt.Errorf("unexpected partial %q", name)
			}
		},
	})
	plush.EnableRenderPartialFallbackDiagnostics(ctx)

	out, err := Render(`<%= partial("outer.plush") %><%= partial("outer.plush") %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "outer:innerouter:inner", out)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, 4, diagnostics.PartialFallbacks.Calls)
	require.Zero(t, diagnostics.PartialFallbacks.DetailsDropped)
	require.Len(t, diagnostics.PartialFallbacks.Details, 2)
	requirePartialFallbackDetail(t, diagnostics.PartialFallbacks.Details, "outer.plush", plush.RenderPartialFallbackBlockCallCompatibility, 2)
	requirePartialFallbackDetail(t, diagnostics.PartialFallbacks.Details, "inner.plush", plush.RenderPartialFallbackInheritedInterpreter, 2)
}

func Test_VM_Form_Partial_With_Nested_Loops_Does_Not_Use_Compatibility_Fallback(t *testing.T) {
	previousFallback := plush.SetVMGenericFallback(true)
	defer plush.SetVMGenericFallback(previousFallback)
	cache := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(cache)
	defer func() {
		plush.ClearTemplateCache()
		plush.PlushCacheSetup(nil)
	}()

	interpreter, err := plush.RenderInterpreter(partialFormDocumentSource, newPartialFormDocumentContext("document-form.plush", partialFormDocumentSource, "form"))
	require.NoError(t, err)

	tmpl, err := Compile(`<%= partial("document-form.plush") %>`)
	require.NoError(t, err)
	feederCalls := 0
	newContext := func() *plush.Context {
		ctx := newPartialFormDocumentContext("document-form.plush", partialFormDocumentSource, "form")
		ctx.Set("partialFeeder", func(name string) (string, error) {
			feederCalls++
			if name != "document-form.plush" {
				return "", fmt.Errorf("unexpected partial %q", name)
			}
			return partialFormDocumentSource, nil
		})
		plush.EnableTrustedPartialBytecodeCache(ctx)
		plush.EnableRenderPartialFallbackDiagnostics(ctx)
		return ctx
	}

	firstOutput, err := tmpl.Render(newContext())
	require.NoError(t, err)
	require.Equal(t, interpreter, firstOutput)

	ctx := newContext()
	vmOutput, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, interpreter, vmOutput)
	require.Contains(t, vmOutput, `name="Rows[0].Rank"`)
	require.Contains(t, vmOutput, `name="Rows[1].Rank"`)
	require.Equal(t, 1, feederCalls, "warm trusted form partial should not reload its source")

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Zero(t, diagnostics.PartialFallbacks.Calls)
}

func Test_VM_Form_Partial_Error_Uses_Inner_Block_Line(t *testing.T) {
	const source = `<%= for (_, row) in rows { %>
<%= if (row.Enabled) { %>
<%= form({}) { %>
<%= missing(row.Name) %>
<% } %>
<% } %>
<% } %>`
	type row struct {
		Name    string
		Enabled bool
	}
	ctx := plush.NewContextWith(map[string]interface{}{
		"rows": []row{{Name: "First", Enabled: true}},
		"form": func(_ tags.Options, help hctx.HelperContext) (template.HTML, error) {
			body, err := help.Block()
			return template.HTML(body), err
		},
		"partialFeeder": func(name string) (string, error) {
			if name != "document-error.plush" {
				return "", fmt.Errorf("unexpected partial %q", name)
			}
			return source, nil
		},
	})
	tmpl, err := Compile(`<%= partial("document-error.plush") %>`)
	require.NoError(t, err)
	_, err = tmpl.Render(ctx)
	require.Error(t, err)

	var trace *plush.TemplateTraceError
	require.ErrorAs(t, err, &trace)
	require.NotEmpty(t, trace.Frames)
	require.Equal(t, 4, trace.Frames[len(trace.Frames)-1].Line)
	require.Contains(t, err.Error(), `document-error.plush:4: could not call form function`)
}

func Test_VM_Form_Partial_With_Generic_Block_Retains_Compatibility_Fallback(t *testing.T) {
	tmpl, err := Compile(`<%= form({}) { %><%= name %><% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan)
	require.Len(t, tmpl.bytecode.FastRenderPlan.Segments, 1)
	block := tmpl.bytecode.FastRenderPlan.Segments[0].BlockCall
	require.NotNil(t, block)
	require.NotNil(t, block.BlockBytecode)
	block.BlockBytecode.FastRenderPlan = nil

	previousFallback := plush.SetVMGenericFallback(true)
	defer plush.SetVMGenericFallback(previousFallback)
	require.True(t, shouldFallbackPartialBytecode(tmpl.bytecode))
	require.Equal(t, plush.RenderPartialFallbackBlockCallCompatibility, partialFallbackReason(tmpl.bytecode))
}

func Test_VM_Partial_Fallback_Diagnostics_Classify_Generic_Bytecode(t *testing.T) {
	tmpl, err := Compile(`<%= name %>`)
	require.NoError(t, err)
	tmpl.bytecode.FastRenderPlan = nil

	previousFallback := plush.SetVMGenericFallback(true)
	defer plush.SetVMGenericFallback(previousFallback)
	disabledCtx := plush.NewContextWith(map[string]interface{}{"name": "Mido"})
	disabledOut, err := renderInterpreterPartial(`<%= name %>`, disabledCtx, "disabled.plush", tmpl.bytecode)
	require.NoError(t, err)
	require.Equal(t, "Mido", disabledOut)
	disabledDiagnostics, ok := plush.RenderDiagnosticsFromContext(disabledCtx)
	require.True(t, ok)
	require.Zero(t, disabledDiagnostics.PartialFallbacks.Calls)

	ctx := plush.NewContextWith(map[string]interface{}{"name": "Mido"})
	plush.EnableRenderPartialFallbackDiagnostics(ctx)

	out, err := renderInterpreterPartial(`<%= name %>`, ctx, "legacy.plush", tmpl.bytecode)
	require.NoError(t, err)
	require.Equal(t, "Mido", out)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, 1, diagnostics.PartialFallbacks.Calls)
	requirePartialFallbackDetail(t, diagnostics.PartialFallbacks.Details, "legacy.plush", plush.RenderPartialFallbackGenericBytecode, 1)
}

func Test_VM_Partial_Fallback_Diagnostics_Bound_Dynamic_Names(t *testing.T) {
	ctx := plush.NewContext()
	plush.EnableRenderPartialFallbackDiagnostics(ctx)
	for index := 0; index < 20; index++ {
		plush.AddRenderDiagnosticPartialFallback(ctx, fmt.Sprintf("partial-%d.plush", index), plush.RenderPartialFallbackGenericBytecode)
	}
	plush.AddRenderDiagnosticPartialFallback(ctx, "partial-0.plush", plush.RenderPartialFallbackGenericBytecode)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, 21, diagnostics.PartialFallbacks.Calls)
	require.Len(t, diagnostics.PartialFallbacks.Details, 16)
	require.Equal(t, 4, diagnostics.PartialFallbacks.DetailsDropped)
	requirePartialFallbackDetail(t, diagnostics.PartialFallbacks.Details, "partial-0.plush", plush.RenderPartialFallbackGenericBytecode, 2)
}

func requirePartialFallbackDetail(t *testing.T, details []plush.RenderPartialFallbackDetail, nameSuffix string, reason plush.RenderPartialFallbackReason, calls int) {
	t.Helper()
	for _, detail := range details {
		if strings.HasSuffix(strings.ReplaceAll(detail.Name, "\\", "/"), nameSuffix) && detail.Reason == reason {
			require.Equal(t, calls, detail.Calls)
			return
		}
	}
	require.Failf(t, "missing fallback detail", "name suffix %q reason %q in %#v", nameSuffix, reason, details)
}
