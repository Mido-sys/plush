package vm

import (
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/vm/compiler"
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
