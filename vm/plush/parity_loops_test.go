package plush_test

import (
	"encoding/json"
	"html/template"
	"strconv"
	"strings"
	"testing"
)

type parityLoopMenu struct {
	Items []parityLoopItem
}

type parityLoopItem struct {
	Name  string
	Count int
}

type paritySelectableEntry struct {
	ID string
}

type parityMediaRecord struct {
	Assets []parityMediaAsset
}

type parityMediaAsset struct {
	URL string
	Alt string
}

func Test_Parity_Loops_Array_Return(t *testing.T) {
	compareRender(t, `<%= for (i,v) in ["a", "b", "c"] { return v } %>`, emptyContext)
}

func Test_Parity_Loops_Context_Slice(t *testing.T) {
	compareRender(t, `<%= for (i,v) in items { %><%= i %>:<%= v %>;<% } %>`, contextWith(map[string]interface{}{
		"items": []string{"a", "b"},
	}))
}

func Test_Parity_Loops_Key_Only(t *testing.T) {
	compareRender(t, `<%= for (v) in ["a", "b", "c"] {%><%=v%><%} %>`, emptyContext)
}

func Test_Parity_Loops_Key_Value_And_Outer_Same_Identifier(t *testing.T) {
	compareRender(t, `<%= for (i,v) in ["a", "b", "c"] {%><%=i%><%=v%><%} %>`, emptyContext)
	compareRender(t, `<% let i = 10000 %><%= for (i,v) in ["a", "b", "c"] {%><%=i%><%=v%><%} %><%= i %>`, emptyContext)
}

func Test_Parity_Loops_Update_Outer_Binding(t *testing.T) {
	compareRender(t, `<% let varTest = "" %><% for (i,v) in ["a", "b", "c"] {varTest = v} %><%= varTest %>`, emptyContext)
}

func Test_Parity_Loops_Nested_Output_Branches_Update_Outer_Binding(t *testing.T) {
	input := `<% let selected = entries[0] %>
<%= if (input["target_id"] != "" && count(entries) > 0) { %>
	<%= for (i, entry) in entries { %>
		<%= if (entry.ID == input["target_id"]) { %>
			<% selected = entry %>
		<% } %>
	<% } %>
<% } %>
<%= selected.ID %>`
	for _, tt := range []struct {
		name     string
		form     map[string]string
		expected string
	}{
		{name: "updates selected value", form: map[string]string{"target_id": "second"}, expected: "second"},
		{name: "keeps initial value when branch skips", form: map[string]string{"target_id": ""}, expected: "first"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			factory := contextWith(map[string]interface{}{
				"input": tt.form,
				"entries": []paritySelectableEntry{
					{ID: "first"},
					{ID: "second"},
				},
				"count": func(values []paritySelectableEntry) int {
					return len(values)
				},
			})
			compareRender(t, input, factory)

			vmOut, vmErr := renderVM(t, input, factory)
			if vmErr != nil {
				t.Fatalf("unexpected VM error: %v", vmErr)
			}
			if actual := strings.TrimSpace(vmOut); actual != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}

func Test_Parity_Loops_Nested_If_Appends_Local_String_To_Outer_Array(t *testing.T) {
	input := `<% let handlers = [] %>
<% let items = [1,2,3,4,5,6,7,8,9,0,10] %>
<%= for (index, item) in items { %>
	<%= if(true) { %>
		<% let handle = "test" + to_string(index) %>
		<% handlers = handlers + handle %>
		<script>console.log(<%= json_encode(handlers) %>)</script>
	<% } %>
<% } %>
<%= json_encode(handlers) %>`
	compareRender(t, input, contextWith(map[string]interface{}{
		"to_string": strconv.Itoa,
		"json_encode": func(value interface{}) template.HTML {
			data, _ := json.Marshal(value)
			return template.HTML(data)
		},
	}))
}

func Test_Parity_Loops_Scoped_Assignment_Combinations(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "silent conditional syncs an outer assignment",
			input:    `<% let result = [] %><% if (true) { %>discarded<% let local = "A" %><% result = result + local %><% } %><%= result[0] %>`,
			expected: "A",
		},
		{
			name:     "else branch syncs an outer assignment",
			input:    `<% let result = [] %><%= if (false) { %><% let local = "wrong" %><% result = result + local %><% } else { %><% let local = "B" %><% result = result + local %><% } %><%= result[0] %>`,
			expected: "B",
		},
		{
			name:     "nested conditionals sync an outer assignment",
			input:    `<% let result = [] %><%= if (true) { %><% let prefix = "A" %><%= if (true) { %><% let suffix = "B" %><% result = result + (prefix + suffix) %><% } %><% } %><%= result[0] %>`,
			expected: "AB",
		},
		{
			name:     "else if branch syncs an outer assignment",
			input:    `<% let result = "start" %><%= if (false) { %><% result = "wrong" %><% } else if (true) { %><% let local = "selected" %><% result = local %><% } else { %><% result = "also-wrong" %><% } %><%= result %>`,
			expected: "selected",
		},
		{
			name:     "silent loop conditional syncs between iterations",
			input:    `<% let result = [] %><%= for (_, item) in [1,2,3] { %><% if (item > 1) { %>discarded<% let local = item %><% result = result + local %><% } %><% } %><%= result[0] %><%= result[1] %>`,
			expected: "23",
		},
		{
			name:     "nested loops sync an outer assignment",
			input:    `<% let result = [] %><%= for (_, row) in [[1,2],[3]] { %><%= for (_, item) in row { %><%= if (true) { %><% let local = item %><% result = result + local %><% } %><% } %><% } %><%= result[0] %><%= result[1] %><%= result[2] %>`,
			expected: "123",
		},
		{
			name:     "nested loop assignment is visible before the parent iteration ends",
			input:    `<% let result = "" %><%= for (_, row) in [["A","B"],["C"]] { %><%= for (_, item) in row { %><% let local = item %><% result = result + local %><% } %><% result = result + "|" %><% } %><%= result %>`,
			expected: "AB|C|",
		},
		{
			name:     "loop inside a scoped conditional syncs an outer assignment",
			input:    `<% let result = "" %><%= if (true) { %><% let prefix = "x" %><%= for (_, item) in ["A","B"] { %><% let local = prefix + item %><% result = result + local %><% } %><% } %><%= result %>`,
			expected: "xAxB",
		},
		{
			name:     "nested loop local shadow does not leak while outer assignment syncs",
			input:    `<% let label = "outer" %><% let result = "" %><%= for (_, row) in [["A","B"]] { %><%= for (_, item) in row { %><% let label = item %><% label = label + "!" %><% result = result + label %><% } %><% } %><%= result %>|<%= label %>`,
			expected: "A!B!|outer",
		},
		{
			name:     "continue preserves an outer assignment",
			input:    `<% let result = [] %><%= for (_, item) in [1,2,3] { if (item == 2) { let local = item; result = result + local; continue }; result = result + item } %><%= result[0] %><%= result[1] %><%= result[2] %>`,
			expected: "123",
		},
		{
			name:     "break preserves an outer assignment",
			input:    `<% let result = [] %><%= for (_, item) in [1,2,3] { if (item == 2) { let local = item; result = result + local; break }; result = result + item } %><%= result[0] %><%= result[1] %>`,
			expected: "12",
		},
		{
			name:     "multiple outer assignments sync independently",
			input:    `<% let left = "" %><% let right = "" %><%= for (_, item) in ["A","B"] { %><%= if (true) { %><% let local = item %><% left = left + local %><% right = local + right %><% } %><% } %><%= left %>|<%= right %>`,
			expected: "AB|BA",
		},
		{
			name:     "branch local assignment does not replace shadowed outer binding",
			input:    `<% let label = "outer" %><%= if (true) { %><% let label = "inner" %><% label = label + "!" %><%= label %><% } %>|<%= label %>`,
			expected: "inner!|outer",
		},
		{
			name:     "local shadow in an unselected branch does not hide selected assignment",
			input:    `<% let result = "start" %><%= if (false) { %><% let result = "shadow" %><% result = "wrong" %><% } else { %><% result = "selected" %><% } %><%= result %>`,
			expected: "selected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comparePlannedRender(t, tt.input, emptyContext, tt.expected)
		})
	}
}

func Test_Parity_Loops_Scoped_Assignment_Iterable_Matrix(t *testing.T) {
	stringPointer := []string{"A", "B"}
	tests := []struct {
		name     string
		input    string
		factory  contextFactory
		expected string
	}{
		{
			name:     "native string slice",
			input:    `<% let result = "" %><%= for (_, item) in items { %><%= if (true) { %><% let local = item %><% result = result + local %><% } %><% } %><%= result %>`,
			factory:  contextWith(map[string]interface{}{"items": []string{"A", "B"}}),
			expected: "AB",
		},
		{
			name:     "native interface slice",
			input:    `<% let result = "" %><%= for (_, item) in items { %><%= if (true) { %><% let local = item %><% result = result + local %><% } %><% } %><%= result %>`,
			factory:  contextWith(map[string]interface{}{"items": []interface{}{"A", "B"}}),
			expected: "AB",
		},
		{
			name:     "typed struct slice",
			input:    `<% let result = "" %><%= for (_, item) in items { %><%= if (true) { %><% let local = item.Name %><% result = result + local %><% } %><% } %><%= result %>`,
			factory:  contextWith(map[string]interface{}{"items": []parityLoopItem{{Name: "A"}, {Name: "B"}}}),
			expected: "AB",
		},
		{
			name:     "native array",
			input:    `<% let result = "" %><%= for (_, item) in items { %><%= if (true) { %><% let local = item %><% result = result + local %><% } %><% } %><%= result %>`,
			factory:  contextWith(map[string]interface{}{"items": [2]string{"A", "B"}}),
			expected: "AB",
		},
		{
			name:     "pointer to native slice",
			input:    `<% let result = "" %><%= for (_, item) in items { %><%= if (true) { %><% let local = item %><% result = result + local %><% } %><% } %><%= result %>`,
			factory:  contextWith(map[string]interface{}{"items": &stringPointer}),
			expected: "AB",
		},
		{
			name:     "native map",
			input:    `<% let result = 0 %><%= for (_, item) in items { %><%= if (true) { %><% let local = item %><% result = result + local %><% } %><% } %><%= result %>`,
			factory:  contextWith(map[string]interface{}{"items": map[string]int{"a": 1, "b": 2}}),
			expected: "3",
		},
		{
			name:     "existing context binding",
			input:    `<%= for (_, item) in ["A","B"] { %><%= if (true) { %><% let local = item %><% result = result + local %><% } %><% } %><%= result %>`,
			factory:  contextWith(map[string]interface{}{"result": ""}),
			expected: "AB",
		},
		{
			name:     "indexed outer mutation",
			input:    `<% let result = {} %><%= for (_, item) in ["A","B"] { %><%= if (true) { %><% let local = item %><% result[item] = local %><% } %><% } %><%= result["A"] %><%= result["B"] %>`,
			factory:  emptyContext,
			expected: "AB",
		},
		{
			name:     "assigned iterator does not replace shadowed outer binding",
			input:    `<% let item = "outer" %><%= for (_, item) in ["A"] { %><% item = item + "!" %><%= item %><% } %>|<%= item %>`,
			factory:  emptyContext,
			expected: "A!|outer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comparePlannedRender(t, tt.input, tt.factory, tt.expected)
		})
	}
}

func Test_Parity_Loops_Guarded_Collection_Loop_Reads_Value_Property(t *testing.T) {
	input := `<%= if (len(record.Assets) > 0 && record.Assets[0].URL) { %>
	<%= for (index, asset) in record.Assets { %>
		<link href="<%= asset.URL %>" data-alt="<%= asset.Alt %>" data-index="<%= index %>" />
	<% } %>
<% } %>`
	compareRender(t, input, contextWith(map[string]interface{}{
		"record": parityMediaRecord{Assets: []parityMediaAsset{
			{URL: "/first.png", Alt: "First"},
			{URL: "/second.png", Alt: "Second"},
		}},
	}))
	vmOut, vmErr := renderVM(t, input, contextWith(map[string]interface{}{
		"record": parityMediaRecord{Assets: []parityMediaAsset{
			{URL: "/first.png", Alt: "First"},
			{URL: "/second.png", Alt: "Second"},
		}},
	}))
	if vmErr != nil {
		t.Fatalf("unexpected VM error: %v", vmErr)
	}
	if !strings.Contains(vmOut, `href="/first.png"`) || !strings.Contains(vmOut, `href="/second.png"`) {
		t.Fatalf("expected VM output to include both loop values, got %q", vmOut)
	}
}

func Test_Parity_Loops_Continue(t *testing.T) {
	compareRender(t, `<%= for (i,v) in [1, 2, 3] { if (v == 2) { continue } return v } %>`, emptyContext)
}

func Test_Parity_Loops_Break(t *testing.T) {
	compareRender(t, `<%= for (i,v) in [1, 2, 3] { if (v == 2) { break } return v } %>`, emptyContext)
}

func Test_Parity_Loops_Continue_Output_Accumulation(t *testing.T) {
	compareRender(t, `<%= for (i,v) in [1, 2, 3, 4] {
		%>Start<%
		if (v == 1 || v == 3) {
			%>Odd<%
			continue
		}
		return v
	} %>`, emptyContext)
}

func Test_Parity_Loops_Continue_No_Output(t *testing.T) {
	compareRender(t, `<%= for (i,v) in [1, 2, 3] {
		continue
		return v
	} %>`, emptyContext)
}

func Test_Parity_Loops_Break_Output_Accumulation(t *testing.T) {
	compareRender(t, `<%= for (i,v) in [1, 2, 3, 4] {
		%>Start<%
		if (v == 3) {
			%>Stop<%
			break
		}
		return v
	} %>`, emptyContext)
}

func Test_Parity_Loops_Break_First_Value_With_Output(t *testing.T) {
	compareRender(t, `<%= for (i,v) in [1, 2, 3] {
		if (v == 1) {
			%><%=v%><%
			break
		}
		return v
	} %>`, emptyContext)
}

func Test_Parity_Loops_Single_Entry_Map(t *testing.T) {
	compareRender(t, `<%= for (k,v) in myMap { %><%= k + ":" + v%><% } %>`, contextWith(map[string]interface{}{
		"myMap": map[string]string{
			"a": "A",
		},
	}))
}

func Test_Parity_Loops_Map_Contains_Entries(t *testing.T) {
	input := `<%= for (k,v) in myMap { %><%= k + ":" + v%>;<% } %>`
	factory := contextWith(map[string]interface{}{
		"myMap": map[string]string{
			"a": "A",
			"b": "B",
		},
	})

	interpreterOut, interpreterErr := renderInterpreter(input, factory)
	vmOut, vmErr := renderVM(t, input, factory)
	if interpreterErr != nil || vmErr != nil {
		t.Fatalf("unexpected errors\ninterpreter: %v\nvm: %v", interpreterErr, vmErr)
	}
	for _, fragment := range []string{"a:A;", "b:B;"} {
		if !strings.Contains(interpreterOut, fragment) {
			t.Fatalf("interpreter output %q missing %q", interpreterOut, fragment)
		}
		if !strings.Contains(vmOut, fragment) {
			t.Fatalf("VM output %q missing %q", vmOut, fragment)
		}
	}
}

func Test_Parity_Loops_Nested_Slices(t *testing.T) {
	compareRender(t, `<%= for (i,row) in rows { %><%= for (j,col) in row { %><%= i %>,<%= j %>:<%= col %>;<% } %><% } %>`, contextWith(map[string]interface{}{
		"rows": [][]string{{"a", "b"}, {"c"}},
	}))
}

func Test_Parity_Loops_Nested_Flash_Style_Condition(t *testing.T) {
	compareRender(t, `<%= for (k, messages) in flash { %><%= for (msg) in messages { %><%= if (len(messages) && messages[0] != "skip") { %><%= k %>:<%= msg %>;<% } %><% } %><% } %>`, contextWith(map[string]interface{}{
		"flash": map[string][]string{"notice": {"Hello", "Bye"}},
	}))
	compareRender(t, `<%= for (k, messages) in flash { %><%= for (msg) in messages { %><%= if (len(messages) && messages[0] != "skip") { %><%= k %>:<%= msg %>;<% } %><% } %><% } %>`, contextWith(map[string]interface{}{
		"flash": map[string][]string{"notice": {"skip", "Bye"}},
	}))
}

func Test_Parity_Loops_Iterator_Helpers(t *testing.T) {
	compareRender(t, `<%= for (v) in range(3,5) { %><%=v%><% } %>|<%= for (v) in between(3,6) { %><%=v%><% } %>|<%= for (v) in until(3) { %><%=v%><% } %>`, emptyContext)
}

func Test_Parity_Loops_Nil_Iterable(t *testing.T) {
	compareRender(t, `<%= for (i,v) in nil { return v } %>`, emptyContext)
	compareBothRenderError(t, `<%= for (i,v) in nilValue { return v } %>`, contextWith(map[string]interface{}{
		"nilValue": nil,
	}))
}

func Test_Parity_Loops_Missing_Map_Key_Iterable(t *testing.T) {
	compareRender(t, `<%= for (k, v) in flash["errors"] { %><%= k %>:<%= v %><% } %>`, contextWith(map[string]interface{}{
		"flash": map[string][]string{},
	}))
}

func Test_Parity_Loops_Prefix_Condition_And_Struct_Field_Concat(t *testing.T) {
	compareRender(t, `<%= if (!userSignedIn) { %>Guest<% } else { %>User<% } %><%= for (item) in menu.Items { %><%= item.Name + " x " + item.Count %>;<% } %>`, contextWith(map[string]interface{}{
		"userSignedIn": false,
		"menu": parityLoopMenu{Items: []parityLoopItem{
			{Name: "One", Count: 2},
			{Name: "Two", Count: 3},
		}},
	}))
}

func Test_Parity_Loops_Iterator_Scope_Does_Not_Leak(t *testing.T) {
	compareBothRenderError(t, `<%= for (i,v) in ["a"] { %><%= i %>:<%= v %><% } %><%= i %>`, emptyContext)
	compareBothRenderError(t, `<%= for (i,v) in ["a"] { %><%= i %>:<%= v %><% } %><%= v %>`, emptyContext)
}
