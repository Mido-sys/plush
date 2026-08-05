package vm

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/vm/compiler"
	"github.com/stretchr/testify/require"
)

func Test_Prepared_Fast_Binding_Scope_Restores_Locals_Without_Allocating(t *testing.T) {
	bindings := fastBindingScopeTestBindings(64)
	bindings.localOK[3] = true
	bindings.localVals[3] = "first-original"
	bindings.localOK[19] = true
	bindings.localVals[19] = "second-original"
	bindings.localOK[41] = true
	bindings.localVals[41] = "outer-original"
	plan := compiler.FastBindingSyncPlan{
		Prepared:         true,
		LocalNameIndexes: []int{3, 19},
	}

	allocs := testing.AllocsPerRun(1000, func() {
		scoped := bindings
		var undo fastBindingUndo
		undo.capturePlan(&scoped, plan)
		scoped.setLocal(3, "first-temporary")
		scoped.setLocal(19, "second-temporary")
		scoped.setLocal(41, "outer-updated")
		undo.restore(&scoped)
	})

	require.Zero(t, allocs)
	require.True(t, bindings.localOK[3])
	require.Equal(t, "first-original", bindings.localVals[3])
	require.True(t, bindings.localOK[19])
	require.Equal(t, "second-original", bindings.localVals[19])
	require.True(t, bindings.localOK[41])
	require.Equal(t, "outer-updated", bindings.localVals[41])
}

func Test_Fast_Loop_Current_Locals_Restore_Shadowed_Iterator_Bindings(t *testing.T) {
	bindings := fastRenderBindings{
		names:     []string{"key", "item", "outer"},
		localOK:   []bool{true, true, true},
		localVals: []interface{}{"old-key", "old-item", "outer-value"},
	}
	loop := &compiler.FastLoopPlan{KeyName: "key", ValueName: "item"}

	scoped, undo := fastLoopBindingsWithCurrentLocals(bindings, loop, 7, "current-item")
	require.Equal(t, 7, scoped.localVals[0])
	require.Equal(t, "current-item", scoped.localVals[1])

	scoped.setLocal(2, "outer-updated")
	undo.restore(&scoped)

	require.Equal(t, "old-key", bindings.localVals[0])
	require.Equal(t, "old-item", bindings.localVals[1])
	require.Equal(t, "outer-updated", bindings.localVals[2])
}

func Test_Fast_Binding_Undo_Restores_Inline_And_Overflow_Entries(t *testing.T) {
	bindings := fastBindingScopeTestBindings(16)
	indexes := make([]int, 12)
	for i := range indexes {
		indexes[i] = i
		bindings.localVals[i] = i
	}
	plan := compiler.FastBindingSyncPlan{Prepared: true, LocalNameIndexes: indexes}

	scoped := bindings
	var undo fastBindingUndo
	undo.capturePlan(&scoped, plan)
	for _, index := range indexes {
		scoped.setLocal(index, "temporary")
	}
	undo.restore(&scoped)

	for i := range indexes {
		require.Equal(t, i, bindings.localVals[i])
	}
	require.Zero(t, undo.count)
	require.Nil(t, undo.extra)
}

func Test_Unprepared_Fast_Binding_Scope_Retains_Full_Copy_Fallback(t *testing.T) {
	bindings := fastRenderBindings{
		names:     []string{"local"},
		localOK:   []bool{true},
		localVals: []interface{}{"outer"},
	}
	parts := []compiler.FastLoopPart{{Kind: compiler.FastLoopPartLet, NameIndex: 0, Value: "local"}}

	_, scoped, undo, cleanup := fastRenderLoopPartScopeForLet(plush.NewContext(), bindings, parts, compiler.FastBindingSyncPlan{})
	defer cleanup()
	scoped.setLocal(0, "inner")
	undo.restore(&scoped)

	require.Equal(t, "outer", bindings.localVals[0])
}

var fastBindingScopeBenchmarkSink interface{}

func Benchmark_VM_Fast_Binding_Scope(b *testing.B) {
	bindings := fastBindingScopeTestBindings(128)
	localIndexes := []int{3, 61, 119}
	plan := compiler.FastBindingSyncPlan{
		Prepared:         true,
		LocalNameIndexes: localIndexes,
	}

	b.Run("prepared_undo", func(b *testing.B) {
		b.ReportAllocs()
		var result interface{}
		for i := 0; i < b.N; i++ {
			scoped := bindings
			outer := bindings
			var undo fastBindingUndo
			undo.capturePlan(&scoped, plan)
			for _, index := range localIndexes {
				scoped.setLocal(index, "temporary")
			}
			result = scoped.localVals[localIndexes[1]]
			undo.restorePlan(&scoped, &outer, plan)
		}
		fastBindingScopeBenchmarkSink = result
	})

	b.Run("legacy_full_copy", func(b *testing.B) {
		b.ReportAllocs()
		var result interface{}
		for i := 0; i < b.N; i++ {
			scoped := fastRenderBindingsWithLocalCopy(bindings)
			for _, index := range localIndexes {
				scoped.setLocal(index, "temporary")
			}
			result = scoped.localVals[localIndexes[1]]
		}
		fastBindingScopeBenchmarkSink = result
	})
}

var fastNestedBindingScopeBenchmarkSink string

func Benchmark_VM_Fast_Nested_Binding_Scopes(b *testing.B) {
	source := fastNestedBindingScopeBenchmarkSource(128)
	rows := [][]string{
		{"a", "b", "c", "d", "e", "f", "g", "h"},
		{"i", "j", "k", "l", "m", "n", "o", "p"},
		{"q", "r", "s", "t", "u", "v", "w", "x"},
		{"y", "z", "a", "b", "c", "d", "e", "f"},
		{"g", "h", "i", "j", "k", "l", "m", "n"},
		{"o", "p", "q", "r", "s", "t", "u", "v"},
		{"w", "x", "y", "z", "a", "b", "c", "d"},
		{"e", "f", "g", "h", "i", "j", "k", "l"},
	}

	prepared, err := Compile(source)
	require.NoError(b, err)
	legacy, err := Compile(source)
	require.NoError(b, err)
	legacyOuter := legacy.bytecode.FastRenderPlan.Segments[len(legacy.bytecode.FastRenderPlan.Segments)-1].Loop
	require.NotNil(b, legacyOuter)
	legacyOuter.BindingSync.Prepared = false
	for i := range legacyOuter.Parts {
		if legacyOuter.Parts[i].Kind == compiler.FastLoopPartLoop {
			legacyOuter.Parts[i].Loop.BindingSync.Prepared = false
		}
	}

	bench := func(b *testing.B, tmpl *Template) {
		ctx := plush.NewContextWith(map[string]interface{}{"rows": rows})
		_, err := tmpl.Render(ctx)
		require.NoError(b, err)
		b.ReportAllocs()
		b.ResetTimer()
		var rendered string
		for i := 0; i < b.N; i++ {
			rendered, err = tmpl.Render(ctx)
			if err != nil {
				b.Fatal(err)
			}
		}
		fastNestedBindingScopeBenchmarkSink = rendered
	}

	b.Run("prepared_undo", func(b *testing.B) {
		bench(b, prepared)
	})
	b.Run("legacy_full_copy", func(b *testing.B) {
		bench(b, legacy)
	})
}

func fastNestedBindingScopeBenchmarkSource(bindingCount int) string {
	var source strings.Builder
	for i := 0; i < bindingCount; i++ {
		source.WriteString(`<% let binding`)
		source.WriteString(strconv.Itoa(i))
		source.WriteString(` = "value" %>`)
	}
	source.WriteString(`<%= for (_, row) in rows { %><% let outerLocal = row %><%= for (_, item) in row { %><% let innerLocal = item %><%= innerLocal %><% } %><% } %>`)
	return source.String()
}

func fastBindingScopeTestBindings(count int) fastRenderBindings {
	bindings := fastRenderBindings{
		ctx:       plush.NewContext(),
		names:     make([]string, count),
		localOK:   make([]bool, count),
		localVals: make([]interface{}, count),
	}
	for i := range bindings.names {
		bindings.localOK[i] = true
		bindings.localVals[i] = "original"
	}
	return bindings
}
