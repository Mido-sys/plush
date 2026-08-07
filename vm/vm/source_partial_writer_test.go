package vm

import (
	"errors"
	"html/template"
	"strconv"
	"strings"
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/meta"
	"github.com/gobuffalo/plush/v5/vm/compiler"
	"github.com/stretchr/testify/require"
)

func Test_VM_Fast_Writer_Write_Source_Partial_Inherits_Context_And_Scopes_Data(t *testing.T) {
	ctx := plush.NewContextWith(map[string]interface{}{
		"inherited": "parent",
		"local":     "outer",
		"nullable":  "outer",
	})
	var out strings.Builder
	w := FastWriter{out: &out, ctx: ctx}

	err := w.WriteSourcePartial(
		"runtime/context.plush",
		`<%= inherited %>|<%= local %>|<%= nullable %>`,
		map[string]interface{}{"local": "inner", "nullable": nil},
	)
	require.NoError(t, err)
	require.Equal(t, "parent|inner|", out.String())
	require.Equal(t, "outer", ctx.Value("local"))
	require.Equal(t, "outer", ctx.Value("nullable"))
}

func Test_VM_Fast_Writer_Write_Source_Partial_Replaces_Changed_Source(t *testing.T) {
	ctx := plush.NewContext()
	var out strings.Builder
	w := FastWriter{out: &out, ctx: ctx}
	links := partialBytecodeLinks(ctx)
	key := sourcePartialPlanKeyForContext(ctx, "runtime/changing.plush", links)

	require.NoError(t, w.WriteSourcePartial("runtime/changing.plush", "first"))
	firstPlan, ok := links.sourcePartialPlan(key, hashString("first"))
	require.True(t, ok)
	require.NoError(t, w.WriteSourcePartial("runtime/changing.plush", "second"))
	secondPlan, ok := links.sourcePartialPlan(key, hashString("second"))
	require.True(t, ok)
	require.Equal(t, "firstsecond", out.String())
	require.NotSame(t, firstPlan, secondPlan)
	require.Equal(t, 1, links.sourcePartialPlanLen())
}

func Test_VM_Fast_Writer_Write_Source_Partial_Warm_Plan_Renders_Current_Loop_Data(t *testing.T) {
	ctx := plush.NewContext()
	var out strings.Builder
	w := FastWriter{out: &out, ctx: ctx}
	const name = "runtime/items.plush"
	const source = `<%= for (_, item) in items { %><%= item %>|<% } %>`

	require.NoError(t, w.WriteSourcePartial(name, source, map[string]interface{}{
		"items": []string{"one", "two"},
	}))
	links := partialBytecodeLinks(ctx)
	key := sourcePartialPlanKeyForContext(ctx, name, links)
	_, ok := links.sourcePartialPlan(key, hashString(source))
	require.True(t, ok)

	require.NoError(t, w.WriteSourcePartial(name, source, map[string]interface{}{
		"items": []string{"three"},
	}))
	require.Equal(t, "one|two|three|", out.String())
}

func Test_VM_Fast_Writer_Write_Source_Partial_Plan_Isolates_Parent_Template_Identity(t *testing.T) {
	ctx := plush.NewContextWith(map[string]interface{}{
		meta.TemplateFileKey:         "/templates/tenant-a/page.plush",
		meta.TemplateBaseFileNameKey: "page",
		meta.TemplateExtensionKey:    "plush",
	})
	var out strings.Builder
	w := FastWriter{out: &out, ctx: ctx}
	const name = "runtime/shared-name.plush"
	const source = "content"

	require.NoError(t, w.WriteSourcePartial(name, source))
	ctx.Set(meta.TemplateFileKey, "/templates/tenant-b/page.plush")
	require.NoError(t, w.WriteSourcePartial(name, source))

	require.Equal(t, "contentcontent", out.String())
	require.Equal(t, 2, partialBytecodeLinks(ctx).sourcePartialPlanLen())
}

func Test_VM_Fast_Writer_Write_Source_Partial_Plan_Cache_Is_Bounded(t *testing.T) {
	cache := newPartialBytecodeLinkCache()
	link := &partialBytecodeLink{bytecode: &compiler.Bytecode{Static: true}}
	for i := 0; i < maxSourcePartialPlans; i++ {
		key := sourcePartialPlanKey{name: "runtime/" + strconv.Itoa(i)}
		require.True(t, cache.setSourcePartialPlan(key, &sourcePartialPlan{sourceHash: uint64(i), link: link}))
	}

	require.False(t, cache.setSourcePartialPlan(
		sourcePartialPlanKey{name: "runtime/overflow"},
		&sourcePartialPlan{sourceHash: 1, link: link},
	))
	require.Equal(t, maxSourcePartialPlans, cache.sourcePartialPlanLen())
}

func Test_VM_Fast_Writer_Write_Source_Partial_Can_Render_Nested_Feeder_Partial(t *testing.T) {
	ctx := plush.NewContextWith(map[string]interface{}{
		"name": "nested",
		"partialFeeder": func(name string) (string, error) {
			if name != "inner.plush" {
				return "", errors.New("unexpected partial")
			}
			return `<b><%= name %></b>`, nil
		},
	})
	var out strings.Builder
	w := FastWriter{out: &out, ctx: ctx}

	err := w.WriteSourcePartial("runtime/outer.plush", `<%= partial("inner.plush") %>`)
	require.NoError(t, err)
	require.Equal(t, "<b>nested</b>", out.String())
}

func Test_VM_Fast_Writer_Write_Source_Partial_Uses_Active_Render_Output(t *testing.T) {
	tmpl, err := Compile(`<%= runtime_fragment() %>`)
	require.NoError(t, err)

	ctx := plush.NewContextWith(map[string]interface{}{
		"runtime_fragment": func() template.HTML { return "fallback" },
		"name":             "Mido",
	})
	plush.EnableRenderVMHotspotDiagnostics(ctx)
	SetFastHelper(ctx, "runtime_fragment", func(w FastWriter, args FastArgs) error {
		if args.Len() != 0 {
			return ErrFastUnsupported
		}
		return w.WriteSourcePartial(
			"runtime/fragment.plush",
			`<strong><%= name %>:<%= label %></strong>`,
			map[string]interface{}{"label": "direct"},
		)
	})

	rendered, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "<strong>Mido:direct</strong>", rendered)
	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, 1, diagnostics.VMHotspots.PartialCalls)
	require.Contains(t, diagnostics.VMPartialHotspotsHeader(), "runtime/fragment.plush:1:")
}

func Test_VM_Fast_Writer_Write_Source_Partial_Charges_Sub_Render_Budget(t *testing.T) {
	ctx := plush.NewContext().WithBudget(plush.NewBudget(0))
	var out strings.Builder
	err := (FastWriter{out: &out, ctx: ctx}).WriteSourcePartial("runtime/budget.plush", "blocked")
	require.Error(t, err)
	require.Empty(t, out.String())
}

func Test_VM_Fast_Writer_Write_Source_Partial_Reports_Named_Errors(t *testing.T) {
	ctx := plush.NewContext()
	var out strings.Builder
	w := FastWriter{out: &out, ctx: ctx}

	err := w.WriteSourcePartial("runtime/error.plush", `<%= missing %>`)
	require.ErrorContains(t, err, "runtime/error.plush")
	require.ErrorContains(t, err, `"missing": unknown identifier`)

	require.ErrorContains(t, w.WriteSourcePartial("", "source"), "name must not be empty")
	require.ErrorContains(t, w.WriteSourcePartial("runtime/data.plush", "source", nil, nil), "at most one data map")
	require.ErrorIs(t, (FastWriter{}).WriteSourcePartial("runtime/nil.plush", "source"), ErrFastUnsupported)
}

func Test_VM_Render_Source_Partial_Value_Inherits_Context_And_Scopes_Data(t *testing.T) {
	ctx := plush.NewContextWith(map[string]interface{}{
		"parent": "outer",
		"local":  "parent-local",
	})

	rendered, err := RenderSourcePartial(
		ctx,
		"runtime/value.plush",
		`<strong><%= parent %>:<%= local %></strong>`,
		map[string]interface{}{"local": "child-local"},
	)

	require.NoError(t, err)
	require.Equal(t, template.HTML("<strong>outer:child-local</strong>"), rendered)
	require.Equal(t, "parent-local", ctx.Value("local"))
}

func Test_VM_Render_Source_Partial_Value_Discards_Output_On_Error(t *testing.T) {
	rendered, err := RenderSourcePartial(
		plush.NewContext(),
		"runtime/value-error.plush",
		`written first<%= missing %>`,
	)

	require.Error(t, err)
	require.Empty(t, rendered)
}
