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

	PunchHoleCacheDisabled = "disabled"
	PunchHoleCacheHit      = "hit"
	PunchHoleCacheMiss     = "miss"

	renderPartialOutputDetailLimit = 8
	renderLoopOutputDetailLimit    = 8
)

var renderDiagnosticsKey = "__plush_internal_render_diagnostics_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"
var renderVMHotspotDiagnosticsKey = "__plush_internal_render_vm_hotspot_diagnostics_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"
var renderDiagnosticsRootActiveKey = "__plush_internal_render_diagnostics_root_active_" + fmt.Sprintf("%d", time.Now().UnixNano()) + "__"

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
	HelperCalls     int
	HelperDuration  time.Duration
	PartialCalls    int
	PartialDuration time.Duration
	Helpers         []RenderVMHotspot
	Partials        []RenderVMHotspot
}

type RenderVMHotspot struct {
	Name     string
	Calls    int
	Duration time.Duration
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

func AddRenderDiagnosticVMHelperTiming(ctx hctx.Context, name string, duration time.Duration) {
	addRenderDiagnosticVMHotspot(ctx, name, duration, true)
}

func AddRenderDiagnosticVMPartialTiming(ctx hctx.Context, name string, duration time.Duration) {
	addRenderDiagnosticVMHotspot(ctx, name, duration, false)
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
	if state == nil {
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
	if name == "" {
		name = "<anonymous>"
	}
	UpdateRenderDiagnostics(ctx, func(d *RenderDiagnostics) {
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
