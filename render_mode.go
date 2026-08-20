package plush

import (
	"errors"
	"sync/atomic"

	"github.com/gobuffalo/plush/v5/helpers/hctx"
)

type RenderMode int32

// OutputSizeEstimatorDiagnosticsMode controls how much estimator observability
// is collected. It does not enable or disable estimator learning.
type OutputSizeEstimatorDiagnosticsMode int32

const (
	RenderModeInterpreter RenderMode = iota
	RenderModeVM
)

const (
	// OutputSizeEstimatorDiagnosticsDetailed preserves aggregate and per-loop /
	// per-partial diagnostics.
	OutputSizeEstimatorDiagnosticsDetailed OutputSizeEstimatorDiagnosticsMode = iota
	// OutputSizeEstimatorDiagnosticsSummary preserves aggregate diagnostics but
	// skips per-loop and per-partial detail collection.
	OutputSizeEstimatorDiagnosticsSummary
	// OutputSizeEstimatorDiagnosticsOff disables estimator diagnostics while
	// leaving estimator learning and builder growth enabled.
	OutputSizeEstimatorDiagnosticsOff
)

var renderMode atomic.Int32
var vmGenericFallback atomic.Bool
var outputSizeEstimatorDisabled atomic.Bool
var outputSizeEstimatorDiagnosticsMode atomic.Int32
var vmRenderer atomic.Value
var trustedVMCacheRenderer atomic.Value

func init() {
	outputSizeEstimatorDiagnosticsMode.Store(int32(OutputSizeEstimatorDiagnosticsOff))
}

var ErrVMRendererNotRegistered = errors.New("plush VM renderer is not registered")

type RenderFunc func(string, hctx.Context) (string, error)
type TrustedVMCacheRenderFunc func(string, hctx.Context) (string, bool, error)

func SetRenderMode(mode RenderMode) RenderMode {
	if mode != RenderModeInterpreter && mode != RenderModeVM {
		mode = RenderModeInterpreter
	}
	previous := RenderMode(renderMode.Swap(int32(mode)))
	return previous
}

func GetRenderMode() RenderMode {
	return RenderMode(renderMode.Load())
}

func SetVMGenericFallback(enabled bool) bool {
	return vmGenericFallback.Swap(enabled)
}

func VMGenericFallbackEnabled() bool {
	return vmGenericFallback.Load()
}

// SetOutputSizeEstimatorEnabled enables or disables adaptive output-size
// learning and growth hints. It returns the previous setting.
func SetOutputSizeEstimatorEnabled(enabled bool) bool {
	wasDisabled := outputSizeEstimatorDisabled.Swap(!enabled)
	return !wasDisabled
}

// OutputSizeEstimatorEnabled reports whether adaptive output-size estimation
// is enabled. The estimator is enabled by default.
func OutputSizeEstimatorEnabled() bool {
	return !outputSizeEstimatorDisabled.Load()
}

// SetOutputSizeEstimatorDiagnosticsMode changes estimator diagnostic
// collection and returns the previous mode. Diagnostics are off by default,
// and invalid values also select off to avoid enabling instrumentation
// accidentally.
func SetOutputSizeEstimatorDiagnosticsMode(mode OutputSizeEstimatorDiagnosticsMode) OutputSizeEstimatorDiagnosticsMode {
	mode = normalizeOutputSizeEstimatorDiagnosticsMode(mode)
	previous := OutputSizeEstimatorDiagnosticsMode(outputSizeEstimatorDiagnosticsMode.Swap(int32(mode)))
	return normalizeOutputSizeEstimatorDiagnosticsMode(previous)
}

// GetOutputSizeEstimatorDiagnosticsMode returns the current estimator
// diagnostic collection mode.
func GetOutputSizeEstimatorDiagnosticsMode() OutputSizeEstimatorDiagnosticsMode {
	return normalizeOutputSizeEstimatorDiagnosticsMode(OutputSizeEstimatorDiagnosticsMode(outputSizeEstimatorDiagnosticsMode.Load()))
}

func normalizeOutputSizeEstimatorDiagnosticsMode(mode OutputSizeEstimatorDiagnosticsMode) OutputSizeEstimatorDiagnosticsMode {
	switch mode {
	case OutputSizeEstimatorDiagnosticsDetailed,
		OutputSizeEstimatorDiagnosticsSummary,
		OutputSizeEstimatorDiagnosticsOff:
		return mode
	default:
		return OutputSizeEstimatorDiagnosticsOff
	}
}

func RegisterVMRenderer(renderer RenderFunc) {
	if renderer == nil {
		return
	}
	vmRenderer.Store(renderer)
}

func registeredVMRenderer() (RenderFunc, bool) {
	renderer := vmRenderer.Load()
	if renderer == nil {
		return nil, false
	}
	fn, ok := renderer.(RenderFunc)
	return fn, ok && fn != nil
}

func RegisterTrustedVMCacheRenderer(renderer TrustedVMCacheRenderFunc) {
	if renderer == nil {
		return
	}
	trustedVMCacheRenderer.Store(renderer)
}

func registeredTrustedVMCacheRenderer() (TrustedVMCacheRenderFunc, bool) {
	renderer := trustedVMCacheRenderer.Load()
	if renderer == nil {
		return nil, false
	}
	fn, ok := renderer.(TrustedVMCacheRenderFunc)
	return fn, ok && fn != nil
}
