package plush_test

import (
	"fmt"
	"html/template"
	"strings"
	"sync"
	"testing"

	rootplush "github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/hctx"
	vmplush "github.com/gobuffalo/plush/v5/vm/plush"
	"github.com/stretchr/testify/require"
)

const forceGenericBytecode = `<% let forceBytecode = fn() { return "unused" } %>`

type paritySlotRecord struct {
	ID    string
	Label string
}

func paritySlotRecords() []paritySlotRecord {
	return []paritySlotRecord{
		{ID: "first", Label: "First"},
		{ID: "second", Label: "Second"},
		{ID: "third", Label: "Third"},
	}
}

func paritySlotContext(values map[string]interface{}) contextFactory {
	return func() hctx.Context {
		data := map[string]interface{}{
			"records": paritySlotRecords(),
			"count": func(records []paritySlotRecord) int {
				return len(records)
			},
		}
		for name, value := range values {
			data[name] = value
		}
		return rootplush.NewContextWith(data)
	}
}

func compareGenericVMParity(t *testing.T, input string, factory contextFactory, expected string) {
	t.Helper()

	interpreterOut, interpreterErr := rootplush.Render(input, factory())
	vmCtx := factory()
	vmOut, vmErr := renderVMContext(t, input, vmCtx)

	require.NoError(t, interpreterErr)
	require.NoError(t, vmErr)
	require.Equal(t, expected, interpreterOut)
	require.Equal(t, expected, vmOut)

	diagnostics, ok := rootplush.RenderDiagnosticsFromContext(vmCtx)
	require.True(t, ok, "expected VM render diagnostics")
	require.Equal(t, rootplush.RenderFastPathGeneric, diagnostics.FastPath)
}

func Test_Parity_Generic_VM_Disjoint_Local_Nested_Assignment_Outcomes(t *testing.T) {
	input := forceGenericBytecode +
		`<%= if (enabled) { %>` +
		`<% let selected = records[0] %>` +
		`<%= if (target != "" && count(records) > 0) { %>` +
		`<%= for (_, candidate) in records { %>` +
		`<%= if (candidate.ID == target) { %><% selected = candidate %><% } %>` +
		`<% } %>` +
		`<% } %>` +
		`<%= selected.Label %>` +
		`<% } %>` +
		`<%= if (true) { %><% let later = "unused" %><% } %>`

	for _, tt := range []struct {
		name     string
		target   string
		expected string
	}{
		{name: "matching target", target: "second", expected: "Second"},
		{name: "missing target", target: "missing", expected: "First"},
		{name: "empty target", target: "", expected: "First"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			compareGenericVMParity(t, input, paritySlotContext(map[string]interface{}{
				"enabled": true,
				"target":  tt.target,
			}), tt.expected)
		})
	}
}

func Test_Parity_Generic_VM_Later_Disjoint_Local_Executed_And_Skipped(t *testing.T) {
	input := forceGenericBytecode +
		`<%= if (enabled) { %>` +
		`<% let current = "start" %>` +
		`<%= for (_, item) in ["updated"] { %><% current = item %><% } %>` +
		`<%= current %>` +
		`<% } %>|` +
		`<%= if (laterEnabled) { %><% let later = "later" %><%= later %><% } %>`

	for _, tt := range []struct {
		name         string
		laterEnabled bool
		expected     string
	}{
		{name: "later block executes", laterEnabled: true, expected: "updated|later"},
		{name: "later block skips", laterEnabled: false, expected: "updated|"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			compareGenericVMParity(t, input, paritySlotContext(map[string]interface{}{
				"enabled":      true,
				"laterEnabled": tt.laterEnabled,
			}), tt.expected)
		})
	}
}

func Test_Parity_Generic_VM_Repeated_Disjoint_Name_Assigns_Independently(t *testing.T) {
	selectionBlock := func(target string) string {
		return `<% let selected = records[0] %>` +
			`<%= for (_, candidate) in records { %>` +
			`<%= if (candidate.ID == "` + target + `") { %><% selected = candidate %><% } %>` +
			`<% } %><%= selected.Label %>`
	}
	input := forceGenericBytecode +
		`<%= if (firstEnabled) { %>` + selectionBlock("second") + `<% } %>|` +
		`<%= if (true) { %><% let separator = "middle" %><%= separator %><% } %>|` +
		`<%= if (secondEnabled) { %>` + selectionBlock("third") + `<% } %>`

	compareGenericVMParity(t, input, paritySlotContext(map[string]interface{}{
		"firstEnabled":  true,
		"secondEnabled": true,
	}), "Second|middle|Third")
}

func Test_Parity_Generic_VM_Many_Disjoint_Local_Names_Keep_Their_Slots(t *testing.T) {
	var input strings.Builder
	input.WriteString(forceGenericBytecode)
	input.WriteString(`<%= if (true) { %><% let selected = records[0] %>`)
	input.WriteString(`<%= for (_, candidate) in records { %><%= if (candidate.ID == "second") { %><% selected = candidate %><% } %><% } %>`)
	input.WriteString(`<%= selected.Label %><% } %>|`)

	var expected strings.Builder
	expected.WriteString("Second|")
	for i := 0; i < 24; i++ {
		name := fmt.Sprintf("later%02d", i)
		value := fmt.Sprintf("L%02d", i)
		fmt.Fprintf(&input, `<%%= if (true) { %%><%% let %s = "%s" %%><%%= %s %%><%% } %%>`, name, value, name)
		expected.WriteString(value)
	}

	compareGenericVMParity(t, input.String(), paritySlotContext(nil), expected.String())
}

func Test_Parity_Generic_VM_Active_Nested_Shadow_Restores_Outer_Local(t *testing.T) {
	input := forceGenericBytecode +
		`<%= if (true) { %>` +
		`<% let value = "outer" %><%= value %>|` +
		`<%= if (true) { %>` +
		`<% let value = "inner" %>` +
		`<%= for (_, suffix) in suffixes { %><% value = value + suffix %><% } %>` +
		`<%= value %>` +
		`<% } %>|<%= value %>` +
		`<% } %>`

	for _, tt := range []struct {
		name     string
		suffixes []string
		expected string
	}{
		{name: "captured assignment", suffixes: []string{"A", "B"}, expected: "outer|innerAB|outer"},
		{name: "no captured assignment", suffixes: nil, expected: "outer|inner|outer"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			compareGenericVMParity(t, input, paritySlotContext(map[string]interface{}{
				"suffixes": tt.suffixes,
			}), tt.expected)
		})
	}
}

func Test_Parity_Generic_VM_Sibling_Branch_Locals_Are_Isolated(t *testing.T) {
	input := forceGenericBytecode +
		`<%= if (branch == "first") { %>` +
		`<% let value = "A" %><%= for (_, suffix) in ["1"] { %><% value = value + suffix %><% } %><%= value %>` +
		`<% } else if (branch == "second") { %>` +
		`<% let value = "B" %><%= for (_, suffix) in ["2"] { %><% value = value + suffix %><% } %><%= value %>` +
		`<% } else { %>` +
		`<% let value = "C" %><%= for (_, suffix) in ["3"] { %><% value = value + suffix %><% } %><%= value %>` +
		`<% } %>`

	for _, tt := range []struct {
		branch   string
		expected string
	}{
		{branch: "first", expected: "A1"},
		{branch: "second", expected: "B2"},
		{branch: "other", expected: "C3"},
	} {
		t.Run(tt.branch, func(t *testing.T) {
			compareGenericVMParity(t, input, paritySlotContext(map[string]interface{}{
				"branch": tt.branch,
			}), tt.expected)
		})
	}
}

func Test_Parity_Generic_VM_Helper_Block_Preserves_Active_Disjoint_Local(t *testing.T) {
	input := forceGenericBytecode +
		`<%= if (true) { %>` +
		`<% let selected = records[0] %>` +
		`<%= wrap() { %>` +
		`<%= if (runLoop) { %><%= for (_, candidate) in records { %><%= if (candidate.ID == target) { %><% selected = candidate %><% } %><% } %><% } %>` +
		`<%= selected.Label %>` +
		`<% } %>|<%= selected.Label %>` +
		`<% } %>` +
		`<%= if (true) { %><% let later = "Later" %>|<%= later %><% } %>`

	for _, tt := range []struct {
		name       string
		runLoop    bool
		helperSets bool
		expected   string
	}{
		{name: "nested loop assignment", runLoop: true, expected: "[Second]|Second|Later"},
		{name: "helper context assignment", helperSets: true, expected: "[Third]|Third|Later"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			factory := paritySlotContext(map[string]interface{}{
				"target":  "second",
				"runLoop": tt.runLoop,
				"wrap": func(help rootplush.HelperContext) (template.HTML, error) {
					if tt.helperSets {
						help.Set("selected", paritySlotRecords()[2])
					}
					body, err := help.Block()
					return template.HTML("[" + body + "]"), err
				},
			})
			compareGenericVMParity(t, input, factory, tt.expected)
		})
	}
}

func Test_Parity_Generic_VM_Partial_Updates_Active_Disjoint_Local(t *testing.T) {
	input := forceGenericBytecode +
		`<%= if (true) { %>` +
		`<% let selected = records[0] %>` +
		`<%= partial("selection.plush.html") %>|<%= selected.Label %>` +
		`<% } %>` +
		`<%= if (true) { %><% let later = "Later" %>|<%= later %><% } %>`
	partial := `<%= for (_, candidate) in records { %>` +
		`<%= if (candidate.ID == target) { %><% selected = candidate %><% } %>` +
		`<% } %><%= selected.Label %>`

	factory := paritySlotContext(map[string]interface{}{
		"target": "second",
		"partialFeeder": func(name string) (string, error) {
			if name != "selection.plush.html" {
				return "", fmt.Errorf("unexpected partial %q", name)
			}
			return partial, nil
		},
	})
	compareGenericVMParity(t, input, factory, "Second|Second|Later")
}

func Test_Parity_Generic_VM_Cached_Template_Rerenders_Isolated_Locals(t *testing.T) {
	input := genericVMAlternatingBranchTemplate()
	tmpl, err := vmplush.Compile(input)
	require.NoError(t, err)

	for i, tt := range []struct {
		branch   string
		target   string
		expected string
	}{
		{branch: "left", target: "second", expected: "left:Second"},
		{branch: "right", target: "third", expected: "right:Third"},
		{branch: "left", target: "missing", expected: "left:First"},
		{branch: "right", target: "second", expected: "right:Second"},
	} {
		t.Run(fmt.Sprintf("render_%d", i), func(t *testing.T) {
			factory := paritySlotContext(map[string]interface{}{
				"branch": tt.branch,
				"target": tt.target,
			})
			interpreterOut, interpreterErr := rootplush.Render(input, factory())
			require.NoError(t, interpreterErr)
			require.Equal(t, tt.expected, interpreterOut)

			ctx := factory()
			vmOut, vmErr := tmpl.Render(ctx)
			require.NoError(t, vmErr)
			require.Equal(t, interpreterOut, vmOut)
			diagnostics, ok := rootplush.RenderDiagnosticsFromContext(ctx)
			require.True(t, ok)
			require.Equal(t, rootplush.RenderFastPathGeneric, diagnostics.FastPath)
		})
	}
}

func Test_Parity_Generic_VM_Concurrent_Template_Renders_Isolate_Locals(t *testing.T) {
	input := genericVMAlternatingBranchTemplate()
	tmpl, err := vmplush.Compile(input)
	require.NoError(t, err)

	type renderCase struct {
		branch   string
		target   string
		expected string
	}
	cases := []renderCase{
		{branch: "left", target: "second", expected: "left:Second"},
		{branch: "right", target: "third", expected: "right:Third"},
		{branch: "left", target: "missing", expected: "left:First"},
		{branch: "right", target: "second", expected: "right:Second"},
	}
	for _, testCase := range cases {
		out, renderErr := rootplush.Render(input, paritySlotContext(map[string]interface{}{
			"branch": testCase.branch,
			"target": testCase.target,
		})())
		require.NoError(t, renderErr)
		require.Equal(t, testCase.expected, out)
	}

	const workers = 32
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			testCase := cases[index%len(cases)]
			ctx := paritySlotContext(map[string]interface{}{
				"branch": testCase.branch,
				"target": testCase.target,
			})()
			out, renderErr := tmpl.Render(ctx)
			if renderErr != nil {
				errs <- renderErr
				return
			}
			if out != testCase.expected {
				errs <- fmt.Errorf("render %d: expected %q, got %q", index, testCase.expected, out)
				return
			}
			diagnostics, ok := rootplush.RenderDiagnosticsFromContext(ctx)
			if !ok || diagnostics.FastPath != rootplush.RenderFastPathGeneric {
				errs <- fmt.Errorf("render %d did not use generic VM", index)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for renderErr := range errs {
		require.NoError(t, renderErr)
	}
}

func Test_Parity_Generic_VM_Local_Index_Byte_Boundary(t *testing.T) {
	for _, maxIndex := range []int{255, 256} {
		t.Run(fmt.Sprintf("index_%d", maxIndex), func(t *testing.T) {
			input := genericVMLocalIndexBoundaryTemplate(maxIndex)
			compareGenericVMParity(t, input, paritySlotContext(nil), "updated")
		})
	}
}

func genericVMAlternatingBranchTemplate() string {
	selection := func(prefix string) string {
		return `<% let selected = records[0] %>` +
			`<%= for (_, candidate) in records { %><%= if (candidate.ID == target) { %><% selected = candidate %><% } %><% } %>` +
			prefix + `:<%= selected.Label %>`
	}
	return forceGenericBytecode +
		`<%= if (branch == "left") { %>` + selection("left") + `<% } %>` +
		`<%= if (true) { %><% let separator = "unused" %><% } %>` +
		`<%= if (branch == "right") { %>` + selection("right") + `<% } %>`
}

func genericVMLocalIndexBoundaryTemplate(maxIndex int) string {
	var input strings.Builder
	input.WriteString(forceGenericBytecode)
	input.WriteString(`<%= if (true) { %>`)
	for index := 0; index <= maxIndex; index++ {
		fmt.Fprintf(&input, `<%% let local_%03d = "initial" %%>`, index)
	}
	fmt.Fprintf(&input, `<%%= for (_, item) in ["updated"] { %%><%% local_%03d = item %%><%% } %%>`, maxIndex)
	fmt.Fprintf(&input, `<%%= local_%03d %%><%% } %%>`, maxIndex)
	return input.String()
}
