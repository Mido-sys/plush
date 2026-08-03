package vm

import (
	"strings"

	"github.com/gobuffalo/plush/v5"
	"github.com/gobuffalo/plush/v5/helpers/hctx"
	"github.com/gobuffalo/plush/v5/vm/compiler"
)

const fastLoopMaxGrow = 256 << 10

type fastLoopSizeObservation struct {
	available           bool
	itemCountKnown      bool
	learnedBytesPerItem int
	samplesBefore       uint64
	growHint            int
	limited             bool
	growCalled          bool
	growAllocated       int
}

func fastLoopGrowHint(loop *compiler.FastLoopPlan, itemCount int) int {
	if !plush.OutputSizeEstimatorEnabled() || loop == nil || loop.Silent || loop.SizeStats == nil || loop.SizeStats.Samples() == 0 || itemCount <= 0 {
		return 0
	}
	bytesPerItem := loop.SizeStats.BytesPerItem()
	if bytesPerItem <= 0 {
		return 0
	}
	if itemCount > fastLoopMaxGrow/bytesPerItem {
		return fastLoopMaxGrow
	}
	hint := itemCount * bytesPerItem
	if hint > fastLoopMaxGrow {
		return fastLoopMaxGrow
	}
	return hint
}

func growFastLoopOutput(out *strings.Builder, loop *compiler.FastLoopPlan, itemCount int) int {
	if out == nil {
		return 0
	}
	hint := fastLoopGrowHint(loop, itemCount)
	if hint <= 0 || out.Cap()-out.Len() >= hint {
		return 0
	}
	out.Grow(hint)
	return hint
}

func beginFastLoopSizeObservation(out *strings.Builder, loop *compiler.FastLoopPlan, itemCount int, itemCountKnown bool) fastLoopSizeObservation {
	if !plush.OutputSizeEstimatorEnabled() || loop == nil || loop.Silent || loop.SizeStats == nil {
		return fastLoopSizeObservation{}
	}
	observation := fastLoopSizeObservation{
		available:           true,
		itemCountKnown:      itemCountKnown,
		learnedBytesPerItem: loop.SizeStats.BytesPerItem(),
		samplesBefore:       loop.SizeStats.Samples(),
	}
	if !itemCountKnown || itemCount <= 0 {
		return observation
	}
	observation.growHint = fastLoopGrowHint(loop, itemCount)
	observation.limited = fastLoopGrowLimited(observation.learnedBytesPerItem, itemCount)
	if out == nil || observation.growHint <= 0 {
		return observation
	}
	capacityBefore := out.Cap()
	growFastLoopOutput(out, loop, itemCount)
	observation.growAllocated = out.Cap() - capacityBefore
	observation.growCalled = observation.growAllocated > 0
	return observation
}

func fastLoopGrowLimited(bytesPerItem, itemCount int) bool {
	if bytesPerItem <= 0 || itemCount <= 0 {
		return false
	}
	return itemCount > fastLoopMaxGrow/bytesPerItem
}

func observeFastLoopOutput(ctx hctx.Context, loop *compiler.FastLoopPlan, producedBytes, renderedItems int, observation fastLoopSizeObservation) {
	if !observation.available || !plush.OutputSizeEstimatorEnabled() || loop == nil || loop.Silent || loop.SizeStats == nil || producedBytes < 0 || renderedItems <= 0 {
		return
	}
	loop.SizeStats.Observe(producedBytes / renderedItems)
	plush.AddRenderDiagnosticLoopOutput(
		ctx,
		loop.IterableName,
		loop.Line,
		renderedItems,
		observation.learnedBytesPerItem,
		observation.growHint,
		producedBytes,
		loop.SizeStats.BytesPerItem(),
		observation.samplesBefore,
		loop.SizeStats.Samples(),
		observation.itemCountKnown,
		observation.limited,
		observation.growCalled,
		observation.growAllocated,
	)
}
