package vm

import (
	"html/template"
	"strings"
	"sync/atomic"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/hctx"
	"github.com/gobuffalo/plush/v5/vm/compiler"
)

type outputSizeOptions struct {
	topLevel    bool
	partialName string
}

type outputSizeObservation struct {
	available      bool
	scope          string
	staticSize     int
	stats          *compiler.OutputSizeStats
	fallbackHint   int
	growHint       int
	estimateBefore int
	samplesBefore  uint64
	contextual     bool
	yieldSize      int
	overheadBefore int
	profileBand    string
	unstable       bool
	limited        bool
	minimum        int
	maximum        int
	growCalled     bool
	capacityBefore int
	capacityGrow   int
	capacityFinal  int
}

type fileOutputSizeScope struct {
	filename     string
	renderPass   uint64
	rootBytecode *compiler.Bytecode
	layoutPass   atomic.Uint64
}

const outputSizeScopeTemplate = "template"
const outputSizeScopeFile = "file"
const outputSizeScopePartial = "partial"
const contextualOutputOverheadGrowLimit = 4 << 20
const unstablePartialGrowLimit = 64 << 10
const inlinePartialParentGrowLimit = 64 << 10

var fileOutputSizeScopeKey = "__plush_vm_file_output_size_scope__"

func outputSizeStatsEnabled(ctx hctx.Context, options outputSizeOptions) bool {
	return plush.OutputSizeEstimatorEnabled() &&
		(options.topLevel || options.partialName != "") &&
		(ctx == nil || !plush.IsHoleRender(ctx))
}

func outputGrowHint(bytecode *compiler.Bytecode, fallback int, ctx hctx.Context, options outputSizeOptions) int {
	return beginOutputSizeObservation(bytecode, fallback, ctx, options).growHint
}

func beginOutputSizeObservation(bytecode *compiler.Bytecode, fallback int, ctx hctx.Context, options outputSizeOptions) outputSizeObservation {
	if fallback < 0 {
		fallback = 0
	}
	observation := outputSizeObservation{
		scope:        outputSizeScopeTemplate,
		fallbackHint: fallback,
		growHint:     fallback,
	}
	if !outputSizeStatsEnabled(ctx, options) || bytecode == nil {
		return observation
	}
	if options.partialName != "" {
		observation.scope = outputSizeScopePartial
		observation.stats = bytecode.PartialSizeStats
	} else {
		observation.stats = bytecode.OutputSizeStats
	}
	observation.staticSize = bytecode.StaticSize
	if options.partialName == "" {
		if scope := fileOutputScope(bytecode, ctx, options); scope != nil {
			filename := plush.PunchHoleTemplateFilename(ctx)
			if yieldSize, ok := fileOutputScopeLayoutYield(scope, filename, ctx); ok && scope.rootBytecode != nil {
				observation.scope = outputSizeScopeFile
				observation.stats, observation.profileBand = scope.rootBytecode.LayoutSizeProfile.Stats(yieldSize)
				if observation.stats == nil {
					observation.stats = scope.rootBytecode.LayoutSizeStats
				}
				observation.contextual = true
				observation.yieldSize = yieldSize
			}
		}
	}
	if observation.stats == nil {
		return observation
	}
	observation.available = true
	observation.estimateBefore, observation.samplesBefore = outputSizeEstimateAndSamples(observation.stats)
	observation.minimum, observation.maximum = observation.stats.Range()
	observation.unstable = observation.stats.Unstable()
	if observation.contextual {
		overheadHint := observation.stats.GrowHint(observation.staticSize)
		if observation.samplesBefore == 0 && fallback > overheadHint {
			overheadHint = fallback
		}
		observation.overheadBefore = observation.estimateBefore
		if observation.samplesBefore == 0 {
			observation.overheadBefore = overheadHint
		}
		if observation.unstable {
			capped := capUnstableTemplateGrow(overheadHint, observation.staticSize, observation.minimum)
			observation.limited = capped < overheadHint
			overheadHint = capped
		}
		observation.estimateBefore = addOutputSizes(observation.yieldSize, observation.overheadBefore)
		observation.fallbackHint = addOutputSizes(observation.yieldSize, fallback)
		observation.growHint = contextualOutputGrowHint(observation.yieldSize, overheadHint)
		recordOutputGrowHint(bytecode, observation, ctx, options)
		return observation
	}
	if observation.stats.Samples() > 0 {
		observation.growHint = observation.stats.GrowHint(observation.staticSize)
	} else if hint := observation.stats.GrowHint(observation.staticSize); hint > fallback {
		observation.growHint = hint
	}
	if options.partialName == "" && observation.unstable {
		capped := capUnstableTemplateGrow(observation.growHint, observation.staticSize, observation.minimum)
		observation.limited = capped < observation.growHint
		observation.growHint = capped
	}
	if options.partialName != "" && observation.unstable {
		capped := capUnstablePartialGrow(observation.growHint, observation.staticSize)
		observation.limited = capped < observation.growHint
		observation.growHint = capped
	}
	recordOutputGrowHint(bytecode, observation, ctx, options)
	return observation
}

func capUnstableTemplateGrow(hint, staticSize, minimum int) int {
	limit := minimum
	if staticSize > limit {
		limit = staticSize
	}
	if limit < 0 {
		limit = 0
	}
	if hint > limit {
		return limit
	}
	return hint
}
func fileOutputScope(bytecode *compiler.Bytecode, ctx hctx.Context, options outputSizeOptions) *fileOutputSizeScope {
	if !options.topLevel || bytecode == nil || ctx == nil || plush.IsHoleRender(ctx) {
		return nil
	}
	if scope, ok := ctx.Value(fileOutputSizeScopeKey).(*fileOutputSizeScope); ok && scope != nil {
		return scope
	}
	filename := plush.PunchHoleTemplateFilename(ctx)
	if filename == "" {
		return nil
	}
	pass, ok := plush.BuffaloRenderPassFromContext(ctx)
	if !ok || pass.TemplateFilename != filename {
		return nil
	}
	scope := &fileOutputSizeScope{
		filename:     filename,
		renderPass:   pass.ID,
		rootBytecode: bytecode,
	}
	ctx.Set(fileOutputSizeScopeKey, scope)
	return scope
}

func fileOutputScopeLayoutYield(scope *fileOutputSizeScope, filename string, ctx hctx.Context) (int, bool) {
	if scope == nil || filename == "" || filename == scope.filename {
		return 0, false
	}
	pass, ok := plush.BuffaloRenderPassFromContext(ctx)
	if !ok {
		return 0, false
	}
	if pass.ID == scope.renderPass || pass.TemplateFilename != filename {
		return 0, false
	}
	yieldSize, ok := buffaloYieldOutputSize(ctx.Value("yield"))
	if !ok {
		return 0, false
	}
	layoutPass := scope.layoutPass.Load()
	if layoutPass == 0 && scope.layoutPass.CompareAndSwap(0, pass.ID) {
		layoutPass = pass.ID
	} else if layoutPass == 0 {
		layoutPass = scope.layoutPass.Load()
	}
	return yieldSize, layoutPass == pass.ID
}

func buffaloYieldOutputSize(value interface{}) (int, bool) {
	switch value := value.(type) {
	case template.HTML:
		return len(value), true
	default:
		return 0, false
	}
}

func addOutputSizes(left, right int) int {
	if left < 0 {
		left = 0
	}
	if right < 0 {
		right = 0
	}
	maxInt := int(^uint(0) >> 1)
	if right > maxInt-left {
		return maxInt
	}
	return left + right
}

func contextualOutputGrowHint(yieldSize, overheadHint int) int {
	if overheadHint > contextualOutputOverheadGrowLimit {
		overheadHint = contextualOutputOverheadGrowLimit
	}
	return addOutputSizes(yieldSize, overheadHint)
}

func capUnstablePartialGrow(hint, staticSize int) int {
	limit := unstablePartialGrowLimit
	if staticSize > limit {
		limit = staticSize
	}
	if hint > limit {
		return limit
	}
	return hint
}

func growEmptyOutputBuilder(out *strings.Builder, hint int, observation *outputSizeObservation) {
	recordOutputBuilderBeforeGrow(out, observation)
	if out == nil || hint <= 0 || out.Len() != 0 {
		return
	}
	if observation != nil {
		observation.growCalled = true
	}
	out.Grow(hint)
	recordOutputBuilderAfterGrow(out, observation)
}

func growInlineOutputBuilder(out *strings.Builder, hint int, observation *outputSizeObservation) {
	recordOutputBuilderBeforeGrow(out, observation)
	if out == nil || hint <= 0 || out.Cap()-out.Len() >= hint {
		return
	}
	if out.Cap() >= inlinePartialParentGrowLimit {
		if observation != nil {
			observation.limited = true
		}
		return
	}
	if observation != nil {
		observation.growCalled = true
	}
	out.Grow(hint)
	recordOutputBuilderAfterGrow(out, observation)
}

func recordOutputBuilderBeforeGrow(out *strings.Builder, observation *outputSizeObservation) {
	if out == nil || observation == nil {
		return
	}
	observation.capacityBefore = out.Cap()
	observation.capacityGrow = out.Cap()
}

func recordOutputBuilderAfterGrow(out *strings.Builder, observation *outputSizeObservation) {
	if out == nil || observation == nil {
		return
	}
	observation.capacityGrow = out.Cap()
}

func recordOutputBuilderFinal(out *strings.Builder, observation *outputSizeObservation) {
	if out == nil || observation == nil {
		return
	}
	observation.capacityFinal = out.Cap()
}

func beginPartialOutputObservation(bytecode *compiler.Bytecode, name string, ctx hctx.Context) outputSizeObservation {
	return beginOutputSizeObservation(bytecode, 0, ctx, outputSizeOptions{partialName: name})
}

func observePartialOutput(bytecode *compiler.Bytecode, name string, ctx hctx.Context, size int, observation outputSizeObservation) {
	observeOutputSize(bytecode, ctx, outputSizeOptions{partialName: name}, size, observation)
}

func renderStaticOutput(bytecode *compiler.Bytecode, ctx hctx.Context, options outputSizeOptions) string {
	if bytecode == nil {
		return ""
	}
	observation := beginOutputSizeObservation(bytecode, len(bytecode.StaticOutput), ctx, options)
	observeOutputSize(bytecode, ctx, options, len(bytecode.StaticOutput), observation)
	return bytecode.StaticOutput
}

func growVMRootOutput(machine *VM, bytecode *compiler.Bytecode, ctx hctx.Context, options outputSizeOptions) outputSizeObservation {
	if machine == nil || len(machine.frames) == 0 || machine.frames[0] == nil {
		return outputSizeObservation{}
	}
	observation := beginOutputSizeObservation(bytecode, 0, ctx, options)
	growEmptyOutputBuilder(&machine.frames[0].output, observation.growHint, &observation)
	return observation
}

func recordVMRootOutputFinal(machine *VM, observation *outputSizeObservation) {
	if machine == nil || len(machine.frames) == 0 || machine.frames[0] == nil {
		return
	}
	recordOutputBuilderFinal(&machine.frames[0].output, observation)
}

func vmRootOutputLen(machine *VM) int {
	if machine == nil || len(machine.frames) == 0 || machine.frames[0] == nil {
		return 0
	}
	return machine.frames[0].output.Len()
}

func observeOutputBuilderSize(bytecode *compiler.Bytecode, ctx hctx.Context, options outputSizeOptions, out *strings.Builder, observation outputSizeObservation) {
	if out == nil {
		return
	}
	recordOutputBuilderFinal(out, &observation)
	observeOutputSize(bytecode, ctx, options, out.Len(), observation)
}

func observeOutputSize(bytecode *compiler.Bytecode, ctx hctx.Context, options outputSizeOptions, size int, observation outputSizeObservation) {
	if !observation.available || !outputSizeStatsEnabled(ctx, options) || bytecode == nil {
		return
	}
	observedSize := size
	if observation.contextual {
		observedSize -= observation.yieldSize
		if observedSize < 0 {
			observedSize = 0
		}
	}
	observation.stats.Observe(observedSize)
	recordOutputActual(bytecode, size, observation, ctx, options)
}

func recordOutputGrowHint(bytecode *compiler.Bytecode, observation outputSizeObservation, ctx hctx.Context, options outputSizeOptions) {
	if !options.topLevel || !outputSizeStatsEnabled(ctx, options) || bytecode == nil || ctx == nil {
		return
	}
	updateOutputSizeDiagnostics(ctx, observation, func(d *plush.RenderDiagnostics) {
		d.OutputSize = plush.RenderOutputSizeDiagnostics{
			Available:      true,
			Scope:          observation.scope,
			Contextual:     observation.contextual,
			ProfileBand:    observation.profileBand,
			YieldSize:      observation.yieldSize,
			OverheadBefore: observation.overheadBefore,
			StaticSize:     observation.staticSize,
			FallbackHint:   observation.fallbackHint,
			GrowHint:       observation.growHint,
			EstimateBefore: observation.estimateBefore,
			EstimateAfter:  observation.estimateBefore,
			SamplesBefore:  observation.samplesBefore,
			SamplesAfter:   observation.samplesBefore,
			Minimum:        observation.minimum,
			Maximum:        observation.maximum,
			Unstable:       observation.unstable,
			Limited:        observation.limited,
			GrowCalled:     observation.growCalled,
			CapacityBefore: observation.capacityBefore,
			CapacityGrow:   observation.capacityGrow,
			CapacityFinal:  observation.capacityFinal,
			GrowAllocated:  outputSpeculativeAllocated(observation),
		}
	})
}

func recordOutputActual(bytecode *compiler.Bytecode, actual int, observation outputSizeObservation, ctx hctx.Context, options outputSizeOptions) {
	if !outputSizeStatsEnabled(ctx, options) || bytecode == nil || ctx == nil {
		return
	}
	estimate, samples := outputSizeEstimateAndSamples(observation.stats)
	minimum, maximum := observation.stats.Range()
	unstable := observation.stats.Unstable()
	overheadActual := 0
	overheadAfter := 0
	if observation.contextual {
		overheadActual = actual - observation.yieldSize
		if overheadActual < 0 {
			overheadActual = 0
		}
		overheadAfter = estimate
		estimate = addOutputSizes(observation.yieldSize, estimate)
	}
	if options.partialName != "" {
		plush.AddRenderDiagnosticPartialOutputAllocation(
			ctx,
			options.partialName,
			observation.estimateBefore,
			observation.growHint,
			actual,
			estimate,
			samples,
			plush.RenderPartialOutputAllocation{
				GrowCalled:           observation.growCalled,
				SpeculativeAllocated: outputSpeculativeAllocated(observation),
				Unstable:             observation.stats.Unstable(),
				Limited:              observation.limited,
				Minimum:              minimum,
				Maximum:              maximum,
			},
		)
		return
	}
	updateOutputSizeDiagnostics(ctx, observation, func(d *plush.RenderDiagnostics) {
		d.OutputSize = plush.RenderOutputSizeDiagnostics{
			Available:      true,
			Scope:          observation.scope,
			Contextual:     observation.contextual,
			ProfileBand:    observation.profileBand,
			YieldSize:      observation.yieldSize,
			OverheadBefore: observation.overheadBefore,
			OverheadActual: overheadActual,
			OverheadAfter:  overheadAfter,
			StaticSize:     observation.staticSize,
			FallbackHint:   observation.fallbackHint,
			GrowHint:       observation.growHint,
			EstimateBefore: observation.estimateBefore,
			Actual:         actual,
			EstimateAfter:  estimate,
			SamplesBefore:  observation.samplesBefore,
			SamplesAfter:   samples,
			Observed:       true,
			Minimum:        minimum,
			Maximum:        maximum,
			Unstable:       unstable,
			Limited:        observation.limited,
			GrowCalled:     observation.growCalled,
			CapacityBefore: observation.capacityBefore,
			CapacityGrow:   observation.capacityGrow,
			CapacityFinal:  observation.capacityFinal,
			UnusedCapacity: outputUnusedCapacity(observation, actual),
			GrowAllocated:  outputSpeculativeAllocated(observation),
		}
	})
}

func outputSpeculativeAllocated(observation outputSizeObservation) int {
	allocated := observation.capacityGrow - observation.capacityBefore
	if allocated < 0 {
		return 0
	}
	return allocated
}

func outputUnusedCapacity(observation outputSizeObservation, actual int) int {
	unused := observation.capacityFinal - actual
	if unused < 0 {
		return 0
	}
	return unused
}

func updateOutputSizeDiagnostics(ctx hctx.Context, observation outputSizeObservation, update func(*plush.RenderDiagnostics)) {
	if observation.scope == outputSizeScopeFile {
		plush.UpdateRenderDiagnostics(ctx, update)
		return
	}
	plush.UpdateRenderDiagnosticsForTemplate(ctx, plush.PunchHoleTemplateFilename(ctx), update)
}

func outputSizeEstimateAndSamples(stats *compiler.OutputSizeStats) (int, uint64) {
	if stats == nil {
		return 0, 0
	}
	return stats.Estimate(), stats.Samples()
}
