package vm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gobuffalo/plush/v5"
)

type outputSizeBenchmarkRecord struct {
	ID      int
	Name    string
	Content string
}

func Benchmark_VM_Output_Size_Estimator(b *testing.B) {
	const source = `<main><%= for (_, entry) in entries { %><article data-id="<%= entry.ID %>"><h2><%= entry.Name %></h2><p><%= entry.Content %></p></article><% } %></main>`

	workloads := []struct {
		name     string
		contexts []*plush.Context
	}{
		{
			name: "stable",
			contexts: []*plush.Context{
				outputSizeBenchmarkContext(1024, 128),
			},
		},
		{
			name: "alternating",
			contexts: []*plush.Context{
				outputSizeBenchmarkContext(64, 32),
				outputSizeBenchmarkContext(1024, 128),
			},
		},
	}

	for _, workload := range workloads {
		workload := workload
		b.Run(workload.name, func(b *testing.B) {
			for _, mode := range []struct {
				name    string
				enabled bool
			}{
				{name: "disabled"},
				{name: "enabled", enabled: true},
			} {
				mode := mode
				b.Run(mode.name, func(b *testing.B) {
					previous := plush.SetOutputSizeEstimatorEnabled(mode.enabled)
					defer plush.SetOutputSizeEstimatorEnabled(previous)

					tmpl, err := Compile(source)
					if err != nil {
						b.Fatal(err)
					}
					warmOutput, err := tmpl.Render(workload.contexts[0])
					if err != nil {
						b.Fatal(err)
					}
					if mode.enabled {
						for _, ctx := range workload.contexts {
							if _, err := tmpl.Render(ctx); err != nil {
								b.Fatal(err)
							}
						}
					}

					b.ReportAllocs()
					b.SetBytes(int64(len(warmOutput)))
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						out, err := tmpl.Render(workload.contexts[i%len(workload.contexts)])
						if err != nil {
							b.Fatal(err)
						}
						benchmarkSink = out
					}
				})
			}
		})
	}
}

func Benchmark_Heavy_Template_Render_Engine(b *testing.B) {
	const source = `<main><%= for (_, entry) in entries { %><article data-id="<%= entry.ID %>"><h2><%= entry.Name %></h2><p><%= entry.Content %></p></article><% } %></main>`

	ctx := outputSizeBenchmarkContext(1024, 128)
	interpreter, err := plush.NewTemplate(source)
	if err != nil {
		b.Fatal(err)
	}
	compiled, err := Compile(source)
	if err != nil {
		b.Fatal(err)
	}
	interpreterOutput, _, err := interpreter.Exec(ctx)
	if err != nil {
		b.Fatal(err)
	}
	previous := plush.SetOutputSizeEstimatorEnabled(true)
	defer plush.SetOutputSizeEstimatorEnabled(previous)
	vmOutput, err := compiled.Render(ctx)
	if err != nil {
		b.Fatal(err)
	}
	if interpreterOutput != vmOutput {
		b.Fatalf("render engines produced different output: interpreter=%d bytes vm=%d bytes", len(interpreterOutput), len(vmOutput))
	}

	b.Run("interpreter_parsed", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(interpreterOutput)))
		for i := 0; i < b.N; i++ {
			out, _, err := interpreter.Exec(ctx)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkSink = out
		}
	})

	b.Run("vm_compiled_estimator", func(b *testing.B) {
		if _, err := compiled.Render(ctx); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.SetBytes(int64(len(vmOutput)))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			out, err := compiled.Render(ctx)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkSink = out
		}
	})
}

func outputSizeBenchmarkContext(count, contentSize int) *plush.Context {
	records := make([]outputSizeBenchmarkRecord, count)
	for i := range records {
		records[i] = outputSizeBenchmarkRecord{
			ID:      i,
			Name:    fmt.Sprintf("entry-%d", i),
			Content: strings.Repeat("x", contentSize),
		}
	}
	return plush.NewContextWith(map[string]interface{}{"entries": records})
}
