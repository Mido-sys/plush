package plush

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gobuffalo/plush/v5/helpers/hctx"
)

const (
	RenderModeNameInterpreter = "interpreter"
	RenderModeNameVM          = "vm"

	VMBytecodeCacheDisabled        = "disabled"
	VMBytecodeCacheHit             = "hit"
	VMBytecodeCacheHitStatic       = "hit-static"
	VMBytecodeCacheHitSource       = "hit-source"
	VMBytecodeCacheMiss            = "miss"
	VMBytecodeCacheMissStore       = "miss-store"
	VMBytecodeCacheMissStoreSource = "miss-store-source"
	VMBytecodeCacheDirect          = "compiled-template"

	RenderFastPathStatic              = "static"
	RenderFastPathFast                = "fast"
	RenderFastPathGeneric             = "generic"
	RenderFastPathInterpreterFallback = "interpreter-fallback"

	RenderPartialFallbackGenericBytecode        RenderPartialFallbackReason = "generic-bytecode"
	RenderPartialFallbackBlockCallCompatibility RenderPartialFallbackReason = "block-call-compatibility"
	RenderPartialFallbackInheritedInterpreter   RenderPartialFallbackReason = "inherited-interpreter"

	PunchHoleCacheDisabled = "disabled"
	PunchHoleCacheHit      = "hit"
	PunchHoleCacheMiss     = "miss"

	renderPartialOutputDetailLimit   = 8
	renderLoopOutputDetailLimit      = 8
	renderVMHelperPathDetailLimit    = 8
	renderPartialFallbackDetailLimit = 16
)

// RenderVMHelperCallPath identifies how the VM invoked a Go helper.
type RenderVMHelperCallPath string

const (
	// RenderVMHelperCallDirect is a statically typed Go invocation.
	RenderVMHelperCallDirect RenderVMHelperCallPath = "direct"
	// RenderVMHelperCallReflection is a reflect.Value.Call compatibility invocation.
	RenderVMHelperCallReflection RenderVMHelperCallPath = "reflection"
)

var renderDiagnosticsKey = "__plush_internal_render_diagnostics_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"
var renderVMHotspotDiagnosticsKey = "__plush_internal_render_vm_hotspot_diagnostics_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"
var renderDiagnosticsRootActiveKey = "__plush_internal_render_diagnostics_root_active_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"
var renderPartialFallbackDiagnosticsKey = "__plush_internal_render_partial_fallback_diagnostics_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"
var renderPartialFallbackActiveKey = "__plush_internal_render_partial_fallback_active_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"

// RenderPartialFallbackReason describes why a partial executed through the
// interpreter while the surrounding render was using the VM.
type RenderPartialFallbackReason string

type renderDiagnosticsState struct {
	mu          sync.Mutex
	diagnostics RenderDiagnostics
}

type RenderDiagnostics struct {
	Mode             string
	TemplateFilename string
	VMBytecodeCache  string
	FastPath         string
	FastRejectLine   int
	FastReject       string
	PunchHoleCache   string
	EngineDuration   time.Duration
	FastPlan         RenderFastPlanDiagnostics
	OutputSize       RenderOutputSizeDiagnostics
	LoopOutput       RenderLoopOutputSizeDiagnostics
	PartialOutput    RenderPartialOutputSizeDiagnostics
	VMHotspots       RenderVMHotspotDiagnostics
	PartialFallbacks RenderPartialFallbackDiagnostics
}

// RenderPartialFallbackDiagnostics aggregates VM partials that entered the
// interpreter. Details are bounded so diagnostics cannot grow with arbitrary
// dynamic partial names.
type RenderPartialFallbackDiagnostics struct {
	Calls          int
	DetailsDropped int
	Details        []RenderPartialFallbackDetail
}

// RenderPartialFallbackDetail aggregates one partial filename and fallback
// reason within a render.
type RenderPartialFallbackDetail struct {
	Name   string
	Reason RenderPartialFallbackReason
	Calls  int
}

type RenderOutputSizeDiagnostics struct {
	Available                  bool
	Scope                      string
	Contextual                 bool
	YieldSize                  int
	OverheadBefore             int
	OverheadActual             int
	OverheadAfter              int
	OverheadPredictor          string
	OverheadPredictorAfter     string
	OverheadAbsolute           int
	OverheadRatio              int
	OverheadAbsoluteErrorScore float64
	OverheadRatioErrorScore    float64
	StaticSize                 int
	FallbackHint               int
	GrowHint                   int
	Headroom                   int
	EstimateBefore             int
	Actual                     int
	EstimateAfter              int
	SamplesBefore              uint64
	SamplesAfter               uint64
	Observed                   bool
	ProfileBand                string
	RefinedProfileBand         string
	ProfileDepth               int
	ProfileChildren            int
	ProfileFallback            bool
	ProfileFallbackMinimum     int
	YieldConsumed              bool
	AccuracyValid              bool
	Minimum                    int
	Maximum                    int
	Unstable                   bool
	Limited                    bool
	GrowCalled                 bool
	CapacityBefore             int
	CapacityGrow               int
	CapacityFinal              int
	UnusedCapacity             int
	GrowAllocated              int
}

type RenderLoopOutputSizeDiagnostics struct {
	Calls         int
	Items         int
	KnownCount    int
	Learned       int
	GrowHint      int
	Actual        int
	AbsoluteError int
	WithinTen     int
	Limited       int
	GrowCalls     int
	GrowAllocated int
	Details       []RenderLoopOutputSizeDetail
}

type RenderLoopOutputSizeDetail struct {
	Name                 string
	Line                 int
	Calls                int
	Items                int
	Learned              int
	GrowHint             int
	Actual               int
	AbsoluteError        int
	WithinTen            int
	KnownCount           int
	LearnedBytesPerItem  int
	ActualBytesPerItem   int
	EstimateBytesPerItem int
	SamplesBefore        uint64
	SamplesAfter         uint64
	Limited              int
	GrowCalls            int
	GrowAllocated        int
}

type RenderPartialOutputSizeDiagnostics struct {
	Calls         int
	Learned       int
	GrowHint      int
	Actual        int
	AbsoluteError int
	WithinTen     int
	Unstable      int
	Limited       int
	GrowCalls     int
	GrowAllocated int
	Details       []RenderPartialOutputSizeDetail
}

type RenderPartialOutputSizeDetail struct {
	Name          string
	Calls         int
	Learned       int
	GrowHint      int
	Actual        int
	AbsoluteError int
	Estimate      int
	Samples       uint64
	Minimum       int
	Maximum       int
	Unstable      bool
	Limited       int
	GrowCalls     int
	GrowAllocated int
}

type RenderPartialOutputAllocation struct {
	GrowCalled           bool
	SpeculativeAllocated int
	Unstable             bool
	Limited              bool
	Minimum              int
	Maximum              int
}

type RenderFastPlanDiagnostics struct {
	Bindings       int
	Segments       int
	StaticSegments int
	NameSegments   int
	PropertyReads  int
	ValueWrites    int
	HelperCalls    int
	Conditionals   int
	Loops          int
	LoopParts      int
	Partials       int
	MaxDepth       int
	HelperNames    []string
	PartialNames   []string
}

type RenderVMHotspotDiagnostics struct {
	HelperCalls                    int
	HelperDuration                 time.Duration
	HelperDirectCalls              int
	HelperDirectDuration           time.Duration
	HelperReflectionCalls          int
	HelperReflectionDuration       time.Duration
	HelperDirectDetailsDropped     int
	HelperReflectionDetailsDropped int
	PartialCalls                   int
	PartialDuration                time.Duration
	Helpers                        []RenderVMHotspot
	Partials                       []RenderVMHotspot
	HelperCallPaths                []RenderVMHelperCallPathDiagnostics
}

type RenderVMHotspot struct {
	Name     string
	Calls    int
	Duration time.Duration
}

// RenderVMHelperCallPathDiagnostics aggregates one retained helper name,
// signature, and invocation-path combination.
type RenderVMHelperCallPathDiagnostics struct {
	Name      string
	Signature string
	Path      RenderVMHelperCallPath
	Calls     int
	Duration  time.Duration
}

// RenderVMHotspotDiagnosticsRecorder is a render-scoped snapshot of VM
// hotspot diagnostics. VM renderers capture it once and reuse it so disabled
// diagnostics do not require a context lookup for every helper or partial.
type RenderVMHotspotDiagnosticsRecorder struct {
	state    *renderDiagnosticsState
	captured bool
}

func (d RenderDiagnostics) EngineDurationMilliseconds() float64 {
	return float64(d.EngineDuration) / float64(time.Millisecond)
}

func (d RenderDiagnostics) VMHelperDurationMilliseconds() float64 {
	return float64(d.VMHotspots.HelperDuration) / float64(time.Millisecond)
}

func (d RenderDiagnostics) VMPartialDurationMilliseconds() float64 {
	return float64(d.VMHotspots.PartialDuration) / float64(time.Millisecond)
}

func (d RenderDiagnostics) VMHelperDirectDurationMilliseconds() float64 {
	return float64(d.VMHotspots.HelperDirectDuration) / float64(time.Millisecond)
}

func (d RenderDiagnostics) VMHelperReflectionDurationMilliseconds() float64 {
	return float64(d.VMHotspots.HelperReflectionDuration) / float64(time.Millisecond)
}

func (d RenderDiagnostics) VMHelperReflectionPercent() float64 {
	if d.VMHotspots.HelperCalls == 0 {
		return 0
	}
	return float64(d.VMHotspots.HelperReflectionCalls) * 100 / float64(d.VMHotspots.HelperCalls)
}

func (d RenderDiagnostics) FastPlanHelperNamesHeader() string {
	return strings.Join(d.FastPlan.HelperNames, ";")
}

func (d RenderDiagnostics) FastPlanPartialNamesHeader() string {
	return strings.Join(d.FastPlan.PartialNames, ";")
}

func (d RenderDiagnostics) OutputSizeHeader() string {
	if !d.OutputSize.Available {
		return ""
	}
	scope := d.OutputSize.Scope
	if scope == "" {
		scope = "template"
	}
	observed := 0
	withinTen := 0
	accuracyValid := d.OutputSize.Observed
	if d.OutputSize.Contextual {
		accuracyValid = accuracyValid && d.OutputSize.AccuracyValid
	}
	if d.OutputSize.Observed {
		observed = 1
		errorSize := d.OutputSize.EstimateBefore - d.OutputSize.Actual
		if errorSize < 0 {
			errorSize = -errorSize
		}
		if accuracyValid && outputSizeErrorPercent(errorSize, d.OutputSize.Actual) < 10 {
			withinTen = 1
		}
	}
	errorSize := d.OutputSize.EstimateBefore - d.OutputSize.Actual
	if errorSize < 0 {
		errorSize = -errorSize
	}
	header := fmt.Sprintf(
		"scope=%s;static=%d;fallback=%d;hint=%d;headroom=%d;learned=%d;actual=%d;error=%.2f;within-10=%d;estimate=%d;samples=%d;observed=%d;accuracy-valid=%d;min=%d;max=%d;unstable=%d;limited=%d;grow-called=%d;grow-allocated=%d;cap-before=%d;cap-after-grow=%d;cap-final=%d;unused-cap=%d",
		scope,
		d.OutputSize.StaticSize,
		d.OutputSize.FallbackHint,
		d.OutputSize.GrowHint,
		d.OutputSize.Headroom,
		d.OutputSize.EstimateBefore,
		d.OutputSize.Actual,
		outputSizeErrorPercent(errorSize, d.OutputSize.Actual),
		withinTen,
		d.OutputSize.EstimateAfter,
		d.OutputSize.SamplesAfter,
		observed,
		boolHeaderValue(accuracyValid),
		d.OutputSize.Minimum,
		d.OutputSize.Maximum,
		boolHeaderValue(d.OutputSize.Unstable),
		boolHeaderValue(d.OutputSize.Limited),
		boolHeaderValue(d.OutputSize.GrowCalled),
		d.OutputSize.GrowAllocated,
		d.OutputSize.CapacityBefore,
		d.OutputSize.CapacityGrow,
		d.OutputSize.CapacityFinal,
		d.OutputSize.UnusedCapacity,
	)
	if !d.OutputSize.Contextual {
		return header
	}
	return fmt.Sprintf(
		"scope=%s;profile=%s;refined-profile=%s;profile-depth=%d;profile-children=%d;profile-fallback=%d;profile-fallback-min=%d;yield=%d;yield-consumed=%d;accuracy-valid=%d;predictor=%s;predictor-after=%s;overhead=%d;overhead-absolute=%d;overhead-ratio=%d;absolute-error-score=%.2f;ratio-error-score=%.2f;overhead-actual=%d;overhead-estimate=%d;static=%d;fallback=%d;hint=%d;headroom=%d;learned=%d;actual=%d;error=%.2f;within-10=%d;estimate=%d;samples=%d;observed=%d;min=%d;max=%d;unstable=%d;limited=%d;grow-called=%d;grow-allocated=%d;cap-before=%d;cap-after-grow=%d;cap-final=%d;unused-cap=%d",
		scope,
		outputSizeProfileBand(d.OutputSize.ProfileBand),
		outputSizeProfileBand(d.OutputSize.RefinedProfileBand),
		d.OutputSize.ProfileDepth,
		d.OutputSize.ProfileChildren,
		boolHeaderValue(d.OutputSize.ProfileFallback),
		d.OutputSize.ProfileFallbackMinimum,
		d.OutputSize.YieldSize,
		boolHeaderValue(d.OutputSize.YieldConsumed),
		boolHeaderValue(accuracyValid),
		d.OutputSize.OverheadPredictor,
		d.OutputSize.OverheadPredictorAfter,
		d.OutputSize.OverheadBefore,
		d.OutputSize.OverheadAbsolute,
		d.OutputSize.OverheadRatio,
		d.OutputSize.OverheadAbsoluteErrorScore,
		d.OutputSize.OverheadRatioErrorScore,
		d.OutputSize.OverheadActual,
		d.OutputSize.OverheadAfter,
		d.OutputSize.StaticSize,
		d.OutputSize.FallbackHint,
		d.OutputSize.GrowHint,
		d.OutputSize.Headroom,
		d.OutputSize.EstimateBefore,
		d.OutputSize.Actual,
		outputSizeErrorPercent(errorSize, d.OutputSize.Actual),
		withinTen,
		d.OutputSize.EstimateAfter,
		d.OutputSize.SamplesAfter,
		observed,
		d.OutputSize.Minimum,
		d.OutputSize.Maximum,
		boolHeaderValue(d.OutputSize.Unstable),
		boolHeaderValue(d.OutputSize.Limited),
		boolHeaderValue(d.OutputSize.GrowCalled),
		d.OutputSize.GrowAllocated,
		d.OutputSize.CapacityBefore,
		d.OutputSize.CapacityGrow,
		d.OutputSize.CapacityFinal,
		d.OutputSize.UnusedCapacity,
	)
}

func (d RenderDiagnostics) PartialOutputSizeHeader() string {
	partial := d.PartialOutput
	if partial.Calls == 0 {
		return ""
	}
	return fmt.Sprintf(
		"calls=%d;learned=%d;hint=%d;actual=%d;absolute-error=%d;error=%.2f;within-10=%d;unstable=%d;limited=%d;grow-calls=%d;grow-allocated=%d",
		partial.Calls,
		partial.Learned,
		partial.GrowHint,
		partial.Actual,
		partial.AbsoluteError,
		outputSizeErrorPercent(partial.AbsoluteError, partial.Actual),
		partial.WithinTen,
		partial.Unstable,
		partial.Limited,
		partial.GrowCalls,
		partial.GrowAllocated,
	)
}

func (d RenderDiagnostics) PartialOutputSizeDetailsHeader() string {
	if len(d.PartialOutput.Details) == 0 {
		return ""
	}
	parts := make([]string, 0, len(d.PartialOutput.Details))
	for _, detail := range d.PartialOutput.Details {
		parts = append(parts, fmt.Sprintf(
			"name=%s,calls=%d,learned=%d,hint=%d,actual=%d,error=%.2f,estimate=%d,samples=%d,min=%d,max=%d,unstable=%d,limited=%d,grow-calls=%d,grow-allocated=%d",
			partialOutputHeaderName(detail.Name),
			detail.Calls,
			detail.Learned,
			detail.GrowHint,
			detail.Actual,
			outputSizeErrorPercent(detail.AbsoluteError, detail.Actual),
			detail.Estimate,
			detail.Samples,
			detail.Minimum,
			detail.Maximum,
			boolHeaderValue(detail.Unstable),
			detail.Limited,
			detail.GrowCalls,
			detail.GrowAllocated,
		))
	}
	return strings.Join(parts, "|")
}

func AddRenderDiagnosticPartialOutput(ctx hctx.Context, name string, learned, growHint, actual, estimate int, samples uint64) {
	AddRenderDiagnosticPartialOutputAllocation(ctx, name, learned, growHint, actual, estimate, samples, RenderPartialOutputAllocation{})
}

func AddRenderDiagnosticLoopOutput(ctx hctx.Context, name string, line, items, learnedBytesPerItem, growHint, actual, estimateBytesPerItem int, samplesBefore, samplesAfter uint64, itemCountKnown, limited, growCalled bool, growAllocated int) {
	if ctx == nil || items <= 0 || actual < 0 {
		return
	}
	mode := GetOutputSizeEstimatorDiagnosticsMode()
	if mode == OutputSizeEstimatorDiagnosticsOff {
		return
	}
	if name == "" {
		name = "<expression>"
	}
	learned := outputSizeProduct(learnedBytesPerItem, items)
	errorSize := learned - actual
	if errorSize < 0 {
		errorSize = -errorSize
	}
	withinTen := outputSizeErrorPercent(errorSize, actual) < 10
	actualBytesPerItem := actual / items

	UpdateRenderDiagnostics(ctx, func(d *RenderDiagnostics) {
		loop := &d.LoopOutput
		loop.Calls++
		loop.Items += items
		if itemCountKnown {
			loop.KnownCount++
		}
		loop.Learned += learned
		loop.GrowHint += growHint
		loop.Actual += actual
		loop.AbsoluteError += errorSize
		if withinTen {
			loop.WithinTen++
		}
		if limited {
			loop.Limited++
		}
		if growCalled {
			loop.GrowCalls++
		}
		loop.GrowAllocated += growAllocated
		if mode != OutputSizeEstimatorDiagnosticsDetailed {
			return
		}

		for i := range loop.Details {
			if loop.Details[i].Name == name && loop.Details[i].Line == line {
				addLoopOutputDetail(&loop.Details[i], items, learned, growHint, actual, errorSize, learnedBytesPerItem, actualBytesPerItem, estimateBytesPerItem, samplesBefore, samplesAfter, withinTen, itemCountKnown, limited, growCalled, growAllocated)
				return
			}
		}
		if len(loop.Details) >= renderLoopOutputDetailLimit {
			return
		}
		detail := RenderLoopOutputSizeDetail{Name: name, Line: line}
		addLoopOutputDetail(&detail, items, learned, growHint, actual, errorSize, learnedBytesPerItem, actualBytesPerItem, estimateBytesPerItem, samplesBefore, samplesAfter, withinTen, itemCountKnown, limited, growCalled, growAllocated)
		loop.Details = append(loop.Details, detail)
	})
}

func addLoopOutputDetail(detail *RenderLoopOutputSizeDetail, items, learned, growHint, actual, errorSize, learnedBytesPerItem, actualBytesPerItem, estimateBytesPerItem int, samplesBefore, samplesAfter uint64, withinTen, itemCountKnown, limited, growCalled bool, growAllocated int) {
	detail.Calls++
	detail.Items += items
	detail.Learned += learned
	detail.GrowHint += growHint
	detail.Actual += actual
	detail.AbsoluteError += errorSize
	if withinTen {
		detail.WithinTen++
	}
	if itemCountKnown {
		detail.KnownCount++
	}
	detail.LearnedBytesPerItem = learnedBytesPerItem
	detail.ActualBytesPerItem = actualBytesPerItem
	detail.EstimateBytesPerItem = estimateBytesPerItem
	detail.SamplesBefore = samplesBefore
	detail.SamplesAfter = samplesAfter
	if limited {
		detail.Limited++
	}
	if growCalled {
		detail.GrowCalls++
	}
	detail.GrowAllocated += growAllocated
}

func AddRenderDiagnosticPartialOutputAllocation(ctx hctx.Context, name string, learned, growHint, actual, estimate int, samples uint64, allocation RenderPartialOutputAllocation) {
	if ctx == nil || actual < 0 {
		return
	}
	mode := GetOutputSizeEstimatorDiagnosticsMode()
	if mode == OutputSizeEstimatorDiagnosticsOff {
		return
	}
	if name == "" {
		name = "<anonymous>"
	}
	errorSize := learned - actual
	if errorSize < 0 {
		errorSize = -errorSize
	}
	withinTen := outputSizeErrorPercent(errorSize, actual) < 10
	UpdateRenderDiagnostics(ctx, func(d *RenderDiagnostics) {
		partial := &d.PartialOutput
		partial.Calls++
		partial.Learned += learned
		partial.GrowHint += growHint
		partial.Actual += actual
		partial.AbsoluteError += errorSize
		if withinTen {
			partial.WithinTen++
		}
		if allocation.Unstable {
			partial.Unstable++
		}
		if allocation.Limited {
			partial.Limited++
		}
		if allocation.GrowCalled {
			partial.GrowCalls++
		}
		partial.GrowAllocated += allocation.SpeculativeAllocated
		if mode != OutputSizeEstimatorDiagnosticsDetailed {
			return
		}

		for i := range partial.Details {
			if partial.Details[i].Name == name {
				addPartialOutputDetail(&partial.Details[i], learned, growHint, actual, errorSize, estimate, samples, allocation)
				return
			}
		}
		if len(partial.Details) >= renderPartialOutputDetailLimit {
			return
		}
		detail := RenderPartialOutputSizeDetail{Name: name}
		addPartialOutputDetail(&detail, learned, growHint, actual, errorSize, estimate, samples, allocation)
		partial.Details = append(partial.Details, detail)
	})
}

func addPartialOutputDetail(detail *RenderPartialOutputSizeDetail, learned, growHint, actual, errorSize, estimate int, samples uint64, allocation RenderPartialOutputAllocation) {
	detail.Calls++
	detail.Learned += learned
	detail.GrowHint += growHint
	detail.Actual += actual
	detail.AbsoluteError += errorSize
	detail.Estimate = estimate
	detail.Samples = samples
	detail.Minimum = allocation.Minimum
	detail.Maximum = allocation.Maximum
	detail.Unstable = allocation.Unstable
	if allocation.Limited {
		detail.Limited++
	}
	if allocation.GrowCalled {
		detail.GrowCalls++
	}
	detail.GrowAllocated += allocation.SpeculativeAllocated
}

func boolHeaderValue(value bool) int {
	if value {
		return 1
	}
	return 0
}

func outputSizeProfileBand(band string) string {
	if band == "" {
		return "none"
	}
	return band
}

func outputSizeErrorPercent(errorSize, actual int) float64 {
	if actual <= 0 {
		if errorSize == 0 {
			return 0
		}
		return 100
	}
	return float64(errorSize) * 100 / float64(actual)
}

func outputSizeProduct(left, right int) int {
	if left <= 0 || right <= 0 {
		return 0
	}
	maximum := int(^uint(0) >> 1)
	if left > maximum/right {
		return maximum
	}
	return left * right
}

func partialOutputHeaderName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	pathParts := strings.Split(name, "/")
	trimmed := false
	for i := len(pathParts) - 1; i >= 0; i-- {
		if pathParts[i] == "templates" {
			name = ".../" + strings.Join(pathParts[i:], "/")
			trimmed = true
			break
		}
	}
	if !trimmed && len(pathParts) > 3 {
		name = ".../" + strings.Join(pathParts[len(pathParts)-3:], "/")
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case ',', ';', '|', '=':
			return ' '
		default:
			return r
		}
	}, name)
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	return name
}

func (d RenderDiagnostics) VMHelperHotspotsHeader() string {
	return renderVMHotspotsHeader(d.VMHotspots.Helpers)
}

func (d RenderDiagnostics) VMPartialHotspotsHeader() string {
	return renderVMHotspotsHeader(d.VMHotspots.Partials)
}

func (d RenderDiagnostics) VMHelperCallPathsHeader() string {
	stats := d.VMHotspots
	if stats.HelperCalls == 0 {
		return ""
	}
	unclassified := stats.HelperCalls - stats.HelperDirectCalls - stats.HelperReflectionCalls
	if unclassified < 0 {
		unclassified = 0
	}
	return fmt.Sprintf(
		"direct-calls=%d;reflection-calls=%d;unclassified-calls=%d;reflection-percent=%.2f;direct-time-ms=%.3f;reflection-time-ms=%.3f;direct-details-dropped=%d;reflection-details-dropped=%d",
		stats.HelperDirectCalls,
		stats.HelperReflectionCalls,
		unclassified,
		d.VMHelperReflectionPercent(),
		d.VMHelperDirectDurationMilliseconds(),
		d.VMHelperReflectionDurationMilliseconds(),
		stats.HelperDirectDetailsDropped,
		stats.HelperReflectionDetailsDropped,
	)
}

func (d RenderDiagnostics) VMHelperCallPathDetailsHeader() string {
	if len(d.VMHotspots.HelperCallPaths) == 0 {
		return ""
	}
	details := append([]RenderVMHelperCallPathDiagnostics(nil), d.VMHotspots.HelperCallPaths...)
	sort.Slice(details, func(i, j int) bool {
		if details[i].Path != details[j].Path {
			return details[i].Path == RenderVMHelperCallReflection
		}
		if details[i].Duration != details[j].Duration {
			return details[i].Duration > details[j].Duration
		}
		if details[i].Calls != details[j].Calls {
			return details[i].Calls > details[j].Calls
		}
		if details[i].Name != details[j].Name {
			return details[i].Name < details[j].Name
		}
		return details[i].Signature < details[j].Signature
	})
	if len(details) > renderVMHelperPathDetailLimit {
		details = details[:renderVMHelperPathDetailLimit]
	}
	parts := make([]string, 0, len(details))
	for _, detail := range details {
		parts = append(parts, fmt.Sprintf(
			"path=%s,name=%s,signature=%s,calls=%d,time-ms=%.3f",
			detail.Path,
			renderVMHelperPathHeaderValue(detail.Name),
			renderVMHelperPathHeaderValue(detail.Signature),
			detail.Calls,
			float64(detail.Duration)/float64(time.Millisecond),
		))
	}
	return strings.Join(parts, "|")
}

func AddRenderDiagnosticVMHelperTiming(ctx hctx.Context, name string, duration time.Duration) {
	addRenderDiagnosticVMHotspot(ctx, name, duration, true)
}

// AddRenderDiagnosticVMHelperCall records a classified helper invocation when
// VM hotspot diagnostics are enabled on ctx.
func AddRenderDiagnosticVMHelperCall(ctx hctx.Context, name, signature string, path RenderVMHelperCallPath, duration time.Duration) {
	if ctx == nil || duration < 0 || !RenderVMHotspotDiagnosticsEnabled(ctx) {
		return
	}
	RenderVMHotspotDiagnosticsRecorder{state: renderDiagnosticsStateFromContext(ctx, true), captured: true}.AddHelperCall(name, signature, path, duration)
}

func AddRenderDiagnosticVMPartialTiming(ctx hctx.Context, name string, duration time.Duration) {
	addRenderDiagnosticVMHotspot(ctx, name, duration, false)
}

// CaptureRenderVMHotspotDiagnostics snapshots the diagnostics setting for one
// render. Changes made afterward apply when the next render captures its
// recorder.
func CaptureRenderVMHotspotDiagnostics(ctx hctx.Context) RenderVMHotspotDiagnosticsRecorder {
	if ctx == nil {
		return RenderVMHotspotDiagnosticsRecorder{}
	}
	if provider, ok := ctx.(interface {
		RenderVMHotspotDiagnosticsRecorder() RenderVMHotspotDiagnosticsRecorder
	}); ok {
		if recorder := provider.RenderVMHotspotDiagnosticsRecorder(); recorder.captured {
			return recorder
		}
	}
	if !RenderVMHotspotDiagnosticsEnabled(ctx) {
		return RenderVMHotspotDiagnosticsRecorder{captured: true}
	}
	return RenderVMHotspotDiagnosticsRecorder{state: renderDiagnosticsStateFromContext(ctx, true), captured: true}
}

// Enabled reports whether VM hotspot timing was enabled when this recorder was
// captured.
func (r RenderVMHotspotDiagnosticsRecorder) Enabled() bool {
	return r.state != nil
}

// AddHelperTiming records a helper duration without re-reading the diagnostics
// setting from the render context.
func (r RenderVMHotspotDiagnosticsRecorder) AddHelperTiming(name string, duration time.Duration) {
	r.add(name, duration, true)
}

// AddHelperCall records the invocation path and Go signature of one VM helper
// call without re-reading the diagnostics setting from the render context.
func (r RenderVMHotspotDiagnosticsRecorder) AddHelperCall(name, signature string, path RenderVMHelperCallPath, duration time.Duration) {
	if r.state == nil || duration < 0 {
		return
	}
	if name == "" {
		name = "<anonymous>"
	}
	if signature == "" {
		signature = "<unknown>"
	}
	updateRenderDiagnosticsState(r.state, func(d *RenderDiagnostics) {
		d.VMHotspots.HelperCalls++
		d.VMHotspots.HelperDuration += duration
		addRenderVMHotspot(&d.VMHotspots.Helpers, name, duration)
		addRenderVMHelperCallPath(&d.VMHotspots, name, signature, path, duration)
	})
}

// AddPartialTiming records a partial duration without re-reading the
// diagnostics setting from the render context.
func (r RenderVMHotspotDiagnosticsRecorder) AddPartialTiming(name string, duration time.Duration) {
	r.add(name, duration, false)
}

func (r RenderVMHotspotDiagnosticsRecorder) add(name string, duration time.Duration, helper bool) {
	if r.state == nil || duration < 0 {
		return
	}
	if name == "" {
		name = "<anonymous>"
	}
	updateRenderDiagnosticsState(r.state, func(d *RenderDiagnostics) {
		if helper {
			d.VMHotspots.HelperCalls++
			d.VMHotspots.HelperDuration += duration
			addRenderVMHotspot(&d.VMHotspots.Helpers, name, duration)
			return
		}
		d.VMHotspots.PartialCalls++
		d.VMHotspots.PartialDuration += duration
		addRenderVMHotspot(&d.VMHotspots.Partials, name, duration)
	})
}

func EnableRenderVMHotspotDiagnostics(ctx hctx.Context) {
	SetRenderVMHotspotDiagnostics(ctx, true)
}

func DisableRenderVMHotspotDiagnostics(ctx hctx.Context) {
	SetRenderVMHotspotDiagnostics(ctx, false)
}

func SetRenderVMHotspotDiagnostics(ctx hctx.Context, enabled bool) {
	if ctx == nil {
		return
	}
	ctx.Set(renderVMHotspotDiagnosticsKey, enabled)
	if enabled {
		renderDiagnosticsStateFromContext(ctx, true)
	}
}

func RenderVMHotspotDiagnosticsEnabled(ctx hctx.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(renderVMHotspotDiagnosticsKey).(bool)
	return enabled
}

func RenderDiagnosticsFromContext(ctx hctx.Context) (RenderDiagnostics, bool) {
	if ctx == nil {
		return RenderDiagnostics{}, false
	}
	return renderDiagnosticsFromValue(ctx.Value(renderDiagnosticsKey))
}

func RenderDiagnosticsFromData(data map[string]interface{}) (RenderDiagnostics, bool) {
	if data == nil {
		return RenderDiagnostics{}, false
	}
	return renderDiagnosticsFromValue(data[renderDiagnosticsKey])
}

func SetRenderDiagnostics(ctx hctx.Context, diagnostics RenderDiagnostics) {
	if ctx == nil {
		return
	}
	if state := renderDiagnosticsStateFromContext(ctx, true); state != nil {
		state.mu.Lock()
		state.diagnostics = diagnostics
		state.mu.Unlock()
	}
}

func UpdateRenderDiagnostics(ctx hctx.Context, update func(*RenderDiagnostics)) {
	if ctx == nil || update == nil {
		return
	}
	state := renderDiagnosticsStateFromContext(ctx, true)
	updateRenderDiagnosticsState(state, update)
}

func updateRenderDiagnosticsState(state *renderDiagnosticsState, update func(*RenderDiagnostics)) {
	if state == nil || update == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	update(&state.diagnostics)
}

func UpdateRenderDiagnosticsForTemplate(ctx hctx.Context, filename string, update func(*RenderDiagnostics)) {
	if update == nil {
		return
	}
	UpdateRenderDiagnostics(ctx, func(d *RenderDiagnostics) {
		if !renderDiagnosticsCanUpdateTemplate(d, filename) {
			return
		}
		update(d)
	})
}

// AddRenderDiagnosticPartialFallback records one partial that entered the
// interpreter during a VM render. Recording is available with regular render
// diagnostics and does not require VM hotspot timing to be enabled.
func AddRenderDiagnosticPartialFallback(ctx hctx.Context, name string, reason RenderPartialFallbackReason) {
	if ctx == nil || !RenderPartialFallbackDiagnosticsEnabled(ctx) {
		return
	}
	if name == "" {
		name = "<anonymous>"
	}
	if reason == "" {
		reason = RenderPartialFallbackGenericBytecode
	}
	UpdateRenderDiagnostics(ctx, func(d *RenderDiagnostics) {
		d.PartialFallbacks.Calls++
		for i := range d.PartialFallbacks.Details {
			detail := &d.PartialFallbacks.Details[i]
			if detail.Name == name && detail.Reason == reason {
				detail.Calls++
				return
			}
		}
		if len(d.PartialFallbacks.Details) >= renderPartialFallbackDetailLimit {
			d.PartialFallbacks.DetailsDropped++
			return
		}
		d.PartialFallbacks.Details = append(d.PartialFallbacks.Details, RenderPartialFallbackDetail{
			Name:   name,
			Reason: reason,
			Calls:  1,
		})
	})
}

// BeginRenderDiagnosticPartialFallback marks nested partial calls as inherited
// interpreter work until the returned restore function is called.
func BeginRenderDiagnosticPartialFallback(ctx hctx.Context) func() {
	if ctx == nil || !RenderPartialFallbackDiagnosticsEnabled(ctx) {
		return nil
	}
	previous := ctx.Value(renderPartialFallbackActiveKey)
	ctx.Set(renderPartialFallbackActiveKey, true)
	return func() {
		if previous == nil {
			ctx.Set(renderPartialFallbackActiveKey, false)
			return
		}
		ctx.Set(renderPartialFallbackActiveKey, previous)
	}
}

// RenderDiagnosticPartialFallbackActive reports whether the current
// interpreter render was entered as a VM partial fallback.
func RenderDiagnosticPartialFallbackActive(ctx hctx.Context) bool {
	if ctx == nil {
		return false
	}
	active, _ := ctx.Value(renderPartialFallbackActiveKey).(bool)
	return active
}

// EnableRenderPartialFallbackDiagnostics enables bounded partial fallback
// details on ctx.
func EnableRenderPartialFallbackDiagnostics(ctx hctx.Context) {
	SetRenderPartialFallbackDiagnostics(ctx, true)
}

// DisableRenderPartialFallbackDiagnostics disables partial fallback details on
// ctx.
func DisableRenderPartialFallbackDiagnostics(ctx hctx.Context) {
	SetRenderPartialFallbackDiagnostics(ctx, false)
}

// SetRenderPartialFallbackDiagnostics controls partial fallback details on ctx.
func SetRenderPartialFallbackDiagnostics(ctx hctx.Context, enabled bool) {
	if ctx == nil {
		return
	}
	ctx.Set(renderPartialFallbackDiagnosticsKey, enabled)
	if enabled {
		renderDiagnosticsStateFromContext(ctx, true)
	}
}

// RenderPartialFallbackDiagnosticsEnabled reports whether partial fallback
// details are enabled on ctx.
func RenderPartialFallbackDiagnosticsEnabled(ctx hctx.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(renderPartialFallbackDiagnosticsKey).(bool)
	return enabled
}

func renderDiagnosticsCanUpdateTemplate(d *RenderDiagnostics, filename string) bool {
	if d == nil || d.TemplateFilename == "" {
		return true
	}
	return filename != "" && d.TemplateFilename == filename
}

func SetRenderDiagnosticsRootActive(ctx hctx.Context, active bool) func() {
	if ctx == nil {
		return nil
	}
	previous, _ := ctx.Value(renderDiagnosticsRootActiveKey).(bool)
	ctx.Set(renderDiagnosticsRootActiveKey, active)
	return func() {
		ctx.Set(renderDiagnosticsRootActiveKey, previous)
	}
}

func RenderDiagnosticsRootActive(ctx hctx.Context) bool {
	if ctx == nil {
		return false
	}
	active, _ := ctx.Value(renderDiagnosticsRootActiveKey).(bool)
	return active
}

func renderDiagnosticsStateFromContext(ctx hctx.Context, create bool) *renderDiagnosticsState {
	if ctx == nil {
		return nil
	}
	switch value := ctx.Value(renderDiagnosticsKey).(type) {
	case *renderDiagnosticsState:
		return value
	case RenderDiagnostics:
		state := &renderDiagnosticsState{diagnostics: value}
		if create {
			ctx.Set(renderDiagnosticsKey, state)
		}
		return state
	case *RenderDiagnostics:
		if value == nil {
			break
		}
		state := &renderDiagnosticsState{diagnostics: *value}
		if create {
			ctx.Set(renderDiagnosticsKey, state)
		}
		return state
	}
	if !create {
		return nil
	}
	state := &renderDiagnosticsState{}
	ctx.Set(renderDiagnosticsKey, state)
	return state
}

func renderDiagnosticsFromValue(value interface{}) (RenderDiagnostics, bool) {
	switch value := value.(type) {
	case *renderDiagnosticsState:
		if value == nil {
			return RenderDiagnostics{}, false
		}
		value.mu.Lock()
		defer value.mu.Unlock()
		return value.diagnostics, true
	case RenderDiagnostics:
		return value, true
	case *RenderDiagnostics:
		if value == nil {
			return RenderDiagnostics{}, false
		}
		return *value, true
	default:
		return RenderDiagnostics{}, false
	}
}

func addRenderDiagnosticVMHotspot(ctx hctx.Context, name string, duration time.Duration, helper bool) {
	if ctx == nil || duration < 0 || !RenderVMHotspotDiagnosticsEnabled(ctx) {
		return
	}
	RenderVMHotspotDiagnosticsRecorder{state: renderDiagnosticsStateFromContext(ctx, true), captured: true}.add(name, duration, helper)
}

func addRenderVMHotspot(stats *[]RenderVMHotspot, name string, duration time.Duration) {
	for i := range *stats {
		if (*stats)[i].Name == name {
			(*stats)[i].Calls++
			(*stats)[i].Duration += duration
			return
		}
	}
	*stats = append(*stats, RenderVMHotspot{Name: name, Calls: 1, Duration: duration})
}

func addRenderVMHelperCallPath(stats *RenderVMHotspotDiagnostics, name, signature string, path RenderVMHelperCallPath, duration time.Duration) {
	if stats == nil || (path != RenderVMHelperCallDirect && path != RenderVMHelperCallReflection) {
		return
	}
	if path == RenderVMHelperCallDirect {
		stats.HelperDirectCalls++
		stats.HelperDirectDuration += duration
	} else {
		stats.HelperReflectionCalls++
		stats.HelperReflectionDuration += duration
	}

	pathDetails := 0
	for i := range stats.HelperCallPaths {
		detail := &stats.HelperCallPaths[i]
		if detail.Path != path {
			continue
		}
		pathDetails++
		if detail.Name == name && detail.Signature == signature {
			detail.Calls++
			detail.Duration += duration
			return
		}
	}
	if pathDetails >= renderVMHelperPathDetailLimit {
		if path == RenderVMHelperCallDirect {
			stats.HelperDirectDetailsDropped++
		} else {
			stats.HelperReflectionDetailsDropped++
		}
		return
	}
	stats.HelperCallPaths = append(stats.HelperCallPaths, RenderVMHelperCallPathDiagnostics{
		Name:      name,
		Signature: signature,
		Path:      path,
		Calls:     1,
		Duration:  duration,
	})
}

func renderVMHotspotsHeader(stats []RenderVMHotspot) string {
	if len(stats) == 0 {
		return ""
	}
	copyStats := append([]RenderVMHotspot(nil), stats...)
	sort.Slice(copyStats, func(i, j int) bool {
		if copyStats[i].Duration == copyStats[j].Duration {
			return copyStats[i].Name < copyStats[j].Name
		}
		return copyStats[i].Duration > copyStats[j].Duration
	})
	if len(copyStats) > 8 {
		copyStats = copyStats[:8]
	}
	parts := make([]string, 0, len(copyStats))
	for _, stat := range copyStats {
		parts = append(parts, fmt.Sprintf("%s:%d:%.3f", renderVMHotspotHeaderName(stat.Name), stat.Calls, float64(stat.Duration)/float64(time.Millisecond)))
	}
	return strings.Join(parts, ";")
}

func renderVMHotspotHeaderName(name string) string {
	name = strings.ReplaceAll(name, ",", "_")
	name = strings.ReplaceAll(name, ";", "_")
	name = strings.ReplaceAll(name, ":", "_")
	name = strings.TrimSpace(name)
	if name == "" {
		return "<anonymous>"
	}
	return name
}

func renderVMHelperPathHeaderValue(value string) string {
	value = strings.Map(func(r rune) rune {
		switch r {
		case ',', ';', '|', '=', '\r', '\n':
			return '_'
		default:
			return r
		}
	}, strings.TrimSpace(value))
	if value == "" {
		return "<unknown>"
	}
	runes := []rune(value)
	if len(runes) > 160 {
		return string(runes[:160])
	}
	return value
}
