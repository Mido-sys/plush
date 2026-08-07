package vm

import (
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/vm/code"
	"github.com/gobuffalo/plush/v5/vm/compiler"
	"github.com/gobuffalo/plush/v5/vm/object"
	"github.com/stretchr/testify/require"
)

func Test_Prepared_Fast_Binding_Sync_Updates_Outer_Binding_Without_Allocating(t *testing.T) {
	ctx := plush.NewContextWith(map[string]interface{}{"result": "updated"})
	bindings := fastRenderBindings{
		ctx:       ctx,
		names:     []string{"result"},
		localOK:   make([]bool, 1),
		localVals: make([]interface{}, 1),
	}
	plan := compiler.FastBindingSyncPlan{Prepared: true, NameIndexes: []int{0}}

	allocs := testing.AllocsPerRun(1000, func() {
		syncFastLoopAssignmentBindingsPlan(ctx, &bindings, nil, plan)
	})

	require.Zero(t, allocs)
	require.True(t, bindings.localOK[0])
	require.Equal(t, "updated", bindings.localVals[0])
}

func Test_Unprepared_Fast_Binding_Sync_Uses_Legacy_Analysis(t *testing.T) {
	ctx := plush.NewContextWith(map[string]interface{}{"result": "updated"})
	bindings := fastRenderBindings{
		ctx:       ctx,
		names:     []string{"result"},
		localOK:   make([]bool, 1),
		localVals: make([]interface{}, 1),
	}
	parts := []compiler.FastLoopPart{{
		Kind:      compiler.FastLoopPartAssign,
		Value:     "result",
		NameIndex: 0,
	}}

	syncFastLoopAssignmentBindingsPlan(ctx, &bindings, parts, compiler.FastBindingSyncPlan{})

	require.True(t, bindings.localOK[0])
	require.Equal(t, "updated", bindings.localVals[0])
}

func Benchmark_VM_Fast_Binding_Sync(b *testing.B) {
	ctx := plush.NewContextWith(map[string]interface{}{"result": "updated"})
	bindings := fastRenderBindings{
		ctx:       ctx,
		names:     []string{"result"},
		localOK:   make([]bool, 1),
		localVals: make([]interface{}, 1),
	}
	parts := benchmarkFastBindingSyncParts()

	b.Run("prepared", func(b *testing.B) {
		plan := compiler.FastBindingSyncPlan{Prepared: true, NameIndexes: []int{0}}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			syncFastLoopAssignmentBindingsPlan(ctx, &bindings, parts, plan)
		}
	})
	b.Run("legacy", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			syncFastLoopAssignmentBindingsPlan(ctx, &bindings, parts, compiler.FastBindingSyncPlan{})
		}
	})
}

func Benchmark_VM_Dynamic_Context_Binding_Sync(b *testing.B) {
	constants := []object.Object{
		&object.String{Value: "name0"},
		&object.String{Value: "name1"},
		&object.String{Value: "name2"},
		&object.String{Value: "name3"},
		&object.String{Value: "name4"},
		&object.String{Value: "Prop"},
		&object.String{Value: "<b>"},
	}
	source := plush.NewContextWith(map[string]interface{}{
		"name0": "zero",
		"name1": "one",
		"name2": "two",
		"name3": "three",
		"name4": "four",
	})
	instructions := benchmarkDynamicContextSyncInstructions()

	b.Run("prepared", func(b *testing.B) {
		target := newIDLookupTestContext(map[string]interface{}{})
		machine := newRuntimeHelperTestVM(target)
		machine.constants = constants
		frame := machine.currentFrame()
		frame.cl.Fn.DynamicContextNamesReady = true
		frame.cl.Fn.DynamicContextNameIndexes = []int{0, 1, 2, 3, 4}
		machine.syncDynamicContextBindingsFromContext(target, source, frame)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			machine.syncDynamicContextBindingsFromContext(target, source, frame)
		}
	})

	b.Run("fallback", func(b *testing.B) {
		target := newIDLookupTestContext(map[string]interface{}{})
		machine := newRuntimeHelperTestVM(target)
		machine.constants = constants
		frame := machine.currentFrame()
		frame.cl.Fn.Instructions = instructions
		machine.syncDynamicContextBindingsFromContext(target, source, frame)

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			machine.syncDynamicContextBindingsFromContext(target, source, frame)
		}
	})
}

func Benchmark_VM_Partial_Overlay_InternID(b *testing.B) {
	b.Run("parent_same_name", func(b *testing.B) {
		parent := plush.NewContext()
		parent.InternID("section")

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			parent.InternID("section")
		}
	})

	b.Run("overlay_cached_same_name", func(b *testing.B) {
		parent := plush.NewContext()
		overlay := borrowPartialOverlayContext(parent)
		defer releasePartialOverlayContext(overlay)
		overlay.InternID("section")

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			overlay.InternID("section")
		}
	})
}

func benchmarkDynamicContextSyncInstructions() code.Instructions {
	var ins code.Instructions
	for i := 0; i < 10; i++ {
		ins = append(ins, code.Make(code.OpWriteHTML, 6)...)
		ins = append(ins, code.Make(code.OpWriteName, 0)...)
		ins = append(ins, code.Make(code.OpGetNameOrNull, 1)...)
		ins = append(ins, code.Make(code.OpWriteNameProperty, 2, 5)...)
		ins = append(ins, code.Make(code.OpGetNameOrJumpMissing, 3, 0)...)
		ins = append(ins, code.Make(code.OpWriteNameCall, 4, 0)...)
	}
	return ins
}

func benchmarkFastBindingSyncParts() []compiler.FastLoopPart {
	parts := make([]compiler.FastLoopPart, 0, 14)
	for _, name := range []string{
		"loopLocal0", "loopLocal1", "loopLocal2", "loopLocal3", "loopLocal4",
		"loopLocal5", "loopLocal6", "loopLocal7", "loopLocal8", "loopLocal9",
	} {
		parts = append(parts, compiler.FastLoopPart{Kind: compiler.FastLoopPartLet, Value: name, NameIndex: 1})
	}
	parts = append(parts,
		compiler.FastLoopPart{
			Kind: compiler.FastLoopPartConditional,
			Conditional: &compiler.FastLoopConditionalPlan{
				Branches: []compiler.FastLoopConditionalBranch{{Parts: []compiler.FastLoopPart{
					{Kind: compiler.FastLoopPartLet, Value: "branchLocal", NameIndex: 2},
					{Kind: compiler.FastLoopPartAssign, Value: "result", NameIndex: 0},
				}}},
			},
		},
		compiler.FastLoopPart{Kind: compiler.FastLoopPartAssign, Value: "loopLocal0", NameIndex: 1},
	)
	return parts
}
