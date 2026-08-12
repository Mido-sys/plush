package vm

import (
	"fmt"
	"html/template"
	"strings"
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/stretchr/testify/require"
)

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
