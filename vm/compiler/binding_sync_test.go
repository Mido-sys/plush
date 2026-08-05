package compiler

import (
	"testing"

	"github.com/gobuffalo/plush/v5/parser"
	"github.com/stretchr/testify/require"
)

func Test_Compiler_Bytecode_Prepares_Fast_Binding_Sync_Plans(t *testing.T) {
	program, err := parser.Parse(`<% let result = "" %><%= for (_, item) in items { %><% let local = item %><% result = result + local %><% } %><%= result %>`)
	require.NoError(t, err)

	compiled := New()
	require.NoError(t, compiled.Compile(program))
	bytecode := compiled.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan)
	require.Len(t, bytecode.FastRenderPlan.Segments, 3)

	loop := bytecode.FastRenderPlan.Segments[1].Loop
	require.NotNil(t, loop)
	require.True(t, loop.BindingSync.Prepared)
	require.Len(t, loop.BindingSync.NameIndexes, 1)
	require.Equal(t, "result", bytecode.FastRenderPlan.Bindings[loop.BindingSync.NameIndexes[0]])
	require.Len(t, loop.BindingSync.LocalNameIndexes, 1)
	require.Equal(t, "local", bytecode.FastRenderPlan.Bindings[loop.BindingSync.LocalNameIndexes[0]])
}

func Test_Prepare_Fast_Loop_Binding_Sync_Plans_Respect_Lexical_Scopes(t *testing.T) {
	nameTarget := func(name string, index int) *FastAssignTarget {
		return &FastAssignTarget{Kind: FastAssignTargetName, Name: name, NameIndex: index}
	}
	assign := func(name string, index int) FastLoopPart {
		return FastLoopPart{
			Kind:         FastLoopPartAssign,
			Value:        name,
			NameIndex:    index,
			AssignTarget: nameTarget(name, index),
		}
	}

	nested := &FastLoopPlan{Parts: []FastLoopPart{
		{Kind: FastLoopPartLet, Value: "nestedLocal", NameIndex: 4},
		assign("result", 0),
	}}
	conditional := &FastLoopConditionalPlan{
		Branches: []FastLoopConditionalBranch{
			{Parts: []FastLoopPart{
				{Kind: FastLoopPartLet, Value: "branchLocal", NameIndex: 2},
				assign("branchLocal", 2),
				assign("total", 3),
			}},
			{Parts: []FastLoopPart{assign("local", 1)}},
		},
	}
	loop := &FastLoopPlan{Parts: []FastLoopPart{
		assign("result", 0),
		{Kind: FastLoopPartLet, Value: "local", NameIndex: 1},
		assign("local", 1),
		{Kind: FastLoopPartConditional, Conditional: conditional},
		{Kind: FastLoopPartLoop, Loop: nested},
		assign("result", 0),
	}}
	plan := &FastRenderPlan{Segments: []FastRenderSegment{{Kind: FastRenderSegmentLoop, Loop: loop}}}

	prepareFastBindingSyncPlans(plan)

	require.True(t, loop.BindingSync.Prepared)
	require.Equal(t, []int{0, 3}, loop.BindingSync.NameIndexes)
	require.Equal(t, []int{1}, loop.BindingSync.LocalNameIndexes)
	require.Equal(t, []int{3}, conditional.Branches[0].BindingSync.NameIndexes)
	require.Equal(t, []int{2}, conditional.Branches[0].BindingSync.LocalNameIndexes)
	require.Equal(t, []int{1}, conditional.Branches[1].BindingSync.NameIndexes)
	require.Empty(t, conditional.Branches[1].BindingSync.LocalNameIndexes)
	require.True(t, conditional.ElseBindingSync.Prepared)
	require.Empty(t, conditional.ElseBindingSync.NameIndexes)
	require.Equal(t, []int{0}, nested.BindingSync.NameIndexes)
	require.Equal(t, []int{4}, nested.BindingSync.LocalNameIndexes)
}

func Test_Prepare_Fast_Segment_Binding_Sync_Plans_Exclude_Local_And_Non_Name_Assignments(t *testing.T) {
	conditional := &FastConditionalPlan{
		Branches: []FastConditionalBranch{{Segments: []FastRenderSegment{
			{Kind: FastRenderSegmentAssign, Value: "outer", NameIndex: 0},
			{Kind: FastRenderSegmentLet, Value: "local", NameIndex: 1},
			{Kind: FastRenderSegmentAssign, Value: "local", NameIndex: 1},
			{
				Kind:      FastRenderSegmentAssign,
				NameIndex: 2,
				AssignTarget: &FastAssignTarget{
					Kind: FastAssignTargetIndex,
				},
			},
			{Kind: FastRenderSegmentAssign, Value: "outer", NameIndex: 0},
		}}},
	}
	plan := &FastRenderPlan{Segments: []FastRenderSegment{{Kind: FastRenderSegmentConditional, Conditional: conditional}}}

	prepareFastBindingSyncPlans(plan)

	require.True(t, conditional.Branches[0].BindingSync.Prepared)
	require.Equal(t, []int{0}, conditional.Branches[0].BindingSync.NameIndexes)
	require.Equal(t, []int{1}, conditional.Branches[0].BindingSync.LocalNameIndexes)
	require.True(t, conditional.ElseBindingSync.Prepared)
}

func Test_Prepare_Fast_Binding_Sync_Plan_Identifies_Outer_Assignment_Shadowed_Later(t *testing.T) {
	loop := &FastLoopPlan{Parts: []FastLoopPart{
		{
			Kind:      FastLoopPartAssign,
			Value:     "result",
			NameIndex: 0,
			AssignTarget: &FastAssignTarget{
				Kind:      FastAssignTargetName,
				Name:      "result",
				NameIndex: 0,
			},
		},
		{Kind: FastLoopPartLet, Value: "result", NameIndex: 0},
	}}
	plan := &FastRenderPlan{Segments: []FastRenderSegment{{Kind: FastRenderSegmentLoop, Loop: loop}}}

	prepareFastBindingSyncPlans(plan)

	require.Equal(t, []int{0}, loop.BindingSync.NameIndexes)
	require.Equal(t, []int{0}, loop.BindingSync.LocalNameIndexes)
	require.Equal(t, []int{0}, loop.BindingSync.ParentNameIndexes)
}
