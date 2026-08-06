package vm

import (
	"strings"
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/vm/compiler"
	"github.com/stretchr/testify/require"
)

func Test_VM_Fast_Loop_Size_Learns_And_Predicts(t *testing.T) {
	enableDetailedEstimatorDiagnostics(t)
	tmpl, err := Compile(`<%= for (_, item) in items { %><li><%= item %></li><% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan)
	require.Len(t, tmpl.bytecode.FastRenderPlan.Segments, 1)

	loop := tmpl.bytecode.FastRenderPlan.Segments[0].Loop
	require.NotNil(t, loop)
	require.NotNil(t, loop.SizeStats)

	items := []string{"aaaa", "bbbb"}
	firstCtx := plush.NewContextWith(map[string]interface{}{"items": items})
	rendered, err := tmpl.Render(firstCtx)
	require.NoError(t, err)
	require.Equal(t, "<li>aaaa</li><li>bbbb</li>", rendered)
	require.Equal(t, uint64(1), loop.SizeStats.Samples())
	require.Equal(t, len(rendered)/len(items), loop.SizeStats.BytesPerItem())
	require.Equal(t, 100*loop.SizeStats.BytesPerItem(), fastLoopGrowHint(loop, 100))
	firstDiagnostics, ok := plush.RenderDiagnosticsFromContext(firstCtx)
	require.True(t, ok)
	require.Equal(t, 1, firstDiagnostics.LoopOutput.Calls)
	require.Equal(t, len(items), firstDiagnostics.LoopOutput.Items)
	require.Equal(t, 1, firstDiagnostics.LoopOutput.KnownCount)
	require.Zero(t, firstDiagnostics.LoopOutput.Learned)
	require.Equal(t, len(rendered), firstDiagnostics.LoopOutput.Actual)
	require.Len(t, firstDiagnostics.LoopOutput.Details, 1)
	require.Equal(t, "items", firstDiagnostics.LoopOutput.Details[0].Name)
	require.Zero(t, firstDiagnostics.LoopOutput.Details[0].SamplesBefore)
	require.Equal(t, uint64(1), firstDiagnostics.LoopOutput.Details[0].SamplesAfter)
	require.Equal(t, len(rendered)/len(items), firstDiagnostics.LoopOutput.Details[0].EstimateBytesPerItem)

	manyItems := make([]string, 100)
	for i := range manyItems {
		manyItems[i] = "cccc"
	}
	secondCtx := plush.NewContextWith(map[string]interface{}{"items": manyItems})
	rendered, err = tmpl.Render(secondCtx)
	require.NoError(t, err)
	require.Len(t, rendered, len(manyItems)*len("<li>cccc</li>"))
	require.Equal(t, uint64(2), loop.SizeStats.Samples())
	require.Equal(t, len("<li>cccc</li>"), loop.SizeStats.BytesPerItem())
	secondDiagnostics, ok := plush.RenderDiagnosticsFromContext(secondCtx)
	require.True(t, ok)
	require.Equal(t, 1, secondDiagnostics.LoopOutput.Calls)
	require.Equal(t, len(manyItems), secondDiagnostics.LoopOutput.Items)
	require.Equal(t, len(rendered), secondDiagnostics.LoopOutput.Learned)
	require.Equal(t, len(rendered), secondDiagnostics.LoopOutput.Actual)
	require.Equal(t, 1, secondDiagnostics.LoopOutput.WithinTen)
	require.Equal(t, len(rendered), secondDiagnostics.LoopOutput.GrowHint)
	require.Len(t, secondDiagnostics.LoopOutput.Details, 1)
	require.Equal(t, uint64(1), secondDiagnostics.LoopOutput.Details[0].SamplesBefore)
	require.Equal(t, uint64(2), secondDiagnostics.LoopOutput.Details[0].SamplesAfter)
}

func Test_VM_Fast_Loop_Size_Grow_Uses_Remaining_Capacity_And_Cap(t *testing.T) {
	stats := &compiler.LoopSizeStats{}
	stats.Observe(1024)
	loop := &compiler.FastLoopPlan{SizeStats: stats}

	var out strings.Builder
	out.WriteString("prefix")
	require.Equal(t, fastLoopMaxGrow, fastLoopGrowHint(loop, 1000))
	require.Equal(t, fastLoopMaxGrow, growFastLoopOutput(&out, loop, 1000))
	require.GreaterOrEqual(t, out.Cap()-out.Len(), fastLoopMaxGrow)
	require.Zero(t, growFastLoopOutput(&out, loop, 1000))
}

func Test_VM_Fast_Loop_Size_Counts_Break_And_Continue_Iterations(t *testing.T) {
	type item struct {
		Name string
		Skip bool
		Stop bool
	}
	tmpl, err := Compile(`<%= for (_, item) in items { %>xx<%= if item.Stop { %><%= break %><% } %><%= if item.Skip { %><%= continue %><% } %><%= item.Name %><% } %>`)
	require.NoError(t, err)
	loop := tmpl.bytecode.FastRenderPlan.Segments[0].Loop
	require.NotNil(t, loop)

	rendered, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"items": []item{
			{Name: "A"},
			{Name: "B", Skip: true},
			{Name: "C", Stop: true},
			{Name: "D"},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, "xxAxxxx", rendered)
	require.Equal(t, uint64(1), loop.SizeStats.Samples())
	require.Equal(t, len(rendered)/3, loop.SizeStats.BytesPerItem())
}

func Test_VM_Fast_Loop_Size_Nested_Loops_Learn_Independently(t *testing.T) {
	type group struct {
		Values []string
	}
	tmpl, err := Compile(`<%= for (_, group) in groups { %>[<%= for (_, value) in group.Values { %><%= value %>,<% } %>]<% } %>`)
	require.NoError(t, err)
	outer := tmpl.bytecode.FastRenderPlan.Segments[0].Loop
	require.NotNil(t, outer)

	var inner *compiler.FastLoopPlan
	for i := range outer.Parts {
		if outer.Parts[i].Kind == compiler.FastLoopPartLoop {
			inner = outer.Parts[i].Loop
			break
		}
	}
	require.NotNil(t, inner)

	rendered, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"groups": []group{{Values: []string{"a", "b"}}, {Values: []string{"c"}}},
	}))
	require.NoError(t, err)
	require.Equal(t, "[a,b,][c,]", rendered)
	require.Equal(t, uint64(1), outer.SizeStats.Samples())
	require.Equal(t, uint64(2), inner.SizeStats.Samples())
}

func Test_VM_Fast_Loop_Size_Does_Not_Learn_From_Failed_Render(t *testing.T) {
	type item struct {
		Name string
	}
	tmpl, err := Compile(`<%= for (_, item) in items { %><%= item.Missing %><% } %>`)
	require.NoError(t, err)
	loop := tmpl.bytecode.FastRenderPlan.Segments[0].Loop
	require.NotNil(t, loop)

	_, err = tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"items": []item{{Name: "A"}},
	}))
	require.Error(t, err)
	require.Zero(t, loop.SizeStats.Samples())
}

func Benchmark_VM_Fast_Loop_Size_Grow(b *testing.B) {
	items := make([]string, 1000)
	for i := range items {
		items[i] = strings.Repeat("x", 64)
	}
	ctx := plush.NewContextWith(map[string]interface{}{"items": items})
	plan := &compiler.FastRenderPlan{Bindings: []string{"items"}}
	bindings := newFastRenderBindings(plan, ctx)
	bytesPerItem := len("<li></li>") + len(items[0])

	for _, test := range []struct {
		name     string
		adaptive bool
	}{
		{name: "natural_growth"},
		{name: "adaptive", adaptive: true},
	} {
		b.Run(test.name, func(b *testing.B) {
			loop := &compiler.FastLoopPlan{
				IterableName:      "items",
				IterableNameIndex: 0,
				Parts: []compiler.FastLoopPart{
					{Kind: compiler.FastLoopPartStatic, Value: "<li>"},
					{Kind: compiler.FastLoopPartValue},
					{Kind: compiler.FastLoopPartStatic, Value: "</li>"},
				},
			}
			if test.adaptive {
				loop.SizeStats = &compiler.LoopSizeStats{}
				loop.SizeStats.Observe(bytesPerItem)
			}
			b.ReportAllocs()
			b.SetBytes(int64(bytesPerItem * len(items)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var out strings.Builder
				handled, err := renderFastLoop(&out, ctx, bindings, loop)
				if err != nil || !handled {
					b.Fatalf("render fast loop: handled=%t err=%v", handled, err)
				}
				benchmarkSink = out.String()
			}
		})
	}
}
