package plush_test

import (
	"html/template"
	"strings"
	"sync"
	"testing"
	"time"

	rootplush "github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/meta"
	"github.com/gobuffalo/plush/v5/templatecache/inmemory"
	"github.com/gobuffalo/plush/v5/vm/compiler"
	_ "github.com/gobuffalo/plush/v5/vm/plush"
	"github.com/stretchr/testify/require"
)

func enableDetailedEstimatorDiagnostics(t *testing.T) {
	t.Helper()
	previous := rootplush.SetOutputSizeEstimatorDiagnosticsMode(rootplush.OutputSizeEstimatorDiagnosticsDetailed)
	t.Cleanup(func() {
		rootplush.SetOutputSizeEstimatorDiagnosticsMode(previous)
	})
}

func Test_Buffalo_Render_Pass_File_Output_Tracks_Layout_Not_Nested_Renders(t *testing.T) {
	enableDetailedEstimatorDiagnostics(t)
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previous := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previous)

	const pageFile = "templates/pages/detail.plush"
	const layoutFile = "templates/application.plush"
	pageSource := `<%= nested() %><%= name %>`
	layoutSource := `<html><%= nestedBuffalo() %><%= yield %></html>`

	renderRequest := func(name string) rootplush.RenderDiagnostics {
		data := map[string]interface{}{
			meta.TemplateFileKey: pageFile,
			"name":               name,
		}
		helpers := map[string]interface{}{
			"nested": func(help rootplush.HelperContext) (template.HTML, error) {
				help.Set(meta.TemplateFileKey, "templates/sections/nested.plush")
				defer help.Set(meta.TemplateFileKey, pageFile)
				rendered, err := rootplush.Render(`<section>N</section>`, help.Context)
				return template.HTML(rendered), err
			},
		}
		helpers["nestedBuffalo"] = func() (template.HTML, error) {
			originalFilename := data[meta.TemplateFileKey]
			data[meta.TemplateFileKey] = "templates/sections/nested-buffalo.plush"
			defer func() {
				data[meta.TemplateFileKey] = originalFilename
			}()
			rendered, err := rootplush.BuffaloRenderer(`<aside>B</aside>`, data, helpers)
			return template.HTML(rendered), err
		}
		body, err := rootplush.BuffaloRenderer(pageSource, data, helpers)
		require.NoError(t, err)

		data["yield"] = template.HTML(body)
		data[meta.TemplateFileKey] = layoutFile
		rendered, err := rootplush.BuffaloRenderer(layoutSource, data, helpers)
		require.NoError(t, err)
		require.Equal(t, "<html><aside>B</aside><section>N</section>"+name+"</html>", rendered)

		diagnostics, ok := rootplush.RenderDiagnosticsFromData(data)
		require.True(t, ok)
		return diagnostics
	}

	first := renderRequest("Fry")
	firstYield := "<section>N</section>Fry"
	firstRendered := "<html><aside>B</aside>" + firstYield + "</html>"
	require.Equal(t, "file", first.OutputSize.Scope)
	require.True(t, first.OutputSize.Contextual)
	require.Equal(t, len(firstYield), first.OutputSize.YieldSize)
	require.Equal(t, len(firstRendered), first.OutputSize.Actual)
	require.Equal(t, len(firstRendered)-len(firstYield), first.OutputSize.OverheadActual)
	require.Equal(t, first.OutputSize.OverheadActual, first.OutputSize.OverheadAfter)
	require.Equal(t, uint64(1), first.OutputSize.SamplesAfter)

	second := renderRequest("Fry")
	require.Equal(t, "file", second.OutputSize.Scope)
	require.Equal(t, second.OutputSize.Actual, second.OutputSize.EstimateBefore)
	require.Equal(t, second.OutputSize.OverheadActual, second.OutputSize.OverheadBefore)
	require.Equal(t, uint64(2), second.OutputSize.SamplesAfter)

	third := renderRequest("Philip J. Fry")
	require.Greater(t, third.OutputSize.YieldSize, second.OutputSize.YieldSize)
	require.Equal(t, third.OutputSize.Actual, third.OutputSize.EstimateBefore)
	require.Equal(t, third.OutputSize.OverheadActual, third.OutputSize.OverheadBefore)
	require.Equal(t, uint64(3), third.OutputSize.SamplesAfter)
}

func Test_Buffalo_Render_Pass_File_Output_Isolates_Yield_Size_Bands(t *testing.T) {
	enableDetailedEstimatorDiagnostics(t)
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previous := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previous)

	const pageFile = "templates/pages/detail.plush"
	const pageSource = `<%= body %>`
	const layoutFile = "templates/application.plush"
	const layoutSource = `<html><%= yield %></html>`

	renderRequest := func(body string) rootplush.RenderDiagnostics {
		data := map[string]interface{}{
			meta.TemplateFileKey: pageFile,
			"body":               body,
		}
		bodyOutput, err := rootplush.BuffaloRenderer(pageSource, data, nil)
		require.NoError(t, err)
		data["yield"] = template.HTML(bodyOutput)
		data[meta.TemplateFileKey] = layoutFile
		_, err = rootplush.BuffaloRenderer(layoutSource, data, nil)
		require.NoError(t, err)
		diagnostics, ok := rootplush.RenderDiagnosticsFromData(data)
		require.True(t, ok)
		return diagnostics
	}

	smallBody := strings.Repeat("s", 31_694)
	mediumBody := strings.Repeat("m", 35_179)
	smallFirst := renderRequest(smallBody)
	mediumFirst := renderRequest(mediumBody)
	smallSecond := renderRequest(smallBody)

	require.Equal(t, "16k-32k", smallFirst.OutputSize.ProfileBand)
	require.Zero(t, smallFirst.OutputSize.SamplesBefore)
	require.Equal(t, "32k-64k", mediumFirst.OutputSize.ProfileBand)
	require.Zero(t, mediumFirst.OutputSize.SamplesBefore)
	require.Equal(t, "16k-32k", smallSecond.OutputSize.ProfileBand)
	require.Equal(t, uint64(1), smallSecond.OutputSize.SamplesBefore)
	require.Equal(t, smallSecond.OutputSize.Actual, smallSecond.OutputSize.EstimateBefore)
}

func Test_Buffalo_Render_Pass_File_Output_Refines_Unstable_Band(t *testing.T) {
	enableDetailedEstimatorDiagnostics(t)
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previousMode := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previousMode)
	previousEstimator := rootplush.SetOutputSizeEstimatorEnabled(true)
	defer rootplush.SetOutputSizeEstimatorEnabled(previousEstimator)

	const pageFile = "templates/pages/adaptive-layout.plush"
	const pageSource = `<%= body %>`
	const layoutFile = "templates/adaptive-layout.plush"
	const layoutSource = `<html><%= chrome %><%= yield %></html>`

	renderRequest := func(bodySize, chromeSize int) rootplush.RenderOutputSizeDiagnostics {
		data := map[string]interface{}{
			meta.TemplateFileKey: pageFile,
			"body":               strings.Repeat("b", bodySize),
			"chrome":             strings.Repeat("c", chromeSize),
		}
		body, err := rootplush.BuffaloRenderer(pageSource, data, nil)
		require.NoError(t, err)
		data["yield"] = template.HTML(body)
		data[meta.TemplateFileKey] = layoutFile
		_, err = rootplush.BuffaloRenderer(layoutSource, data, nil)
		require.NoError(t, err)
		diagnostics, ok := rootplush.RenderDiagnosticsFromData(data)
		require.True(t, ok)
		return diagnostics.OutputSize
	}

	for i := 0; i < 32; i++ {
		bodySize, chromeSize := 20<<10, 8<<10
		if i%2 == 1 {
			bodySize, chromeSize = 28<<10, 64<<10
		}
		renderRequest(bodySize, chromeSize)
	}

	firstChild := renderRequest(20<<10, 8<<10)
	require.Equal(t, "16k-32k", firstChild.ProfileBand)
	require.Equal(t, "16k-24k", firstChild.RefinedProfileBand)
	require.Equal(t, 1, firstChild.ProfileDepth)
	require.Equal(t, 2, firstChild.ProfileChildren)
	require.True(t, firstChild.ProfileFallback)
	require.Positive(t, firstChild.ProfileFallbackMinimum)
	require.Zero(t, firstChild.SamplesBefore)
	require.True(t, firstChild.YieldConsumed)
	require.True(t, firstChild.AccuracyValid)
	require.Equal(t, firstChild.Actual, firstChild.GrowHint)

	for i := 0; i < 3; i++ {
		renderRequest(20<<10, 8<<10)
	}
	warmedChild := renderRequest(20<<10, 8<<10)
	require.Equal(t, uint64(4), warmedChild.SamplesBefore)
	require.False(t, warmedChild.ProfileFallback)
	require.Equal(t, warmedChild.Actual, warmedChild.EstimateBefore)
}

func Test_Buffalo_Render_Pass_File_Output_Does_Not_Learn_Unconsumed_Yield(t *testing.T) {
	enableDetailedEstimatorDiagnostics(t)
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previousMode := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previousMode)
	previousEstimator := rootplush.SetOutputSizeEstimatorEnabled(true)
	defer rootplush.SetOutputSizeEstimatorEnabled(previousEstimator)

	const pageFile = "templates/pages/conditional-layout.plush"
	data := map[string]interface{}{
		meta.TemplateFileKey: pageFile,
		"body":               strings.Repeat("b", 12<<10),
	}
	body, err := rootplush.BuffaloRenderer(`<%= body %>`, data, nil)
	require.NoError(t, err)
	data["yield"] = template.HTML(body)
	data["showYield"] = false
	data[meta.TemplateFileKey] = "templates/conditional-layout.plush"
	rendered, err := rootplush.BuffaloRenderer(`<%= if (showYield) { %><%= yield %><% } %>x`, data, nil)
	require.NoError(t, err)
	require.Equal(t, "x", rendered)

	diagnostics, ok := rootplush.RenderDiagnosticsFromData(data)
	require.True(t, ok)
	require.Equal(t, len(body), diagnostics.OutputSize.YieldSize)
	require.False(t, diagnostics.OutputSize.YieldConsumed)
	require.False(t, diagnostics.OutputSize.AccuracyValid)
	require.Zero(t, diagnostics.OutputSize.SamplesAfter)

	entry, ok := cache.Get(rootplush.GenerateASTKey(pageFile))
	require.True(t, ok)
	bytecode, ok := entry.VMBytecode.(*compiler.Bytecode)
	require.True(t, ok)
	stats, _, _ := bytecode.LayoutSizeProfile.Predict(len(body))
	require.Zero(t, stats.Samples())
}

func Test_Buffalo_Render_Pass_File_Output_Selects_Proportional_Layout_Predictor(t *testing.T) {
	enableDetailedEstimatorDiagnostics(t)
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previousMode := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previousMode)
	previousEstimator := rootplush.SetOutputSizeEstimatorEnabled(true)
	defer rootplush.SetOutputSizeEstimatorEnabled(previousEstimator)

	const pageFile = "templates/pages/proportional.plush"
	const pageSource = `<%= body %>`
	const layoutFile = "templates/proportional-layout.plush"
	const layoutSource = `<%= chrome %><%= yield %>`

	renderRequest := func(size int) rootplush.RenderOutputSizeDiagnostics {
		data := map[string]interface{}{
			meta.TemplateFileKey: pageFile,
			"body":               strings.Repeat("b", size),
			"chrome":             strings.Repeat("c", size),
		}
		body, err := rootplush.BuffaloRenderer(pageSource, data, nil)
		require.NoError(t, err)
		data["yield"] = template.HTML(body)
		data[meta.TemplateFileKey] = layoutFile
		_, err = rootplush.BuffaloRenderer(layoutSource, data, nil)
		require.NoError(t, err)
		diagnostics, ok := rootplush.RenderDiagnosticsFromData(data)
		require.True(t, ok)
		return diagnostics.OutputSize
	}

	first := renderRequest(100)
	require.Equal(t, compiler.LayoutOutputPredictorAbsolute, first.OverheadPredictor)
	require.Equal(t, 100, first.OverheadActual)

	second := renderRequest(200)
	require.Equal(t, compiler.LayoutOutputPredictorAbsolute, second.OverheadPredictor)
	require.Equal(t, 100, second.OverheadAbsolute)
	require.Equal(t, 200, second.OverheadRatio)

	third := renderRequest(300)
	require.Equal(t, compiler.LayoutOutputPredictorRatio, third.OverheadPredictor)
	require.Equal(t, 150, third.OverheadAbsolute)
	require.Equal(t, 300, third.OverheadRatio)
	require.Equal(t, 300, third.OverheadBefore)
	require.Equal(t, third.Actual, third.EstimateBefore)
	require.Equal(t, float64(25), third.OverheadAbsoluteErrorScore)
	require.Zero(t, third.OverheadRatioErrorScore)
}

func Test_Buffalo_Render_Pass_File_Output_Does_Not_Learn_When_Estimator_Is_Disabled(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previousMode := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previousMode)
	previousEstimator := rootplush.SetOutputSizeEstimatorEnabled(false)
	defer rootplush.SetOutputSizeEstimatorEnabled(previousEstimator)

	const pageFile = "templates/pages/disabled-estimator.plush"
	data := map[string]interface{}{
		meta.TemplateFileKey: pageFile,
		"body":               strings.Repeat("b", 100),
		"chrome":             strings.Repeat("c", 100),
	}
	body, err := rootplush.BuffaloRenderer(`<%= body %>`, data, nil)
	require.NoError(t, err)
	data["yield"] = template.HTML(body)
	data[meta.TemplateFileKey] = "templates/disabled-layout.plush"
	_, err = rootplush.BuffaloRenderer(`<%= chrome %><%= yield %>`, data, nil)
	require.NoError(t, err)

	diagnostics, ok := rootplush.RenderDiagnosticsFromData(data)
	require.True(t, ok)
	require.False(t, diagnostics.OutputSize.Available)
	entry, ok := cache.Get(rootplush.GenerateASTKey(pageFile))
	require.True(t, ok)
	bytecode, ok := entry.VMBytecode.(*compiler.Bytecode)
	require.True(t, ok)
	stats, prediction, _ := bytecode.LayoutSizeProfile.Predict(len(body))
	require.Zero(t, stats.Samples())
	require.Zero(t, prediction.Ratio)
}

func Test_Buffalo_Render_Pass_Root_Layout_Overhead_Stats_Are_Concurrent(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previous := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previous)

	const layoutFile = "templates/application.plush"
	const pageSource = `<%= body %>`
	const layoutSource = `<html><%= chrome %><%= yield %></html>`

	renderRequest := func(pageFile, body, chrome string) error {
		data := map[string]interface{}{
			meta.TemplateFileKey: pageFile,
			"body":               body,
			"chrome":             chrome,
		}
		bodyOutput, err := rootplush.BuffaloRenderer(pageSource, data, nil)
		if err != nil {
			return err
		}
		data["yield"] = template.HTML(bodyOutput)
		data[meta.TemplateFileKey] = layoutFile
		_, err = rootplush.BuffaloRenderer(layoutSource, data, nil)
		return err
	}

	pages := []struct {
		filename string
		body     string
		chrome   string
	}{
		{filename: "templates/pages/alpha.plush", body: "alpha", chrome: "ALPHA-CHROME"},
		{filename: "templates/pages/beta.plush", body: "beta", chrome: "BETA-CHROME"},
	}
	for _, page := range pages {
		require.NoError(t, renderRequest(page.filename, page.body, page.chrome))
	}

	const rendersPerRoot = 16
	errCh := make(chan error, len(pages)*rendersPerRoot)
	var wg sync.WaitGroup
	for _, page := range pages {
		for i := 0; i < rendersPerRoot; i++ {
			wg.Add(1)
			go func(filename, body, chrome string) {
				defer wg.Done()
				errCh <- renderRequest(filename, body, chrome)
			}(page.filename, page.body, page.chrome)
		}
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	for _, page := range pages {
		entry, ok := cache.Get(rootplush.GenerateASTKey(page.filename))
		require.True(t, ok)
		bytecode, ok := entry.VMBytecode.(*compiler.Bytecode)
		require.True(t, ok)
		stats, _ := bytecode.LayoutSizeProfile.Stats(len(page.body))
		require.Equal(t, uint64(rendersPerRoot+1), stats.Samples())
	}
}

func Test_Root_Render_Mode_VM_Uses_Registered_VM_Renderer(t *testing.T) {
	previous := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previous)

	out, err := rootplush.Render(`<%= "vm" %>`, rootplush.NewContext())
	require.NoError(t, err)
	require.Equal(t, "vm", out)
}

func Test_Root_Render_Mode_VM_Uses_Bytecode_Only_Cache(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previous := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previous)

	filename := "render-mode-vm.plush"
	ctx := rootplush.NewContext()
	ctx.Set(meta.TemplateFileKey, filename)

	out, err := rootplush.Render(`<%= "vm-cache" %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "vm-cache", out)

	entry, ok := cache.Get(rootplush.GenerateASTKey(filename))
	require.True(t, ok)
	require.NotNil(t, entry.VMBytecode)
	require.Nil(t, entry.Program)
	require.Empty(t, entry.Input)
}

func Test_Buffalo_Renderer_From_Trusted_Bytecode_Cache(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previousMode := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previousMode)

	const logicalFilename = "templates/page.plush"
	const physicalFilename = "/templates/client-7/page.plush.html"
	configure := func(ctx *rootplush.Context) {
		rootplush.EnableTrustedPartialBytecodeCache(ctx)
		rootplush.SetTrustedTopLevelBytecodeCacheFilename(ctx, physicalFilename)
	}
	helpers := map[string]interface{}{
		"currentTemplate": func(help rootplush.HelperContext) string {
			filename, _ := help.Value(meta.TemplateFileKey).(string)
			return filename
		},
	}

	firstData := map[string]interface{}{
		meta.TemplateFileKey: logicalFilename,
		"name":               "Mido",
	}
	first, err := rootplush.BuffaloRendererWithContext(`<%= currentTemplate() %>|<%= name %>`, firstData, helpers, configure)
	require.NoError(t, err)
	require.Equal(t, logicalFilename+"|Mido", first)
	_, hit, err := rootplush.BuffaloRendererFromTrustedBytecodeCache(physicalFilename, firstData, helpers, nil)
	require.NoError(t, err)
	require.False(t, hit)

	secondData := map[string]interface{}{
		meta.TemplateFileKey: logicalFilename,
		"name":               "Leela",
	}
	second, hit, err := rootplush.BuffaloRendererFromTrustedBytecodeCache(physicalFilename, secondData, helpers, func(ctx *rootplush.Context) {
		rootplush.EnableTrustedPartialBytecodeCache(ctx)
	})
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, logicalFilename+"|Leela", second)

	cache.Delete(rootplush.GenerateASTKey(physicalFilename))
	_, hit, err = rootplush.BuffaloRendererFromTrustedBytecodeCache(physicalFilename, secondData, helpers, func(ctx *rootplush.Context) {
		rootplush.EnableTrustedPartialBytecodeCache(ctx)
	})
	require.NoError(t, err)
	require.False(t, hit)
}

func Test_Buffalo_Trusted_Bytecode_Cache_Hit_Bypasses_Source_Load_Until_Invalidated(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	t.Cleanup(func() {
		rootplush.ClearTemplateCache()
		rootplush.PlushCacheSetup(nil)
	})

	previousMode := rootplush.SetRenderMode(rootplush.RenderModeVM)
	t.Cleanup(func() {
		rootplush.SetRenderMode(previousMode)
	})

	const physicalFilename = "/templates/client-7/trusted-page.plush.html"
	source := `<h1><%= name %></h1>`
	sourceLoads := 0
	renderFile := func(name string) (string, bool, error) {
		data := map[string]interface{}{
			meta.TemplateFileKey: "templates/trusted-page.plush",
			"name":               name,
		}
		configure := func(ctx *rootplush.Context) {
			rootplush.EnableTrustedPartialBytecodeCache(ctx)
		}
		if rendered, hit, err := rootplush.BuffaloRendererFromTrustedBytecodeCache(physicalFilename, data, nil, configure); hit || err != nil {
			return rendered, hit, err
		}

		sourceLoads++
		configureAndCache := func(ctx *rootplush.Context) {
			configure(ctx)
			rootplush.SetTrustedTopLevelBytecodeCacheFilename(ctx, physicalFilename)
		}
		rendered, err := rootplush.BuffaloRendererWithContext(source, data, nil, configureAndCache)
		return rendered, false, err
	}

	first, hit, err := renderFile("Mido")
	require.NoError(t, err)
	require.False(t, hit)
	require.Equal(t, "<h1>Mido</h1>", first)
	require.Equal(t, 1, sourceLoads)

	second, hit, err := renderFile("Leela")
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "<h1>Leela</h1>", second)
	require.Equal(t, 1, sourceLoads, "a trusted hit must not load template source")

	source = `<section><%= name %></section>`
	cache.Delete(rootplush.GenerateASTKey(physicalFilename))
	third, hit, err := renderFile("Fry")
	require.NoError(t, err)
	require.False(t, hit)
	require.Equal(t, "<section>Fry</section>", third)
	require.Equal(t, 2, sourceLoads, "explicit invalidation must force one source reload")

	fourth, hit, err := renderFile("Bender")
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "<section>Bender</section>", fourth)
	require.Equal(t, 2, sourceLoads)
}

func Test_Buffalo_Renderer_From_Trusted_Bytecode_Cache_Stays_Off_In_Interpreter_Mode(t *testing.T) {
	previousMode := rootplush.SetRenderMode(rootplush.RenderModeInterpreter)
	defer rootplush.SetRenderMode(previousMode)

	rendered, hit, err := rootplush.BuffaloRendererFromTrustedBytecodeCache("templates/page.plush", map[string]interface{}{}, nil, func(ctx *rootplush.Context) {
		rootplush.EnableTrustedPartialBytecodeCache(ctx)
	})
	require.NoError(t, err)
	require.False(t, hit)
	require.Empty(t, rendered)
}

func Test_Buffalo_Renderer_From_Trusted_Bytecode_Cache_Does_Not_Leak_Filename_Into_Holes(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previousMode := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previousMode)

	const filename = "/templates/client-7/hole-page.plush.html"
	configure := func(ctx *rootplush.Context) {
		rootplush.EnableTrustedPartialBytecodeCache(ctx)
		rootplush.SetTrustedTopLevelBytecodeCacheFilename(ctx, filename)
	}

	first, err := rootplush.BuffaloRendererWithContext(`A<%H name %>B`, map[string]interface{}{
		meta.TemplateFileKey: "templates/hole-page.plush",
		"name":               "Mido",
	}, nil, configure)
	require.NoError(t, err)
	require.Equal(t, "AMidoB", first)

	second, hit, err := rootplush.BuffaloRendererFromTrustedBytecodeCache(filename, map[string]interface{}{
		meta.TemplateFileKey: "templates/hole-page.plush",
		"name":               "Leela",
	}, nil, func(ctx *rootplush.Context) {
		rootplush.EnableTrustedPartialBytecodeCache(ctx)
	})
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "ALeelaB", second)

	third, hit, err := rootplush.BuffaloRendererFromTrustedBytecodeCache(filename, map[string]interface{}{
		meta.TemplateFileKey: "templates/hole-page.plush",
		"name":               "Fry",
	}, nil, func(ctx *rootplush.Context) {
		rootplush.EnableTrustedPartialBytecodeCache(ctx)
	})
	require.NoError(t, err)
	require.True(t, hit)
	require.Equal(t, "AFryB", third)

	_, ok := cache.Get(rootplush.GenerateASTKey(filename))
	require.True(t, ok)
}

func Test_Root_Render_Mode_VM_Bytecode_Cache_Invalidates_When_Source_Changes(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previous := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previous)

	filename := "render-mode-vm-source-change.plush"
	ctx := rootplush.NewContext()
	ctx.Set(meta.TemplateFileKey, filename)

	out, err := rootplush.Render(`<%= "first" %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "first", out)

	out, err = rootplush.Render(`<%= "second" %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "second", out)

	diagnostics, ok := rootplush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, rootplush.VMBytecodeCacheMissStore, diagnostics.VMBytecodeCache)
}

func Test_Root_Render_Mode_VM_Bytecode_Cache_Uses_Full_Template_Path(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previous := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previous)

	first := "/templates/client-1/index.plush"
	second := "/templates/client-2/index.plush"

	firstCtx := rootplush.NewContext()
	firstCtx.Set(meta.TemplateFileKey, first)
	out, err := rootplush.Render(`<%= "one" %>`, firstCtx)
	require.NoError(t, err)
	require.Equal(t, "one", out)

	secondCtx := rootplush.NewContext()
	secondCtx.Set(meta.TemplateFileKey, second)
	out, err = rootplush.Render(`<%= "two" %>`, secondCtx)
	require.NoError(t, err)
	require.Equal(t, "two", out)

	firstKey := rootplush.GenerateASTKey(first)
	secondKey := rootplush.GenerateASTKey(second)
	require.NotEqual(t, firstKey, secondKey)
	_, ok := cache.Get(firstKey)
	require.True(t, ok)
	_, ok = cache.Get(secondKey)
	require.True(t, ok)
}

func Test_Root_Render_Mode_VM_Punch_Hole_Cache_Invalidates_When_Source_Changes(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previous := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previous)

	filename := "render-mode-vm-hole-source-change.plush"
	ctx := rootplush.NewContext()
	ctx.Set(meta.TemplateFileKey, filename)

	out, err := rootplush.Render(`A<%H "hole" %>B`, ctx)
	require.NoError(t, err)
	require.Equal(t, "AholeB", out)

	out, err = rootplush.Render(`C<%H "hole" %>D`, ctx)
	require.NoError(t, err)
	require.Equal(t, "CholeD", out)
}

func Test_Root_Render_Mode_VM_Writes_Diagnostics_To_Context(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previous := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previous)

	filename := "render-mode-vm-diagnostics.plush"
	ctx := rootplush.NewContext()
	ctx.Set(meta.TemplateFileKey, filename)
	ctx.Set("name", "mido")
	ctx.Set("slow", func() string {
		time.Sleep(20 * time.Millisecond)
		return ""
	})

	out, err := rootplush.Render(`<%= slow() %><%= name %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "mido", out)

	diagnostics, ok := rootplush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, rootplush.RenderModeNameVM, diagnostics.Mode)
	require.Equal(t, filename, diagnostics.TemplateFilename)
	require.Equal(t, rootplush.VMBytecodeCacheMissStore, diagnostics.VMBytecodeCache)
	require.NotZero(t, diagnostics.EngineDuration)

	out, err = rootplush.Render(`<%= slow() %><%= name %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "mido", out)

	diagnostics, ok = rootplush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, rootplush.RenderModeNameVM, diagnostics.Mode)
	require.Equal(t, filename, diagnostics.TemplateFilename)
	require.Equal(t, rootplush.VMBytecodeCacheHit, diagnostics.VMBytecodeCache)
	require.NotZero(t, diagnostics.EngineDuration)
}

func Test_Root_Render_Mode_VM_Generic_Segment_Stays_In_VM(t *testing.T) {
	previousMode := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previousMode)
	previousFallback := rootplush.SetVMGenericFallback(true)
	defer rootplush.SetVMGenericFallback(previousFallback)

	ctx := rootplush.NewContext()
	out, err := rootplush.Render(`<% let forceBytecode = fn() { return "x" } %><% let title = "Records" %><%= title %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "Records", out)

	diagnostics, ok := rootplush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, rootplush.RenderModeNameVM, diagnostics.Mode)
	require.Equal(t, rootplush.RenderFastPathGeneric, diagnostics.FastPath)
}

func Test_Root_Render_Mode_VM_Generic_Segment_Keeps_Bytecode_Cache(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previousMode := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previousMode)
	previousFallback := rootplush.SetVMGenericFallback(true)
	defer rootplush.SetVMGenericFallback(previousFallback)

	filename := "render-mode-vm-generic-cache.plush"
	ctx := rootplush.NewContext()
	ctx.Set(meta.TemplateFileKey, filename)

	input := `<% let forceBytecode = fn() { return "x" } %><% let title = "Records" %><%= title %>`
	out, err := rootplush.Render(input, ctx)
	require.NoError(t, err)
	require.Equal(t, "Records", out)
	diagnostics, ok := rootplush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, rootplush.VMBytecodeCacheMissStore, diagnostics.VMBytecodeCache)

	out, err = rootplush.Render(input, ctx)
	require.NoError(t, err)
	require.Equal(t, "Records", out)
	diagnostics, ok = rootplush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, rootplush.VMBytecodeCacheHit, diagnostics.VMBytecodeCache)
	require.Equal(t, rootplush.RenderFastPathGeneric, diagnostics.FastPath)
}

func Test_Root_Render_Mode_VM_Generic_Segment_Renders_Partials_In_VM(t *testing.T) {
	previousMode := rootplush.SetRenderMode(rootplush.RenderModeVM)
	defer rootplush.SetRenderMode(previousMode)
	previousFallback := rootplush.SetVMGenericFallback(true)
	defer rootplush.SetVMGenericFallback(previousFallback)

	ctx := rootplush.NewContext()
	ctx.Set("partial", rootplush.PartialHelper)
	ctx.Set("partialFeeder", func(name string) (string, error) {
		require.Equal(t, "row.plush", name)
		return `<% let title = "Row" %><%= title %>`, nil
	})
	ctx.Set(meta.TemplateFileKey, "render-mode-vm-generic-partial.plush")

	out, err := rootplush.Render(`<% let forceBytecode = fn() { return "x" } %><%= partial("row.plush", {}) %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "Row", out)

	diagnostics, ok := rootplush.RenderDiagnosticsFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, rootplush.RenderFastPathGeneric, diagnostics.FastPath)
	require.Zero(t, diagnostics.VMHotspots.PartialCalls)
}

func Test_Root_Render_Mode_Interpreter_Still_Uses_AST_Cache(t *testing.T) {
	cache := inmemory.NewMemoryCache()
	rootplush.PlushCacheSetup(cache)
	defer rootplush.ClearTemplateCache()

	previous := rootplush.SetRenderMode(rootplush.RenderModeInterpreter)
	defer rootplush.SetRenderMode(previous)

	filename := "render-mode-interpreter.plush"
	ctx := rootplush.NewContext()
	ctx.Set(meta.TemplateFileKey, filename)

	out, err := rootplush.Render(`<%= "interpreter-cache" %>`, ctx)
	require.NoError(t, err)
	require.Equal(t, "interpreter-cache", out)

	entry, ok := cache.Get(rootplush.GenerateASTKey(filename))
	require.True(t, ok)
	require.NotNil(t, entry.Program)
	require.Nil(t, entry.VMBytecode)
	require.Empty(t, entry.Input)
}
