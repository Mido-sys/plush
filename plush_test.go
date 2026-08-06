package plush_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/meta"
	"github.com/gobuffalo/plush/v5/templatecache/inmemory"
	"github.com/stretchr/testify/require"
)

func Test_Render_Simple_HTML(t *testing.T) {
	r := require.New(t)

	input := `<p>Hi</p>`
	s, err := plush.Render(input, plush.NewContext())
	r.NoError(err)
	r.Equal(input, s)
}

func Test_Render_Keeps_Spacing(t *testing.T) {
	r := require.New(t)
	input := `<%= greet %> <%= name %>`

	ctx := plush.NewContext()
	ctx.Set("greet", "hi")
	ctx.Set("name", "mark")

	s, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal("hi mark", s)
}

func Test_Render_HTML_Injected_String(t *testing.T) {
	r := require.New(t)

	input := `<p><%= "mark" %></p>`
	s, err := plush.Render(input, plush.NewContext())
	r.NoError(err)
	r.Equal("<p>mark</p>", s)
}

func Test_Render_Injected_Variable(t *testing.T) {
	r := require.New(t)

	input := `<p><%= name %></p>`
	s, err := plush.Render(input, plush.NewContextWith(map[string]interface{}{
		"name": "Mark",
	}))
	r.NoError(err)
	r.Equal("<p>Mark</p>", s)
}

func Test_Render_Missing_Variable(t *testing.T) {
	r := require.New(t)

	input := `<p><%= name %></p>`
	_, err := plush.Render(input, plush.NewContext())
	r.Error(err)
}

func Test_Render_Show_No_Show(t *testing.T) {
	r := require.New(t)
	input := `<%= "shown" %><% "notshown" %>`
	s, err := plush.Render(input, plush.NewContext())
	r.NoError(err)
	r.Equal("shown", s)
}

func Test_Render_Script_Function(t *testing.T) {
	r := require.New(t)

	input := `<% let add = fn(x) { return x + 2; }; %><%= add(2) %>`

	s, err := plush.Render(input, plush.NewContext())
	r.NoError(err)
	r.Equal("4", s)
}

func Test_Render_Mode_Default_Interpreter(t *testing.T) {
	r := require.New(t)
	previous := plush.SetRenderMode(plush.RenderModeInterpreter)
	defer plush.SetRenderMode(previous)

	s, err := plush.Render(`<%= "interpreter" %>`, plush.NewContext())
	r.NoError(err)
	r.Equal("interpreter", s)
}

func Test_Render_Diagnostics_Interpreter_Context(t *testing.T) {
	r := require.New(t)
	previous := plush.SetRenderMode(plush.RenderModeInterpreter)
	defer plush.SetRenderMode(previous)

	ctx := plush.NewContext()
	ctx.Set(meta.TemplateFileKey, "products/show.plush")
	ctx.Set("renderValue", func() string {
		time.Sleep(20 * time.Millisecond)
		return "interpreter"
	})

	s, err := plush.Render(`<%= renderValue() %>`, ctx)
	r.NoError(err)
	r.Equal("interpreter", s)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	r.True(ok)
	r.Equal(plush.RenderModeNameInterpreter, diagnostics.Mode)
	r.Equal("products/show.plush", diagnostics.TemplateFilename)
	r.Equal(plush.VMBytecodeCacheDisabled, diagnostics.VMBytecodeCache)
	r.NotZero(diagnostics.EngineDuration)
}

func Test_Render_Diagnostics_From_Data_After_Buffalo_Renderer(t *testing.T) {
	r := require.New(t)
	previous := plush.SetRenderMode(plush.RenderModeInterpreter)
	defer plush.SetRenderMode(previous)

	data := map[string]interface{}{
		meta.TemplateFileKey: "products/show.plush",
		"renderValue": func() string {
			time.Sleep(20 * time.Millisecond)
			return "interpreter"
		},
	}
	rendered, err := plush.BuffaloRenderer(`<%= renderValue() %>`, data, nil)
	r.NoError(err)
	r.Equal("interpreter", rendered)

	diagnostics, ok := plush.RenderDiagnosticsFromData(data)
	r.True(ok)
	r.Equal(plush.RenderModeNameInterpreter, diagnostics.Mode)
	r.Equal("products/show.plush", diagnostics.TemplateFilename)
	r.Equal(plush.VMBytecodeCacheDisabled, diagnostics.VMBytecodeCache)
	r.NotZero(diagnostics.EngineDuration)
}

func Test_Render_Diagnostics_Accumulates_Sequential_Template_Durations(t *testing.T) {
	r := require.New(t)
	previous := plush.SetRenderMode(plush.RenderModeInterpreter)
	defer plush.SetRenderMode(previous)

	ctx := plush.NewContext()
	ctx.Set(meta.TemplateFileKey, "products/show.plush")
	ctx.Set("renderBody", func() string {
		time.Sleep(20 * time.Millisecond)
		return "body"
	})
	ctx.Set("renderLayout", func() string {
		time.Sleep(40 * time.Millisecond)
		return "layout"
	})

	_, err := plush.Render(`<%= renderBody() %>`, ctx)
	r.NoError(err)
	first, ok := plush.RenderDiagnosticsFromContext(ctx)
	r.True(ok)
	r.Equal("products/show.plush", first.TemplateFilename)
	r.NotZero(first.EngineDuration)

	ctx.Set(meta.TemplateFileKey, "application.plush")
	_, err = plush.Render(`<%= renderLayout() %>`, ctx)
	r.NoError(err)
	second, ok := plush.RenderDiagnosticsFromContext(ctx)
	r.True(ok)
	r.Equal("products/show.plush", second.TemplateFilename)
	r.Greater(second.EngineDuration, first.EngineDuration)
}

func Test_Render_Interpreter_AST_Cache_Invalidates_When_Source_Changes(t *testing.T) {
	r := require.New(t)
	cache := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(cache)
	defer plush.ClearTemplateCache()

	previous := plush.SetRenderMode(plush.RenderModeInterpreter)
	defer plush.SetRenderMode(previous)

	ctx := plush.NewContext()
	ctx.Set(meta.TemplateFileKey, "interpreter-source-change.plush")

	out, err := plush.Render(`<%= "first" %>`, ctx)
	r.NoError(err)
	r.Equal("first", out)

	out, err = plush.Render(`<%= "second" %>`, ctx)
	r.NoError(err)
	r.Equal("second", out)
}

func Test_Buffalo_Renderer_With_Context_Configures_Context(t *testing.T) {
	r := require.New(t)
	previous := plush.SetRenderMode(plush.RenderModeInterpreter)
	defer plush.SetRenderMode(previous)

	configured := false
	data := map[string]interface{}{
		meta.TemplateFileKey: "products/show.plush",
	}
	rendered, err := plush.BuffaloRendererWithContext(`<%= marker %>`, data, nil, func(ctx *plush.Context) {
		configured = true
		ctx.Set("marker", "configured")
	})
	r.NoError(err)
	r.True(configured)
	r.Equal("configured", rendered)
	r.Equal("configured", data["marker"])

	diagnostics, ok := plush.RenderDiagnosticsFromData(data)
	r.True(ok)
	r.Equal(plush.RenderModeNameInterpreter, diagnostics.Mode)
}

func Test_Buffalo_Renderer_Copy_Back_Concurrent_Context_Keys(t *testing.T) {
	r := require.New(t)
	previous := plush.SetRenderMode(plush.RenderModeInterpreter)
	defer plush.SetRenderMode(previous)

	const workers = 32
	const iterations = 64
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	start := make(chan struct{})

	for worker := 0; worker < workers; worker++ {
		worker := worker
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				marker := fmt.Sprintf("buffalo_concurrent_marker_%d_%d", worker, i)
				helper := fmt.Sprintf("buffalo_concurrent_helper_%d_%d", worker, i)
				data := map[string]interface{}{
					"name": marker,
				}
				helpers := map[string]interface{}{
					helper: func() string {
						return "helper"
					},
				}

				rendered, err := plush.BuffaloRendererWithContext(`<% contentFor("name") { %>MD<% } %><%= name %>`, data, helpers, func(ctx *plush.Context) {
					ctx.Set(marker, "configured")
				})
				if err != nil {
					errs <- err
					return
				}
				if rendered != marker {
					errs <- fmt.Errorf("rendered %q, expected %q", rendered, marker)
					return
				}
				if _, ok := data["contentFor:name"]; !ok {
					errs <- fmt.Errorf("missing contentFor copy-back for %s", marker)
					return
				}
				if data[marker] != "configured" {
					errs <- fmt.Errorf("missing configured copy-back for %s", marker)
					return
				}
				if _, ok := data[helper]; !ok {
					errs <- fmt.Errorf("missing helper copy-back for %s", helper)
					return
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		r.NoError(err)
	}
}

func Test_Render_Diagnostics_VM_Hotspots_Header(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	plush.EnableRenderVMHotspotDiagnostics(ctx)

	plush.AddRenderDiagnosticVMHelperTiming(ctx, "slow:helper", 3*time.Millisecond)
	plush.AddRenderDiagnosticVMHelperTiming(ctx, "slow:helper", 2*time.Millisecond)
	plush.AddRenderDiagnosticVMHelperTiming(ctx, "fast", time.Millisecond)
	plush.AddRenderDiagnosticVMPartialTiming(ctx, "row,card", 4*time.Millisecond)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	r.True(ok)
	r.Equal(3, diagnostics.VMHotspots.HelperCalls)
	r.Equal(1, diagnostics.VMHotspots.PartialCalls)
	r.InDelta(6.0, diagnostics.VMHelperDurationMilliseconds(), 0.001)
	r.InDelta(4.0, diagnostics.VMPartialDurationMilliseconds(), 0.001)
	r.Equal("slow_helper:2:5.000;fast:1:1.000", diagnostics.VMHelperHotspotsHeader())
	r.Equal("row_card:1:4.000", diagnostics.VMPartialHotspotsHeader())
}

func Test_Render_Diagnostics_VM_Helper_Call_Paths(t *testing.T) {
	ctx := plush.NewContext()
	plush.EnableRenderVMHotspotDiagnostics(ctx)

	plush.AddRenderDiagnosticVMHelperCall(ctx, "format:value", "func(string) string", plush.RenderVMHelperCallDirect, 2*time.Millisecond)
	plush.AddRenderDiagnosticVMHelperCall(ctx, "format:value", "func(string) string", plush.RenderVMHelperCallDirect, time.Millisecond)
	plush.AddRenderDiagnosticVMHelperCall(ctx, "custom,value", "func(plush_test.namedValue) string", plush.RenderVMHelperCallReflection, 4*time.Millisecond)
	plush.AddRenderDiagnosticVMHelperTiming(ctx, "legacy", time.Millisecond)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, 4, diagnostics.VMHotspots.HelperCalls)
	require.Equal(t, 2, diagnostics.VMHotspots.HelperDirectCalls)
	require.Equal(t, 1, diagnostics.VMHotspots.HelperReflectionCalls)
	require.InDelta(t, 3.0, diagnostics.VMHelperDirectDurationMilliseconds(), 0.001)
	require.InDelta(t, 4.0, diagnostics.VMHelperReflectionDurationMilliseconds(), 0.001)
	require.InDelta(t, 25.0, diagnostics.VMHelperReflectionPercent(), 0.001)
	require.Equal(t, "direct-calls=2;reflection-calls=1;unclassified-calls=1;reflection-percent=25.00;direct-time-ms=3.000;reflection-time-ms=4.000;direct-details-dropped=0;reflection-details-dropped=0", diagnostics.VMHelperCallPathsHeader())
	require.Equal(t, "path=reflection,name=custom_value,signature=func(plush_test.namedValue) string,calls=1,time-ms=4.000|path=direct,name=format:value,signature=func(string) string,calls=2,time-ms=3.000", diagnostics.VMHelperCallPathDetailsHeader())
}

func Test_Render_Diagnostics_VM_Helper_Call_Path_Details_Are_Bounded(t *testing.T) {
	ctx := plush.NewContext()
	plush.EnableRenderVMHotspotDiagnostics(ctx)

	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("direct-%d", i)
		plush.AddRenderDiagnosticVMHelperCall(ctx, name, "func() string", plush.RenderVMHelperCallDirect, time.Microsecond)
		name = fmt.Sprintf("reflection-%d", i)
		plush.AddRenderDiagnosticVMHelperCall(ctx, name, "func(custom) string", plush.RenderVMHelperCallReflection, time.Microsecond)
	}

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, 20, diagnostics.VMHotspots.HelperCalls)
	require.Equal(t, 10, diagnostics.VMHotspots.HelperDirectCalls)
	require.Equal(t, 10, diagnostics.VMHotspots.HelperReflectionCalls)
	require.Len(t, diagnostics.VMHotspots.HelperCallPaths, 16)
	require.Equal(t, 2, diagnostics.VMHotspots.HelperDirectDetailsDropped)
	require.Equal(t, 2, diagnostics.VMHotspots.HelperReflectionDetailsDropped)
}

func Test_Render_Diagnostics_Output_Size_Header(t *testing.T) {
	r := require.New(t)
	diagnostics := plush.RenderDiagnostics{
		OutputSize: plush.RenderOutputSizeDiagnostics{
			Available:      true,
			StaticSize:     10,
			FallbackHint:   20,
			GrowHint:       30,
			Headroom:       4,
			EstimateBefore: 25,
			Actual:         28,
			EstimateAfter:  27,
			SamplesBefore:  2,
			SamplesAfter:   3,
			Observed:       true,
		},
	}

	r.Equal("scope=template;static=10;fallback=20;hint=30;headroom=4;learned=25;actual=28;error=10.71;within-10=0;estimate=27;samples=3;observed=1;accuracy-valid=1;min=0;max=0;unstable=0;limited=0;grow-called=0;grow-allocated=0;cap-before=0;cap-after-grow=0;cap-final=0;unused-cap=0", diagnostics.OutputSizeHeader())
	r.Empty(plush.RenderDiagnostics{}.OutputSizeHeader())
}

func Test_Render_Diagnostics_Contextual_Output_Size_Header(t *testing.T) {
	diagnostics := plush.RenderDiagnostics{
		OutputSize: plush.RenderOutputSizeDiagnostics{
			Available:                  true,
			Scope:                      "file",
			Contextual:                 true,
			ProfileBand:                "0-4k",
			RefinedProfileBand:         "0-4k",
			YieldSize:                  100,
			YieldConsumed:              true,
			AccuracyValid:              true,
			OverheadPredictor:          "ratio",
			OverheadPredictorAfter:     "absolute",
			OverheadBefore:             20,
			OverheadAbsolute:           22,
			OverheadRatio:              20,
			OverheadAbsoluteErrorScore: 3.5,
			OverheadRatioErrorScore:    1.25,
			OverheadActual:             21,
			OverheadAfter:              21,
			StaticSize:                 10,
			FallbackHint:               115,
			GrowHint:                   120,
			Headroom:                   5,
			EstimateBefore:             120,
			Actual:                     121,
			EstimateAfter:              121,
			SamplesAfter:               2,
			Observed:                   true,
		},
	}

	require.Equal(t, "scope=file;profile=0-4k;refined-profile=0-4k;profile-depth=0;profile-children=0;profile-fallback=0;profile-fallback-min=0;yield=100;yield-consumed=1;accuracy-valid=1;predictor=ratio;predictor-after=absolute;overhead=20;overhead-absolute=22;overhead-ratio=20;absolute-error-score=3.50;ratio-error-score=1.25;overhead-actual=21;overhead-estimate=21;static=10;fallback=115;hint=120;headroom=5;learned=120;actual=121;error=0.83;within-10=1;estimate=121;samples=2;observed=1;min=0;max=0;unstable=0;limited=0;grow-called=0;grow-allocated=0;cap-before=0;cap-after-grow=0;cap-final=0;unused-cap=0", diagnostics.OutputSizeHeader())
}

func Test_Render_Diagnostics_Contextual_Output_Size_Excludes_Unconsumed_Yield_From_Accuracy(t *testing.T) {
	diagnostics := plush.RenderDiagnostics{
		OutputSize: plush.RenderOutputSizeDiagnostics{
			Available:          true,
			Scope:              "file",
			Contextual:         true,
			ProfileBand:        "4k-16k",
			RefinedProfileBand: "4k-16k",
			YieldSize:          11_307,
			EstimateBefore:     82_332,
			Actual:             7,
			Observed:           true,
		},
	}

	header := diagnostics.OutputSizeHeader()
	require.Contains(t, header, ";yield-consumed=0;accuracy-valid=0;")
	require.Contains(t, header, ";within-10=0;")
}

func Test_Render_Diagnostics_Partial_Output_Size_Headers(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()

	plush.AddRenderDiagnosticPartialOutput(ctx, "components/item.plush", 90, 100, 100, 95, 3)
	plush.AddRenderDiagnosticPartialOutput(ctx, "components/item.plush", 110, 110, 100, 105, 4)
	plush.AddRenderDiagnosticPartialOutput(ctx, "nav;main.plush", 40, 45, 50, 45, 2)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	r.True(ok)
	r.Equal("calls=3;learned=240;hint=255;actual=250;absolute-error=30;error=12.00;within-10=0;unstable=0;limited=0;grow-calls=0;grow-allocated=0", diagnostics.PartialOutputSizeHeader())
	r.Equal("name=components/item.plush,calls=2,learned=200,hint=210,actual=200,error=10.00,estimate=105,samples=4,min=0,max=0,unstable=0,limited=0,grow-calls=0,grow-allocated=0|name=nav main.plush,calls=1,learned=40,hint=45,actual=50,error=20.00,estimate=45,samples=2,min=0,max=0,unstable=0,limited=0,grow-calls=0,grow-allocated=0", diagnostics.PartialOutputSizeDetailsHeader())
	r.Empty(plush.RenderDiagnostics{}.PartialOutputSizeHeader())
	r.Empty(plush.RenderDiagnostics{}.PartialOutputSizeDetailsHeader())
}

func Test_Render_Diagnostics_VM_Hotspots_Default_Off(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()

	plush.AddRenderDiagnosticVMHelperTiming(ctx, "helper", time.Millisecond)
	plush.AddRenderDiagnosticVMPartialTiming(ctx, "partial", time.Millisecond)

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	r.False(ok)
	r.Zero(diagnostics.VMHotspots.HelperCalls)
	r.Zero(diagnostics.VMHotspots.PartialCalls)
}

func Test_Render_Diagnostics_VM_Hotspot_Recorder_Snapshots_Setting(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()

	disabled := plush.CaptureRenderVMHotspotDiagnostics(ctx)
	r.False(disabled.Enabled())

	plush.EnableRenderVMHotspotDiagnostics(ctx)
	disabled.AddHelperTiming("before-enable", time.Millisecond)

	enabled := plush.CaptureRenderVMHotspotDiagnostics(ctx)
	r.True(enabled.Enabled())
	plush.DisableRenderVMHotspotDiagnostics(ctx)
	enabled.AddHelperTiming("captured-helper", 2*time.Millisecond)
	enabled.AddPartialTiming("captured-partial", 3*time.Millisecond)
	plush.AddRenderDiagnosticVMHelperTiming(ctx, "after-disable", time.Millisecond)

	nextRender := plush.CaptureRenderVMHotspotDiagnostics(ctx)
	r.False(nextRender.Enabled())

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	r.True(ok)
	r.Equal(1, diagnostics.VMHotspots.HelperCalls)
	r.Equal(2*time.Millisecond, diagnostics.VMHotspots.HelperDuration)
	r.Equal(1, diagnostics.VMHotspots.PartialCalls)
	r.Equal(3*time.Millisecond, diagnostics.VMHotspots.PartialDuration)
	r.Contains(diagnostics.VMHelperHotspotsHeader(), "captured-helper:1:2.000")
	r.Contains(diagnostics.VMPartialHotspotsHeader(), "captured-partial:1:3.000")
}

var vmHotspotEnabledBenchmarkSink bool

func Benchmark_Render_Diagnostics_VM_Hotspot_Disabled_Check(b *testing.B) {
	ctx := plush.NewContext()
	recorder := plush.CaptureRenderVMHotspotDiagnostics(ctx)

	b.Run("context-lookup", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			vmHotspotEnabledBenchmarkSink = plush.RenderVMHotspotDiagnosticsEnabled(ctx)
		}
	})
	b.Run("render-snapshot", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			vmHotspotEnabledBenchmarkSink = recorder.Enabled()
		}
	})
}

func Benchmark_Render_Diagnostics_VM_Hotspot_Record(b *testing.B) {
	b.Run("context-lookup", func(b *testing.B) {
		ctx := plush.NewContext()
		plush.EnableRenderVMHotspotDiagnostics(ctx)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			plush.AddRenderDiagnosticVMHelperTiming(ctx, "helper", time.Nanosecond)
		}
	})
	b.Run("render-snapshot", func(b *testing.B) {
		ctx := plush.NewContext()
		plush.EnableRenderVMHotspotDiagnostics(ctx)
		recorder := plush.CaptureRenderVMHotspotDiagnostics(ctx)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			recorder.AddHelperTiming("helper", time.Nanosecond)
		}
	})
}

func Test_Output_Size_Estimator_Can_Be_Disabled_And_Restored(t *testing.T) {
	r := require.New(t)
	previous := plush.SetOutputSizeEstimatorEnabled(false)
	defer plush.SetOutputSizeEstimatorEnabled(previous)

	r.True(previous)
	r.False(plush.OutputSizeEstimatorEnabled())
	r.False(plush.SetOutputSizeEstimatorEnabled(true))
	r.True(plush.OutputSizeEstimatorEnabled())
}

func Test_Render_Diagnostics_VM_Hotspots_Concurrent_Update(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	plush.EnableRenderVMHotspotDiagnostics(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				plush.AddRenderDiagnosticVMHelperTiming(ctx, "helper", time.Microsecond)
				plush.AddRenderDiagnosticVMPartialTiming(ctx, "partial", time.Microsecond)
			}
		}()
	}
	wg.Wait()

	diagnostics, ok := plush.RenderDiagnosticsFromContext(ctx)
	r.True(ok)
	r.Equal(400, diagnostics.VMHotspots.HelperCalls)
	r.Equal(400, diagnostics.VMHotspots.PartialCalls)
}

func Test_Render_Mode_VM_Requires_Registered_Renderer(t *testing.T) {
	r := require.New(t)
	previous := plush.SetRenderMode(plush.RenderModeVM)
	defer plush.SetRenderMode(previous)

	_, err := plush.Render(`<%= "vm" %>`, plush.NewContext())
	r.ErrorIs(err, plush.ErrVMRendererNotRegistered)
}

func Test_Render_Has_Block(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContext()
	ctx.Set("blockCheck", func(help plush.HelperContext) string {
		if help.HasBlock() {
			s, _ := help.Block()
			return s
		}
		return "no block"
	})
	input := `<%= blockCheck() {return "block"} %>|<%= blockCheck() %>`
	s, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal("block|no block", s)
}

func Test_Render_Dash_In_Helper(t *testing.T) {
	r := require.New(t)
	ctx := plush.NewContextWith(map[string]interface{}{
		"my-helper": func() string {
			return "hello"
		},
	})
	s, err := plush.Render(`<%= my-helper() %>`, ctx)
	r.NoError(err)
	r.Equal("hello", s)
}

func Test_Buffalo_Renderer(t *testing.T) {
	r := require.New(t)
	input := `<%= foo() %><%= name %>`
	data := map[string]interface{}{
		"name": "Ringo",
	}
	helpers := map[string]interface{}{
		"foo": func() string {
			return "George"
		},
	}
	s, err := plush.BuffaloRenderer(input, data, helpers)
	r.NoError(err)
	r.Equal("GeorgeRingo", s)
}

func Test_Buffalo_Renderer_Nil_Data(t *testing.T) {
	r := require.New(t)
	input := `<%= foo() %>`
	helpers := map[string]interface{}{
		"foo": func() string {
			return "test"
		},
	}
	s, err := plush.BuffaloRenderer(input, nil, helpers)
	r.NoError(err)
	r.Equal("test", s)
}

func Test_Buffalo_Renderer_Data_Persistence(t *testing.T) {
	r := require.New(t)
	input := `<%= contentFor("name") { %>MD<% }  %>`
	data := map[string]interface{}{}
	s, err := plush.BuffaloRenderer(input, data, map[string]interface{}{})
	r.NoError(err)
	r.Empty(s)
	r.Contains(data, "contentFor:name")
}

func Test_Helper_Nil_Arg(t *testing.T) {
	r := require.New(t)
	input := `<%= foo(nil, "k") %><%= foo(one, "k") %>`
	ctx := plush.NewContextWith(map[string]interface{}{
		"one": map[string]string{
			"k": "test",
		},
		"foo": func(a map[string]string, b string) string {
			if a != nil {
				return a[b]
			}
			return ""
		},
	})
	s, err := plush.Render(input, ctx)
	r.NoError(err)
	r.Equal("test", s)
}

func Test_Undefined_Arg(t *testing.T) {
	r := require.New(t)
	input := `<%= foo(bar) %>`
	ctx := plush.NewContext()
	ctx.Set("foo", func(string) {})

	_, err := plush.Render(input, ctx)
	r.Error(err)
	r.Equal(`line 1: "bar": unknown identifier`, err.Error())
}

func Test_Caching(t *testing.T) {
	r := require.New(t)

	fileCacheName := "testing-123.plush"
	astCacheName := "ast:" + fileCacheName
	template, err := plush.NewTemplate("<%= \"AA\" %>")
	r.NoError(err)

	imC := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(imC)
	template.Input = ""
	template.IsCache = true
	imC.Set(astCacheName, template)

	tc, err := plush.Parse("<%= a %>", fileCacheName)
	r.NoError(err)
	r.NotEqual(tc, template)

	imC = nil
	tc, err = plush.Parse("<%= a %>")
	r.NoError(err)
	r.NotEqual(tc, template)
}

func Test_Caching_Empty_File_Name(t *testing.T) {
	r := require.New(t)

	fileCacheName := "testing 123"
	template, err := plush.NewTemplate("<%= \"AA\" %>")
	r.NoError(err)

	imC := inmemory.NewMemoryCache()
	plush.PlushCacheSetup(imC)
	imC.Set(fileCacheName, template)

	tc, err := plush.Parse("<%= a %>")
	r.NoError(err)
	r.NotEqual(tc, template)
}
