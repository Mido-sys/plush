package compiler

import (
	"testing"

	"github.com/gobuffalo/plush/v5/parser"
	"github.com/stretchr/testify/require"
)

func Test_Fast_Render_Plan_Literal_Lets_And_Path_Loops(t *testing.T) {
	input := `<% let title = "Default" %><title><%= title %></title><%= for (item) in menu.Items { %><%= item.Name %>;<% } %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	plan := compiler.Bytecode().FastRenderPlan
	require.NotNil(t, plan)
	require.Len(t, plan.Segments, 5)
	require.Equal(t, FastRenderSegmentLet, plan.Segments[0].Kind)
	require.Equal(t, FastValueString, plan.Segments[0].ValuePlan.Kind)
	require.Equal(t, FastRenderSegmentLoop, plan.Segments[4].Kind)
	require.Equal(t, FastValuePath, plan.Segments[4].Loop.Iterable.Kind)
	require.Equal(t, "menu", plan.Segments[4].Loop.Iterable.Value)
	require.Len(t, plan.Segments[4].Loop.Iterable.Path, 1)
	require.Equal(t, "Items", plan.Segments[4].Loop.Iterable.Path[0].Value)
}

func Test_Fast_Render_Plan_Loop_Let_With_Helper_And_Arithmetic_Arg(t *testing.T) {
	input := `<%= for (_, item) in items { %><% let normalizedPath = replace(document.Path, "-draft", "", 0 - 1) %><%= normalizedPath %>:<%= item.Name %>;<% } %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 1)

	loop := bytecode.FastRenderPlan.Segments[0].Loop
	require.NotNil(t, loop)
	require.GreaterOrEqual(t, len(loop.Parts), 4)
	require.Equal(t, FastLoopPartLet, loop.Parts[0].Kind)
	require.Equal(t, "normalizedPath", loop.Parts[0].Value)
	require.Equal(t, FastValueCall, loop.Parts[0].ValuePlan.Kind)
	require.NotNil(t, loop.Parts[0].ValuePlan.Call)
	require.Equal(t, "replace", loop.Parts[0].ValuePlan.Call.Name)
	require.Len(t, loop.Parts[0].ValuePlan.Call.Args, 4)
	require.Equal(t, FastValueInfix, loop.Parts[0].ValuePlan.Call.Args[3].Kind)
	require.Equal(t, "-", loop.Parts[0].ValuePlan.Call.Args[3].Operator)

	require.Equal(t, FastLoopPartValuePath, loop.Parts[1].Kind)
	require.Equal(t, FastValueName, loop.Parts[1].ValuePlan.Kind)
	require.Equal(t, "normalizedPath", loop.Parts[1].ValuePlan.Value)
}

func Test_Fast_Render_Plan_Loop_Assignment_Updates_Outer_Binding(t *testing.T) {
	input := `<% let last = "" %><%= for (_, item) in items { %><% let label = item %><% last = label %><%= label %><% } %><%= last %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 3)

	loop := bytecode.FastRenderPlan.Segments[1].Loop
	require.NotNil(t, loop)
	require.Len(t, loop.Parts, 3)
	require.Equal(t, FastLoopPartLet, loop.Parts[0].Kind)
	require.Equal(t, FastLoopPartAssign, loop.Parts[1].Kind)
	require.Equal(t, "last", loop.Parts[1].Value)
	require.Equal(t, FastValueName, loop.Parts[1].ValuePlan.Kind)
	require.Equal(t, "label", loop.Parts[1].ValuePlan.Value)

	require.Equal(t, FastRenderSegmentName, bytecode.FastRenderPlan.Segments[2].Kind)
	require.Equal(t, "last", bytecode.FastRenderPlan.Segments[2].Value)
}

func Test_Fast_Render_Plan_Ignores_Comment_Blocks(t *testing.T) {
	input := `<%# editor metadata lives here %><%= title %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 1)
	require.Equal(t, FastRenderSegmentName, bytecode.FastRenderPlan.Segments[0].Kind)
	require.Equal(t, "title", bytecode.FastRenderPlan.Segments[0].Value)
}

func Test_Fast_Render_Plan_Comment_With_Static_Output_Has_Zero_Binding_Plan(t *testing.T) {
	program, err := parser.Parse(`<%# metadata %>Ready`)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)
	require.Empty(t, bytecode.FastRenderPlan.Bindings)
	require.Len(t, bytecode.FastRenderPlan.Segments, 1)
	require.Equal(t, FastRenderSegmentStatic, bytecode.FastRenderPlan.Segments[0].Kind)
	require.Equal(t, "Ready", bytecode.FastRenderPlan.Segments[0].Value)
}

func Test_Fast_Render_Plan_Assignment_Scalar_Expressions_And_Index_Targets(t *testing.T) {
	input := `<% let count = 0 %><% let data = {} %><%= for (_, item) in items { %><% count = count + 1 %><% data[item.Key] = item.Value %><% } %><%= data[active] %><%= count %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)

	loop := bytecode.FastRenderPlan.Segments[2].Loop
	require.NotNil(t, loop)
	require.Len(t, loop.Parts, 2)
	require.Equal(t, FastLoopPartAssign, loop.Parts[0].Kind)
	require.Equal(t, FastValueConcat, loop.Parts[0].ValuePlan.Kind)
	require.Equal(t, FastLoopPartAssign, loop.Parts[1].Kind)
	require.NotNil(t, loop.Parts[1].AssignTarget)
	require.Equal(t, FastAssignTargetIndex, loop.Parts[1].AssignTarget.Kind)

	require.Equal(t, FastRenderSegmentValue, bytecode.FastRenderPlan.Segments[3].Kind)
	require.Equal(t, FastValueIndex, bytecode.FastRenderPlan.Segments[3].ValuePlan.Kind)
}

func Test_Fast_Render_Plan_Receiver_Call_With_Arguments(t *testing.T) {
	input := `<%= builder.RenderControl({name: "Email", type: inputType}) %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 1)

	segment := bytecode.FastRenderPlan.Segments[0]
	require.Equal(t, FastRenderSegmentValue, segment.Kind)
	require.Equal(t, FastValuePath, segment.ValuePlan.Kind)
	require.Len(t, segment.ValuePlan.Path, 2)
	require.Equal(t, FastPathStepProperty, segment.ValuePlan.Path[0].Kind)
	require.True(t, segment.ValuePlan.Path[0].Method)
	require.Equal(t, FastPathStepCall, segment.ValuePlan.Path[1].Kind)
	require.Len(t, segment.ValuePlan.Path[1].Args, 1)
	require.Equal(t, FastValueHash, segment.ValuePlan.Path[1].Args[0].Kind)
}

func Test_Fast_Render_Plan_Receiver_Call_With_Scalar_And_Hash_Arguments(t *testing.T) {
	input := `<%= builder.RenderSelect("Level", {options: ["one", "two"], value: selected}) %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 1)

	segment := bytecode.FastRenderPlan.Segments[0]
	require.Equal(t, FastRenderSegmentValue, segment.Kind)
	require.Equal(t, FastValuePath, segment.ValuePlan.Kind)
	require.Len(t, segment.ValuePlan.Path, 2)
	require.Equal(t, FastPathStepCall, segment.ValuePlan.Path[1].Kind)
	require.Len(t, segment.ValuePlan.Path[1].Args, 2)
	require.Equal(t, FastValueString, segment.ValuePlan.Path[1].Args[0].Kind)
	require.Equal(t, FastValueHash, segment.ValuePlan.Path[1].Args[1].Kind)
}

func Test_Fast_Render_Plan_FormFor_Doc_Syntax(t *testing.T) {
	input := `<%= form_for(record, {action: recordPath({id: record.ID}), method: "PUT"}) { %><%= f.InputTag("Title") %><%= f.SelectTag("Level", {options: ["one", "two"], value: selected}) %><%= f.CheckboxTag("Enabled", {unchecked: false}) %><% } %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 1)

	blockCall := bytecode.FastRenderPlan.Segments[0].BlockCall
	require.NotNil(t, blockCall)
	require.Equal(t, "form_for", blockCall.Name)
	require.Len(t, blockCall.Args, 2)
	require.Equal(t, FastValueName, blockCall.Args[0].Kind)
	require.Equal(t, "record", blockCall.Args[0].Value)

	options := blockCall.Args[1]
	require.Equal(t, FastValueHash, options.Kind)
	require.Len(t, options.Pairs, 2)
	require.Equal(t, "action", options.Pairs[0].Key)
	require.Equal(t, FastValueCall, options.Pairs[0].Value.Kind)
	require.Equal(t, "recordPath", options.Pairs[0].Value.Call.Name)
	require.Len(t, options.Pairs[0].Value.Call.Args, 1)
	require.Equal(t, FastValueHash, options.Pairs[0].Value.Call.Args[0].Kind)
	require.Equal(t, "method", options.Pairs[1].Key)
	require.Equal(t, FastValueString, options.Pairs[1].Value.Kind)

	require.NotNil(t, blockCall.BlockBytecode)
	require.NotNil(t, blockCall.BlockBytecode.FastRenderPlan)
}

func Test_Fast_Render_Plan_Silent_Block_Helper_Call(t *testing.T) {
	input := `<% capture("extra") { %><style><%= klass %></style><% } %><%= readCapture("extra") %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 2)

	segment := bytecode.FastRenderPlan.Segments[0]
	require.Equal(t, FastRenderSegmentBlockCall, segment.Kind)
	require.NotNil(t, segment.BlockCall)
	require.Equal(t, "capture", segment.BlockCall.Name)
	require.True(t, segment.BlockCall.Silent)
	require.NotNil(t, segment.BlockCall.BlockBytecode)
}

func Test_Fast_Render_Plan_Silent_Script_For_Allows_Assignments(t *testing.T) {
	input := `<% let collected = [] %><% for (_, item) in items { %>ignored<%= item.Value %><% collected = collected + item.Value %><% } %><%= collected[1] %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 3)

	loop := bytecode.FastRenderPlan.Segments[1].Loop
	require.NotNil(t, loop)
	require.True(t, loop.Silent)
	require.GreaterOrEqual(t, len(loop.Parts), 3)
	require.Equal(t, FastLoopPartAssign, loop.Parts[len(loop.Parts)-1].Kind)
	require.Equal(t, FastValueConcat, loop.Parts[len(loop.Parts)-1].ValuePlan.Kind)
}

func Test_Fast_Render_Plan_Loop_Local_Assignment_After_Let(t *testing.T) {
	input := `<% let out = "" %><%= for (_, item) in items { %><% let label = "" %><% label = item %><% out = out + label %><% } %><%= out %><%= if (label) { %>leak<% } %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)

	loop := bytecode.FastRenderPlan.Segments[1].Loop
	require.NotNil(t, loop)
	require.Len(t, loop.Parts, 3)
	require.Equal(t, FastLoopPartLet, loop.Parts[0].Kind)
	require.Equal(t, FastLoopPartAssign, loop.Parts[1].Kind)
	require.Equal(t, "label", loop.Parts[1].Value)
	require.Equal(t, FastLoopPartAssign, loop.Parts[2].Kind)
	require.Equal(t, "out", loop.Parts[2].Value)
}

func Test_Fast_Render_Plan_Prefix_Condition_And_Loop_Concat(t *testing.T) {
	input := `<%= if (!userSignedIn) { %>Guest<% } else { %>User<% } %><%= for (item) in menu.Items { %><%= item.Name + " x " + item.Count %>;<% } %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	plan := compiler.Bytecode().FastRenderPlan
	require.NotNil(t, plan)
	require.Len(t, plan.Segments, 2)
	require.Equal(t, FastRenderSegmentConditional, plan.Segments[0].Kind)
	require.NotNil(t, plan.Segments[0].Conditional)
	require.Len(t, plan.Segments[0].Conditional.Branches, 1)
	require.Equal(t, FastValuePrefix, plan.Segments[0].Conditional.Branches[0].Condition.Kind)
	require.Equal(t, "!", plan.Segments[0].Conditional.Branches[0].Condition.Operator)

	require.Equal(t, FastRenderSegmentLoop, plan.Segments[1].Kind)
	require.NotNil(t, plan.Segments[1].Loop)
	require.Len(t, plan.Segments[1].Loop.Parts, 2)
	require.Equal(t, FastLoopPartValuePath, plan.Segments[1].Loop.Parts[0].Kind)
	require.Equal(t, FastValueConcat, plan.Segments[1].Loop.Parts[0].ValuePlan.Kind)
	require.Equal(t, "+", plan.Segments[1].Loop.Parts[0].ValuePlan.Operator)
	require.Equal(t, FastLoopPartStatic, plan.Segments[1].Loop.Parts[1].Kind)
	require.Equal(t, ";", plan.Segments[1].Loop.Parts[1].Value)
}

func Test_Fast_Render_Plan_Nested_Loop_Tracks_Outer_Names(t *testing.T) {
	input := `<%= for (i, row) in rows { %><%= for (j, col) in row { %><%= i %>,<%= j %>:<%= col %>;<% } %><% } %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	plan := compiler.Bytecode().FastRenderPlan
	require.NotNil(t, plan)
	require.Len(t, plan.Segments, 1)
	require.Equal(t, FastRenderSegmentLoop, plan.Segments[0].Kind)

	outer := plan.Segments[0].Loop
	require.NotNil(t, outer)
	require.Len(t, outer.Parts, 1)
	require.Equal(t, FastLoopPartLoop, outer.Parts[0].Kind)

	inner := outer.Parts[0].Loop
	require.NotNil(t, inner)
	require.ElementsMatch(t, []string{"i", "row"}, inner.OuterNames)
	require.Len(t, inner.Parts, 6)
	require.Equal(t, FastLoopPartValuePath, inner.Parts[0].Kind)
	require.Equal(t, FastValueName, inner.Parts[0].ValuePlan.Kind)
	require.Equal(t, "i", inner.Parts[0].ValuePlan.Value)
	require.Equal(t, FastLoopPartKey, inner.Parts[2].Kind)
	require.Equal(t, FastLoopPartValue, inner.Parts[4].Kind)
}

func Test_Fast_Render_Plan_Silent_Script_If(t *testing.T) {
	input := `<% if (show) { %>hidden<%= touch(name) %><% } else { %><%= touch("else") %><% } %>done`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	plan := compiler.Bytecode().FastRenderPlan
	require.NotNil(t, plan)
	require.Len(t, plan.Segments, 2)

	conditional := plan.Segments[0].Conditional
	require.NotNil(t, conditional)
	require.True(t, conditional.Silent)
	require.Len(t, conditional.Branches, 1)
	require.Len(t, conditional.Branches[0].Segments, 2)
	require.Equal(t, FastRenderSegmentStatic, conditional.Branches[0].Segments[0].Kind)
	require.Equal(t, FastRenderSegmentCall, conditional.Branches[0].Segments[1].Kind)
	require.Len(t, conditional.ElseSegments, 1)
	require.Equal(t, FastRenderSegmentCall, conditional.ElseSegments[0].Kind)
	require.Equal(t, FastRenderSegmentStatic, plan.Segments[1].Kind)
}

func Test_Fast_Render_Plan_Silent_Script_If_Inside_Loop(t *testing.T) {
	input := `<%= for (_, item) in items { %><% if (item.Hidden) { %><%= touch(item.Name) %><% } %><%= item.Name %><% } %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	plan := compiler.Bytecode().FastRenderPlan
	require.NotNil(t, plan)
	require.Len(t, plan.Segments, 1)

	loop := plan.Segments[0].Loop
	require.NotNil(t, loop)
	require.Len(t, loop.Parts, 2)
	require.Equal(t, FastLoopPartConditional, loop.Parts[0].Kind)
	require.True(t, loop.Parts[0].Conditional.Silent)
	require.Len(t, loop.Parts[0].Conditional.Branches, 1)
	require.Len(t, loop.Parts[0].Conditional.Branches[0].Parts, 1)
	require.Equal(t, FastLoopPartCall, loop.Parts[0].Conditional.Branches[0].Parts[0].Kind)
	require.Equal(t, FastLoopPartValueProperty, loop.Parts[1].Kind)
}

func Test_Fast_Render_Plan_Silent_Script_If_Allows_Assignments(t *testing.T) {
	input := `<% if (show) { name = "changed" } %><%= name %>`
	program, err := parser.Parse(input)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 2)
	conditional := bytecode.FastRenderPlan.Segments[0]
	require.Equal(t, FastRenderSegmentConditional, conditional.Kind)
	require.NotNil(t, conditional.Conditional)
	require.True(t, conditional.Conditional.Silent)
	require.Len(t, conditional.Conditional.Branches, 1)
	require.Len(t, conditional.Conditional.Branches[0].Segments, 1)
	require.Equal(t, FastRenderSegmentAssign, conditional.Conditional.Branches[0].Segments[0].Kind)
}

func Test_Fast_Render_Plan_Extended_Syntax_Does_Not_Fallback(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		assert func(*testing.T, *Bytecode)
	}{
		{
			name:  "regex match",
			input: `<%= name ~= "^Mi" %>`,
			assert: func(t *testing.T, bytecode *Bytecode) {
				require.Len(t, bytecode.FastRenderPlan.Segments, 1)
				require.Equal(t, FastValueInfix, bytecode.FastRenderPlan.Segments[0].ValuePlan.Kind)
				require.Equal(t, "~=", bytecode.FastRenderPlan.Segments[0].ValuePlan.Operator)
			},
		},
		{
			name:  "unary minus digit identifier arithmetic",
			input: `<%= -my123greet %>|<%= -my123greet + my123greet2 %>`,
			assert: func(t *testing.T, bytecode *Bytecode) {
				require.Equal(t, []string{"my123greet", "my123greet2"}, bytecode.FastRenderPlan.Bindings)
				require.Len(t, bytecode.FastRenderPlan.Segments, 3)
				require.Equal(t, FastValuePrefix, bytecode.FastRenderPlan.Segments[0].ValuePlan.Kind)
				require.Equal(t, "-", bytecode.FastRenderPlan.Segments[0].ValuePlan.Operator)
				require.Equal(t, FastValueConcat, bytecode.FastRenderPlan.Segments[2].ValuePlan.Kind)
				require.Equal(t, FastValuePrefix, bytecode.FastRenderPlan.Segments[2].ValuePlan.Left.Kind)
				require.Equal(t, FastValueName, bytecode.FastRenderPlan.Segments[2].ValuePlan.Right.Kind)
			},
		},
		{
			name:  "silent plain helper call",
			input: `<% touch(name) %><%= done %>`,
			assert: func(t *testing.T, bytecode *Bytecode) {
				require.Len(t, bytecode.FastRenderPlan.Segments, 2)
				require.Equal(t, FastRenderSegmentCall, bytecode.FastRenderPlan.Segments[0].Kind)
				require.True(t, bytecode.FastRenderPlan.Segments[0].Call.Silent)
			},
		},
		{
			name:  "dynamic partial name and layout",
			input: `<%= partial(templateName, {name: name, layout: layoutName}) %>`,
			assert: func(t *testing.T, bytecode *Bytecode) {
				require.Len(t, bytecode.FastRenderPlan.Segments, 1)
				segment := bytecode.FastRenderPlan.Segments[0]
				require.Equal(t, FastRenderSegmentCall, segment.Kind)
				require.Equal(t, "partial", segment.Call.Name)
				require.Len(t, segment.Call.Args, 2)
				require.Equal(t, FastValueName, segment.Call.Args[0].Kind)
				require.Equal(t, FastValueHash, segment.Call.Args[1].Kind)
			},
		},
		{
			name:  "dynamic callee and chained receiver call",
			input: `<%= helpers[name]("x") %>|<%= makeUser(name).Render("short") %>`,
			assert: func(t *testing.T, bytecode *Bytecode) {
				require.Len(t, bytecode.FastRenderPlan.Segments, 3)
				require.Equal(t, FastRenderSegmentValue, bytecode.FastRenderPlan.Segments[0].Kind)
				require.Equal(t, FastValueIndex, bytecode.FastRenderPlan.Segments[0].ValuePlan.Kind)
				require.Equal(t, FastRenderSegmentValue, bytecode.FastRenderPlan.Segments[2].Kind)
				require.Equal(t, FastValueCall, bytecode.FastRenderPlan.Segments[2].ValuePlan.Kind)
				require.NotEmpty(t, bytecode.FastRenderPlan.Segments[2].ValuePlan.Path)
			},
		},
		{
			name:  "top-level return",
			input: `<% return done %><%= after %>`,
			assert: func(t *testing.T, bytecode *Bytecode) {
				require.Len(t, bytecode.FastRenderPlan.Segments, 2)
				require.Equal(t, FastRenderSegmentReturn, bytecode.FastRenderPlan.Segments[0].Kind)
			},
		},
		{
			name:  "return inside output if and loop",
			input: `<%= if (enabled) { return label } else { return fallback } %><%= for (_, item) in items { return item } %>`,
			assert: func(t *testing.T, bytecode *Bytecode) {
				require.Len(t, bytecode.FastRenderPlan.Segments, 2)
				conditional := bytecode.FastRenderPlan.Segments[0].Conditional
				require.NotNil(t, conditional)
				require.Len(t, conditional.Branches[0].Segments, 1)
				require.NotEqual(t, FastRenderSegmentReturn, conditional.Branches[0].Segments[0].Kind)
				loop := bytecode.FastRenderPlan.Segments[1].Loop
				require.NotNil(t, loop)
				require.Len(t, loop.Parts, 1)
				require.Equal(t, FastLoopPartReturn, loop.Parts[0].Kind)
			},
		},
		{
			name:  "nil loop and loop iterator assignment",
			input: `<%= for (_, item) in nil { item = "x" } %><%= for (_, item) in items { item = "x"; return item } %>`,
			assert: func(t *testing.T, bytecode *Bytecode) {
				require.Len(t, bytecode.FastRenderPlan.Segments, 2)
				require.Equal(t, "nil", bytecode.FastRenderPlan.Segments[0].Loop.Iterable.Value)
				loop := bytecode.FastRenderPlan.Segments[1].Loop
				require.NotNil(t, loop)
				require.Len(t, loop.Parts, 2)
				require.Equal(t, FastLoopPartAssign, loop.Parts[0].Kind)
				require.Equal(t, "item", loop.Parts[0].Value)
				require.Equal(t, FastLoopPartReturn, loop.Parts[1].Kind)
			},
		},
		{
			name:  "non-string hash literal keys",
			input: `<%= {true: "yes", 7: "lucky"}[true] %>`,
			assert: func(t *testing.T, bytecode *Bytecode) {
				require.Len(t, bytecode.FastRenderPlan.Segments, 1)
				require.Equal(t, FastValueIndex, bytecode.FastRenderPlan.Segments[0].ValuePlan.Kind)
				hash := bytecode.FastRenderPlan.Segments[0].ValuePlan.Left
				require.NotNil(t, hash)
				require.Equal(t, FastValueHash, hash.Kind)
				require.NotNil(t, hash.Pairs[0].KeyPlan)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, err := parser.Parse(tt.input)
			require.NoError(t, err)

			compiler := New()
			require.NoError(t, compiler.Compile(program))

			bytecode := compiler.Bytecode()
			require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
			require.Empty(t, bytecode.FastReject)
			tt.assert(t, bytecode)
		})
	}
}

func Test_Fast_Render_Plan_Unsupported_Syntax_Uses_Generic_VM(t *testing.T) {
	program, err := parser.Parse(`<% let add = fn(x) { return x + 1 } %><%= add(2) %>`)
	require.NoError(t, err)

	compiler := New()
	require.NoError(t, compiler.Compile(program))

	bytecode := compiler.Bytecode()
	require.NotNil(t, bytecode.FastRenderPlan, bytecode.FastReject)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 1)
	require.Equal(t, FastRenderSegmentGeneric, bytecode.FastRenderPlan.Segments[0].Kind)

	program, err = parser.Parse(`<%H "hole" %><%= name %>`)
	require.NoError(t, err)

	compiler = New()
	require.NoError(t, compiler.Compile(program))

	bytecode = compiler.Bytecode()
	require.True(t, bytecode.HasHoles)
	require.NotNil(t, bytecode.FastRenderPlan)
	require.Empty(t, bytecode.FastReject)
	require.Len(t, bytecode.FastRenderPlan.Segments, 1)
	require.Equal(t, FastRenderSegmentGeneric, bytecode.FastRenderPlan.Segments[0].Kind)
}
