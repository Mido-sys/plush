package vm

import (
	"html/template"
	"strings"
	"testing"

	"github.com/gobuffalo/plush/v5"
)

var benchmarkSourcePartialOutput string

func BenchmarkFastWriterWriteSourcePartial(b *testing.B) {
	staticScript := strings.Repeat(`window.runtimeQueue.push({event:"view",enabled:true});`, 128)
	workloads := []struct {
		name            string
		inheritedSource string
		dataSource      string
	}{
		{
			name:            "mostly-static",
			inheritedSource: `<script>` + staticScript + `window.pageTitle="<%= pageTitle %>";</script>`,
			dataSource:      `<script>` + staticScript + `window.label="<%= label %>";</script>`,
		},
		{
			name:            "fast-loop",
			inheritedSource: `<script><%= for (_, line) in scriptLines { %><%= line %><% } %>window.pageTitle="<%= pageTitle %>";</script>`,
			dataSource:      `<script><%= for (_, line) in scriptLines { %><%= line %><% } %>window.label="<%= label %>";</script>`,
		},
	}

	for _, workload := range workloads {
		b.Run(workload.name, func(b *testing.B) {
			benchmarkSourcePartialWorkload(b, workload.inheritedSource, workload.dataSource)
		})
	}
}

func benchmarkSourcePartialWorkload(b *testing.B, inheritedSource, dataSource string) {
	b.Run("inherited-context", func(b *testing.B) {
		b.Run("nested-render", func(b *testing.B) {
			benchmarkRuntimeSourceHelper(b, `<%= runtimeFragment() %>`, inheritedSource, false, func(w FastWriter, _ FastArgs) error {
				rendered, err := Render(inheritedSource, w.Context())
				if err != nil {
					return err
				}
				w.WriteHTMLString(rendered)
				return nil
			})
		})
		b.Run("source-partial", func(b *testing.B) {
			benchmarkRuntimeSourceHelper(b, `<%= runtimeFragment() %>`, inheritedSource, false, func(w FastWriter, _ FastArgs) error {
				return w.WriteSourcePartial("runtime/inherited.plush", inheritedSource)
			})
		})
	})

	b.Run("data-map", func(b *testing.B) {
		b.Run("nested-render", func(b *testing.B) {
			benchmarkRuntimeSourceHelper(b, `<%= runtimeFragment(labelArg) %>`, dataSource, true, func(w FastWriter, args FastArgs) error {
				label, ok := args.String(0)
				if !ok || args.Len() != 1 {
					return ErrFastUnsupported
				}
				child := w.Context().New()
				child.Set("label", label)
				rendered, err := Render(dataSource, child)
				if err != nil {
					return err
				}
				w.WriteHTMLString(rendered)
				return nil
			})
		})
		b.Run("source-partial", func(b *testing.B) {
			benchmarkRuntimeSourceHelper(b, `<%= runtimeFragment(labelArg) %>`, dataSource, true, func(w FastWriter, args FastArgs) error {
				label, ok := args.String(0)
				if !ok || args.Len() != 1 {
					return ErrFastUnsupported
				}
				return w.WriteSourcePartial(
					"runtime/data.plush",
					dataSource,
					map[string]interface{}{"label": label},
				)
			})
		})
	})
}

func benchmarkRuntimeSourceHelper(b *testing.B, parentSource, partialSource string, withArg bool, helper FastHelperFunc) {
	b.Helper()
	tmpl, err := Compile(parentSource)
	if err != nil {
		b.Fatal(err)
	}
	scriptLines := make([]string, 128)
	for i := range scriptLines {
		scriptLines[i] = `window.runtimeQueue.push({event:"view",enabled:true});`
	}
	ctx := plush.NewContextWith(map[string]interface{}{
		"labelArg":    "benchmark",
		"pageTitle":   "Benchmark",
		"scriptLines": scriptLines,
	})
	if withArg {
		ctx.Set("runtimeFragment", func(string) template.HTML { return "fallback" })
	} else {
		ctx.Set("runtimeFragment", func() template.HTML { return "fallback" })
	}
	SetFastHelper(ctx, "runtimeFragment", helper)

	warm, err := tmpl.Render(ctx)
	if err != nil {
		b.Fatal(err)
	}
	if warm == "" || partialSource == "" {
		b.Fatal("benchmark rendered empty source")
	}
	b.SetBytes(int64(len(warm)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkSourcePartialOutput, err = tmpl.Render(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}
