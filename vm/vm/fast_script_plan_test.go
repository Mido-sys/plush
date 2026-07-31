package vm

import (
	"fmt"
	"html/template"
	"strings"
	"testing"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/vm/compiler"
	"github.com/stretchr/testify/require"
)

type fastScriptPlanMenu struct {
	Items []fastScriptPlanItem
}

type fastScriptPlanItem struct {
	Name  string
	Count int
}

type fastScriptPlanPair struct {
	Key   string
	Value string
}

type fastScriptPlanRecord struct {
	ID int
}

type fastScriptPlanBuilder struct{}

type fastScriptPlanUser struct {
	Name string
}

type fastScriptPlanRenderBlock struct {
	Type    string
	BlockID string
}

func (u fastScriptPlanUser) Render(mode string) string {
	return mode + ":" + u.Name
}

func (fastScriptPlanBuilder) RenderControl(options map[string]interface{}) string {
	name, _ := options["name"].(string)
	typ, _ := options["type"].(string)
	return name + ":" + typ
}

func (fastScriptPlanBuilder) RenderSelect(name string, options map[string]interface{}) string {
	values, _ := options["options"].([]interface{})
	selected, _ := options["value"].(string)
	return name + ":" + fmt.Sprint(len(values)) + ":" + selected
}

func (fastScriptPlanBuilder) InputTag(name string) string {
	return "input:" + name
}

func (fastScriptPlanBuilder) SelectTag(name string, options map[string]interface{}) string {
	values, _ := options["options"].([]interface{})
	selected, _ := options["value"].(string)
	return "select:" + name + ":" + fmt.Sprint(len(values)) + ":" + selected
}

func (fastScriptPlanBuilder) CheckboxTag(name string, options map[string]interface{}) string {
	return "check:" + name + ":" + fmt.Sprint(options["unchecked"])
}

func Test_VM_Fast_Render_Literal_Lets_And_Path_Loops(t *testing.T) {
	tmpl, err := Compile(`<% let title = "Default" %><title><%= title %></title><%= for (item) in menu.Items { %><%= item.Name %>;<% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"menu": fastScriptPlanMenu{Items: []fastScriptPlanItem{
			{Name: "One"},
			{Name: "Two"},
		}},
	}))
	require.NoError(t, err)
	require.Equal(t, `<title>Default</title>One;Two;`, out)
}

func Test_VM_Fast_Render_Loop_Let_With_Helper_And_Arithmetic_Arg(t *testing.T) {
	tmpl, err := Compile(`<%= for (_, product) in products { %><% let categorySeo = replace(category.CategorySeoUrl, "-outofstock", "", 0 - 1) %><%= categorySeo %>:<%= product.Name %>;<% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan)
	require.Empty(t, tmpl.bytecode.FastReject)

	ctx := plush.NewContextWith(map[string]interface{}{
		"category": struct {
			CategorySeoUrl string
		}{CategorySeoUrl: "pizza-outofstock"},
		"products": []fastScriptPlanItem{
			{Name: "One"},
			{Name: "Two"},
		},
		"replace": strings.Replace,
	})

	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, `pizza:One;pizza:Two;`, out)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathFast, diagnostics.FastPath)
	require.Empty(t, diagnostics.FastReject)
}

func Test_VM_Fast_Render_Loop_Let_Spends_Assignment_Budget(t *testing.T) {
	tmpl, err := Compile(`<%= for (_, item) in items { %><% let label = item.Name %><%= label %>;<% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan)
	require.Empty(t, tmpl.bytecode.FastReject)

	costs := plush.ZeroCosts()
	costs.Assignment = 1
	budget := plush.NewBudgetWithCosts(1, costs)
	ctx := plush.NewContextWith(map[string]interface{}{
		"items": []fastScriptPlanItem{
			{Name: "One"},
			{Name: "Two"},
		},
	}).WithBudget(budget)

	_, err = tmpl.Render(ctx)
	require.ErrorIs(t, err, plush.ErrBudgetExceeded)
	require.Equal(t, int64(2), budget.Stats().Assignments)
}

func Test_VM_Fast_Render_Loop_Assignment_Updates_Outer_Binding(t *testing.T) {
	tmpl, err := Compile(`<% let last = "" %><%= for (_, item) in items { %><% let label = item %><% last = label %><%= label %><% } %>|<%= last %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)
	require.Len(t, tmpl.bytecode.FastRenderPlan.Segments, 4)
	require.Equal(t, compiler.FastLoopPartAssign, tmpl.bytecode.FastRenderPlan.Segments[1].Loop.Parts[1].Kind)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"items": []string{"a", "b"},
	}))
	require.NoError(t, err)
	require.Equal(t, "ab|b", out)

	out, err = tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"items": []string{},
	}))
	require.NoError(t, err)
	require.Equal(t, "|", out)
}

func Test_VM_Fast_Render_Ignores_Comment_Blocks(t *testing.T) {
	tmpl, err := Compile(`<%# editor metadata lives here %><%= title %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"title": "Ready",
	}))
	require.NoError(t, err)
	require.Equal(t, "Ready", out)
}

func Test_VM_Fast_Render_Assignment_Scalar_Expressions_And_Index_Targets(t *testing.T) {
	tmpl, err := Compile(`<% let count = 0 %><% let label = "" %><% let data = {} %><%= for (_, item) in items { %><% count = count + 1 %><% label = label + item.Key %><% data[item.Key] = item.Value %><% } %><%= data[active] %>|<%= count %>|<%= label %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"active": "second",
		"items": []fastScriptPlanPair{
			{Key: "first", Value: "A"},
			{Key: "second", Value: "B"},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, "B|2|firstsecond", out)
}

func Test_VM_Fast_Render_Receiver_Call_With_Arguments(t *testing.T) {
	tmpl, err := Compile(`<%= builder.RenderControl({name: "Email", type: inputType}) %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"builder":   fastScriptPlanBuilder{},
		"inputType": "text",
	}))
	require.NoError(t, err)
	require.Equal(t, "Email:text", out)
}

func Test_VM_Fast_Render_Receiver_Call_With_Scalar_And_Hash_Arguments(t *testing.T) {
	tmpl, err := Compile(`<%= builder.RenderSelect("Level", {options: ["one", "two"], value: selected}) %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"builder":  fastScriptPlanBuilder{},
		"selected": "two",
	}))
	require.NoError(t, err)
	require.Equal(t, "Level:2:two", out)
}

func Test_VM_Fast_Render_FormFor_Doc_Syntax_With_Helper_Block_Context(t *testing.T) {
	tmpl, err := Compile(`<%= form_for(record, {action: recordPath({id: record.ID}), method: "PUT"}) { %><%= f.InputTag("Title") %>|<%= f.SelectTag("Level", {options: ["one", "two"], value: selected}) %>|<%= f.CheckboxTag("Enabled", {unchecked: false}) %><% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)

	ctx := plush.NewContextWith(map[string]interface{}{
		"record":   fastScriptPlanRecord{ID: 7},
		"selected": "two",
		"recordPath": func(data map[string]interface{}) string {
			return fmt.Sprintf("/records/%v", data["id"])
		},
		"form_for": func(_ fastScriptPlanRecord, data map[string]interface{}, help plush.HelperContext) (template.HTML, error) {
			child := help.New()
			child.Set("f", fastScriptPlanBuilder{})
			body, err := help.BlockWith(child)
			if err != nil {
				return "", err
			}
			return template.HTML(fmt.Sprintf(`<form action="%s" method="%s">%s</form>`, data["action"], data["method"], body)), nil
		},
	})

	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, `<form action="/records/7" method="PUT">input:Title|select:Level:2:two|check:Enabled:false</form>`, out)
}

func Test_VM_Fast_Render_Silent_Block_Helper_Discards_Return_And_Renders_Block(t *testing.T) {
	tmpl, err := Compile(`<% capture("extra") { %><style><%= klass %></style><% } %><%= readCapture("extra") %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)
	require.True(t, tmpl.bytecode.FastRenderPlan.Segments[0].BlockCall.Silent)

	ctx := plush.NewContextWith(map[string]interface{}{
		"klass": "online",
		"capture": func(name string, help plush.HelperContext) (template.HTML, error) {
			rendered, err := help.Block()
			if err != nil {
				return "", err
			}
			help.Set("capture:"+name, rendered)
			return template.HTML("visible return should be discarded"), nil
		},
		"readCapture": func(name string, help plush.HelperContext) template.HTML {
			value, _ := help.Value("capture:" + name).(string)
			return template.HTML(value)
		},
	})

	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "<style>online</style>", out)
}

func Test_VM_Fast_Render_Silent_Script_For_Discards_Output_And_Assigns(t *testing.T) {
	tmpl, err := Compile(`<% let collected = [] %><% for (_, item) in items { %>ignored<%= item.Value %><% collected = collected + item.Value %><% } %><%= collected[1] %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)
	require.True(t, tmpl.bytecode.FastRenderPlan.Segments[1].Loop.Silent)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"items": []struct {
			Value string
		}{
			{Value: "first"},
			{Value: "second"},
		},
	}))
	require.NoError(t, err)
	require.Equal(t, "second", out)
}

func Test_VM_Fast_Render_Block_Helper_Allows_Local_Assignment_Setup(t *testing.T) {
	tmpl, err := Compile(`<%= wrap({}) { %><% let options = {} %><% for (_, item) in items { %><% options[item.Key] = item.Value %><% } %><%= builder.RenderControl({name: options[active], type: "hidden"}) %><% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan.Segments[0].BlockCall.BlockBytecode.FastRenderPlan)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"active":  "second",
		"builder": fastScriptPlanBuilder{},
		"items": []fastScriptPlanPair{
			{Key: "first", Value: "A"},
			{Key: "second", Value: "B"},
		},
		"wrap": func(_ map[string]interface{}, help plush.HelperContext) (template.HTML, error) {
			rendered, err := help.Block()
			return template.HTML("[" + rendered + "]"), err
		},
	}))
	require.NoError(t, err)
	require.Equal(t, "[B:hidden]", out)
}

func Test_VM_Fast_Render_Loop_Local_Assignment_Does_Not_Leak(t *testing.T) {
	tmpl, err := Compile(`<% let out = "" %><%= for (_, item) in items { %><% let label = "" %><% label = item %><% out = out + label %><% } %><%= out %><%= if (label) { %>leak<% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"items": []string{"a", "b"},
	}))
	require.NoError(t, err)
	require.Equal(t, "ab", out)
}

func Test_VM_Fast_Render_Prefix_Condition_And_Loop_Concat(t *testing.T) {
	tmpl, err := Compile(`<%= if (!userSignedIn) { %>Guest<% } else { %>User<% } %><%= for (item) in menu.Items { %><%= item.Name + " x " + item.Count %>;<% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"userSignedIn": false,
		"menu": fastScriptPlanMenu{Items: []fastScriptPlanItem{
			{Name: "One", Count: 2},
			{Name: "Two", Count: 3},
		}},
	}))
	require.NoError(t, err)
	require.Equal(t, `GuestOne x 2;Two x 3;`, out)
}

func Test_VM_Fast_Render_Nested_Loop_Uses_Outer_Key(t *testing.T) {
	tmpl, err := Compile(`<%= for (i, row) in rows { %><%= for (j, col) in row { %><%= i %>,<%= j %>:<%= col %>;<% } %><% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"rows": [][]string{{"a", "b"}, {"c"}},
	}))
	require.NoError(t, err)
	require.Equal(t, `0,0:a;0,1:b;1,0:c;`, out)
}

func Test_VM_Fast_Render_Nested_Loop_Condition_Uses_Outer_Value(t *testing.T) {
	tmpl, err := Compile(`<%= for (k, messages) in flash { %><%= for (msg) in messages { %><%= if (len(messages) && messages[0] != "skip") { %><%= k %>:<%= msg %>;<% } %><% } %><% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan)

	out, err := tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"flash": map[string][]string{"notice": {"Hello", "Bye"}},
	}))
	require.NoError(t, err)
	require.Equal(t, `notice:Hello;notice:Bye;`, out)
}

func Test_VM_Fast_Render_Silent_Script_If_Discards_Output_But_Evaluates_Selected_Branch(t *testing.T) {
	tmpl, err := Compile(`<% if (show) { %>hidden<%= touch(name) %><% } else { %><%= touch("else") %><% } %>done`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan)
	require.True(t, tmpl.bytecode.FastRenderPlan.Segments[0].Conditional.Silent)

	calls := []string{}
	ctx := plush.NewContextWith(map[string]interface{}{
		"show": true,
		"name": "Mido",
		"touch": func(value string) string {
			calls = append(calls, value)
			return "called " + value
		},
	})

	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "done", out)
	require.Equal(t, []string{"Mido"}, calls)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathFast, diagnostics.FastPath)

	calls = calls[:0]
	out, err = tmpl.Render(plush.NewContextWith(map[string]interface{}{
		"show": false,
		"name": "Mido",
		"touch": func(value string) string {
			calls = append(calls, value)
			return "called " + value
		},
	}))
	require.NoError(t, err)
	require.Equal(t, "done", out)
	require.Equal(t, []string{"else"}, calls)
}

func Test_VM_Fast_Render_Silent_Script_If_Inside_Loop(t *testing.T) {
	type item struct {
		Name   string
		Hidden bool
		Stop   bool
	}

	tmpl, err := Compile(`<%= for (_, item) in items { %><% if (item.Hidden) { %>hidden<%= touch(item.Name) %><% } %><% if (item.Stop) { %><%= break %><% } %><%= item.Name %>;<% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan)

	calls := []string{}
	ctx := plush.NewContextWith(map[string]interface{}{
		"items": []item{
			{Name: "A"},
			{Name: "B", Hidden: true},
			{Name: "C", Stop: true},
			{Name: "D"},
		},
		"touch": func(value string) string {
			calls = append(calls, value)
			return value
		},
	})

	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "A;B;", out)
	require.Equal(t, []string{"B"}, calls)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathFast, diagnostics.FastPath)
}

func Test_VM_Fast_Render_Regex_And_NonString_Hash_Keys(t *testing.T) {
	tmpl, err := Compile(`<%= name ~= "^Mi" %>|<%= {true: "yes", 7: "lucky"}[true] %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)

	ctx := plush.NewContextWith(map[string]interface{}{
		"name": "Mido",
	})
	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "true|yes", out)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathFast, diagnostics.FastPath)
	require.Empty(t, diagnostics.FastReject)
}

func Test_VM_Fast_Render_Identifier_With_Digits_Unary_Minus(t *testing.T) {
	tmpl, err := Compile(`<%= -my123greet %>|<%= -my123greet + 10 %>|<%= -my123greet - 10 %>|<%= -my123greet + my123greet2 %>|<%= -my123int64 + my123float64 %>|<%= -my123int %>|<%= -my123count + 1 %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)
	require.Equal(t, []string{"my123greet", "my123greet2", "my123int64", "my123float64", "my123int", "my123count"}, tmpl.bytecode.FastRenderPlan.Bindings)
	require.Equal(t, compiler.FastValuePrefix, tmpl.bytecode.FastRenderPlan.Segments[0].ValuePlan.Kind)
	require.Equal(t, "-", tmpl.bytecode.FastRenderPlan.Segments[0].ValuePlan.Operator)

	ctx := plush.NewContextWith(map[string]interface{}{
		"my123greet":   float64(5.5),
		"my123greet2":  int64(-10),
		"my123int64":   int64(10),
		"my123float64": float64(5.5),
		"my123int":     int(4),
		"my123count":   int64(6),
	})
	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "-5.5|4.5|-15.5|-15.5|-4.5|-4|-5", out)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathFast, diagnostics.FastPath)
	require.Empty(t, diagnostics.FastReject)
}

func Test_VM_Fast_Render_Silent_Plain_Helper_Call_Discards_Return(t *testing.T) {
	tmpl, err := Compile(`<% touch(name) %><%= done %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)
	require.True(t, tmpl.bytecode.FastRenderPlan.Segments[0].Call.Silent)

	calls := []string{}
	ctx := plush.NewContextWith(map[string]interface{}{
		"name": "Mido",
		"done": "ok",
		"touch": func(value string) string {
			calls = append(calls, value)
			return "hidden"
		},
	})
	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "ok", out)
	require.Equal(t, []string{"Mido"}, calls)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathFast, diagnostics.FastPath)
}

func Test_VM_Fast_Render_Dynamic_Partial_Name_And_Layout(t *testing.T) {
	tmpl, err := Compile(`<%= partial(templateName, {name: name, layout: layoutName}) %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)
	require.Equal(t, compiler.FastRenderSegmentCall, tmpl.bytecode.FastRenderPlan.Segments[0].Kind)
	require.Equal(t, "partial", tmpl.bytecode.FastRenderPlan.Segments[0].Call.Name)

	ctx := plush.NewContextWith(map[string]interface{}{
		"templateName": "row.plush",
		"layoutName":   "shell.plush",
		"name":         "Mido",
		"partialFeeder": func(name string) (string, error) {
			switch name {
			case "row.plush":
				return `<span><%= name %></span>`, nil
			case "shell.plush":
				return `<section><%= yield %></section>`, nil
			default:
				return "", fmt.Errorf("missing partial %s", name)
			}
		},
	})

	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "<section><span>Mido</span></section>", out)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathFast, diagnostics.FastPath)
}

func Test_VM_Fast_Render_Dynamic_Callee_And_Chained_Receiver_Call(t *testing.T) {
	tmpl, err := Compile(`<%= helpers[name]("x") %>|<%= makeUser(name).Render("short") %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)

	ctx := plush.NewContextWith(map[string]interface{}{
		"name": "echo",
		"helpers": map[string]interface{}{
			"echo": func(value string) string {
				return "dyn:" + value
			},
		},
		"makeUser": func(name string) fastScriptPlanUser {
			return fastScriptPlanUser{Name: name}
		},
	})

	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "dyn:x|short:echo", out)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathFast, diagnostics.FastPath)
}

func Test_VM_Fast_Render_Loop_Registered_Helper_Context_Includes_Loop_Binding(t *testing.T) {
	tmpl, err := Compile(`<%= for (_, block) in blocks { %><%= render(block.Type + ".plush.html", {settings: block}) %><% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)

	ctx := plush.NewContextWith(map[string]interface{}{
		"blocks": []fastScriptPlanRenderBlock{{Type: "product-option", BlockID: "test-1234"}},
		"render": func(string, map[string]interface{}, plush.HelperContext) (template.HTML, error) {
			return "", nil
		},
	})
	SetFastHelper(ctx, "render", func(w FastWriter, args FastArgs) error {
		fileName, ok := args.String(0)
		if !ok {
			return ErrFastUnsupported
		}
		rawData, ok := args.Raw(1)
		if !ok {
			return ErrFastUnsupported
		}
		data, ok := rawData.(map[string]interface{})
		if !ok {
			return ErrFastUnsupported
		}
		settings, ok := data["settings"].(fastScriptPlanRenderBlock)
		if !ok {
			return fmt.Errorf("%q: unknown identifier", "settings")
		}
		block, ok := w.Context().Value("block").(fastScriptPlanRenderBlock)
		if !ok {
			return fmt.Errorf("%q: unknown identifier", "block")
		}
		w.WriteHTMLString(fileName + "|" + settings.BlockID + "|" + block.BlockID)
		return nil
	})

	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "product-option.plush.html|test-1234|test-1234", out)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathFast, diagnostics.FastPath)
	require.Empty(t, diagnostics.FastReject)
}

func Test_VM_Fast_Render_Output_If_Return_Nil_Loop_And_Iterator_Assignment(t *testing.T) {
	tmpl, err := Compile(`<%= if active { return "yes" } %>after`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)
	ctx := plush.NewContextWith(map[string]interface{}{"active": true})
	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "yesafter", out)
	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathFast, diagnostics.FastPath)

	tmpl, err = Compile(`<%= for (_, item) in nil { %>x<% } %><%= for (_, item) in items { %><% item = "x" %><%= item %><% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)
	ctx = plush.NewContextWith(map[string]interface{}{
		"items": []string{"a", "b"},
	})
	out, err = tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "xx", out)

	tmpl, err = Compile(`<%= for (_, value) in items { %><% if (value == 1) { %>P<% return "A" } %><%= value %><% } %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)
	ctx = plush.NewContextWith(map[string]interface{}{
		"items": []int{1, 2},
	})
	out, err = tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "PA2", out)
}

func Test_VM_Fast_Render_Function_Literal_Uses_Generic_VM_Not_Interpreter_Fallback(t *testing.T) {
	tmpl, err := Compile(`<% let add = fn(x) { return x + 1 } %><%= add(2) %>`)
	require.NoError(t, err)
	require.NotNil(t, tmpl.bytecode.FastRenderPlan, tmpl.bytecode.FastReject)
	require.Empty(t, tmpl.bytecode.FastReject)

	ctx := plush.NewContext()
	out, err := tmpl.Render(ctx)
	require.NoError(t, err)
	require.Equal(t, "3", out)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, plush.RenderFastPathGeneric, diagnostics.FastPath)
	require.NotEqual(t, plush.RenderFastPathInterpreterFallback, diagnostics.FastPath)
	require.Empty(t, diagnostics.FastReject)
}
