package plush_test

import (
	"testing"

	rootplush "github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/hctx"
	vmplush "github.com/gobuffalo/plush/v5/vm/plush"
	"github.com/stretchr/testify/require"
)

type contextFactory func() hctx.Context

func emptyContext() hctx.Context {
	return rootplush.NewContext()
}

func contextWith(data map[string]interface{}) contextFactory {
	return func() hctx.Context {
		return rootplush.NewContextWith(data)
	}
}

func compareRender(t *testing.T, input string, factory contextFactory) {
	t.Helper()

	interpreterOut, interpreterErr := renderInterpreter(input, factory)
	vmOut, vmErr := renderVM(t, input, factory)

	require.Equalf(t, errorString(interpreterErr), errorString(vmErr), "error mismatch\ninterpreter: %q\nvm:          %q", errorString(interpreterErr), errorString(vmErr))
	require.Equalf(t, interpreterOut, vmOut, "output mismatch\ninterpreter: %q\nvm:          %q", interpreterOut, vmOut)
}

func comparePlannedRender(t *testing.T, input string, factory contextFactory, expected string) {
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
	require.NotEqual(t, rootplush.RenderFastPathInterpreterFallback, diagnostics.FastPath, "template fell back to the interpreter: %s", diagnostics.FastReject)
	require.Empty(t, diagnostics.FastReject)
}

func compareRenderError(t *testing.T, input string, factory contextFactory) {
	t.Helper()

	interpreterOut, interpreterErr := renderInterpreter(input, factory)
	vmOut, vmErr := renderVM(t, input, factory)

	require.Error(t, interpreterErr, "expected interpreter error, got output %q", interpreterOut)
	require.Error(t, vmErr, "expected VM error, got output %q", vmOut)
	require.Equalf(t, interpreterOut, vmOut, "error output mismatch\ninterpreter: %q\nvm:          %q", interpreterOut, vmOut)
	require.Equalf(t, interpreterErr.Error(), vmErr.Error(), "error mismatch\ninterpreter: %q\nvm:          %q", interpreterErr.Error(), vmErr.Error())
}

func compareBothRenderError(t *testing.T, input string, factory contextFactory) {
	t.Helper()

	interpreterOut, interpreterErr := renderInterpreter(input, factory)
	vmOut, vmErr := renderVM(t, input, factory)

	require.Error(t, interpreterErr, "expected interpreter error, got output %q", interpreterOut)
	require.Error(t, vmErr, "expected VM error, got output %q", vmOut)
}

func renderInterpreter(input string, factory contextFactory) (string, error) {
	return rootplush.Render(input, factory())
}

func renderVM(t *testing.T, input string, factory contextFactory) (string, error) {
	t.Helper()

	return renderVMContext(t, input, factory())
}

func renderVMContext(t *testing.T, input string, ctx hctx.Context) (string, error) {
	t.Helper()

	previousFallback := rootplush.SetVMGenericFallback(false)
	defer rootplush.SetVMGenericFallback(previousFallback)
	rootplush.EnableRenderPartialFallbackDiagnostics(ctx)

	out, err := vmplush.Render(input, ctx)
	requireNoInterpreterFallback(t, ctx, err)
	return out, err
}

func requireNoInterpreterFallback(t *testing.T, ctx hctx.Context, renderErr error) {
	t.Helper()

	diagnostics, ok := rootplush.RenderDiagnosticsFromContext(ctx)
	if renderErr == nil {
		require.True(t, ok, "expected VM render diagnostics")
	}
	if ok {
		require.NotEqual(t, rootplush.RenderFastPathInterpreterFallback, diagnostics.FastPath, "parity render fell back to the interpreter: %s", diagnostics.FastReject)
		require.Zero(t, diagnostics.PartialFallbacks.Calls, "parity render used interpreter fallback for a partial: %+v", diagnostics.PartialFallbacks.Details)
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
