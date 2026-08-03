package compiler

import (
	"math"
	"sync/atomic"
)

const outputSizeStatsMaxGrowHint = 4 << 20
const outputSizeStatsMaxHeadroom = 64 << 10
const outputSizeStatsHeadroomDivisor = 10

type OutputSizeStats struct {
	estimate atomic.Uint64
	headroom atomic.Uint64
	samples  atomic.Uint64
	minimum  atomic.Uint64
	maximum  atomic.Uint64
}

func (s *OutputSizeStats) GrowHint(staticSize int) int {
	if staticSize < 0 {
		staticSize = 0
	}
	hint := uint64(staticSize)
	if s != nil {
		if estimate := s.estimate.Load(); estimate > hint {
			hint = estimate
		}
	}
	return outputSizeHintInt(hint)
}

func (s *OutputSizeStats) Observe(actualSize int) {
	s.observe(actualSize, 0, false)
}

func (s *OutputSizeStats) ObserveWithHeadroom(actualSize, estimatedSize int) {
	s.observe(actualSize, estimatedSize, true)
}

func (s *OutputSizeStats) observe(actualSize, estimatedSize int, trackHeadroom bool) {
	if s == nil || actualSize <= 0 {
		return
	}
	actual := outputSizeHintUint64(uint64(actualSize))
	s.observeRange(actual)
	for {
		current := s.estimate.Load()
		next := nextOutputSizeEstimate(current, actual, s.samples.Load())
		if s.estimate.CompareAndSwap(current, next) {
			if trackHeadroom && estimatedSize > 0 {
				s.observeHeadroom(outputSizeHintUint64(uint64(estimatedSize)), actual)
			}
			s.samples.Add(1)
			return
		}
	}
}

func (s *OutputSizeStats) Headroom(growHint int) int {
	if s == nil || growHint <= 0 {
		return 0
	}
	limit := growHint / outputSizeStatsHeadroomDivisor
	if limit > outputSizeStatsMaxHeadroom {
		limit = outputSizeStatsMaxHeadroom
	}
	headroom := outputSizeHintInt(s.headroom.Load())
	if headroom > limit {
		return limit
	}
	return headroom
}

func (s *OutputSizeStats) observeHeadroom(estimated, actual uint64) {
	for {
		current := s.headroom.Load()
		next := nextOutputSizeHeadroom(current, estimated, actual)
		if s.headroom.CompareAndSwap(current, next) {
			return
		}
	}
}

func (s *OutputSizeStats) Range() (int, int) {
	if s == nil {
		return 0, 0
	}
	return outputSizeHintInt(s.minimum.Load()), outputSizeHintInt(s.maximum.Load())
}

func (s *OutputSizeStats) Unstable() bool {
	if s == nil || s.samples.Load() < 2 {
		return false
	}
	minimum := s.minimum.Load()
	maximum := s.maximum.Load()
	return minimum > 0 && maximum/minimum >= 4
}

func (s *OutputSizeStats) observeRange(actual uint64) {
	for {
		minimum := s.minimum.Load()
		if minimum != 0 && minimum <= actual {
			break
		}
		if s.minimum.CompareAndSwap(minimum, actual) {
			break
		}
	}
	for {
		maximum := s.maximum.Load()
		if maximum >= actual {
			break
		}
		if s.maximum.CompareAndSwap(maximum, actual) {
			break
		}
	}
}

func (s *OutputSizeStats) Estimate() int {
	if s == nil {
		return 0
	}
	return outputSizeHintInt(s.estimate.Load())
}

func (s *OutputSizeStats) Samples() uint64 {
	if s == nil {
		return 0
	}
	return s.samples.Load()
}

func (b *Bytecode) OutputGrowHint() int {
	if b == nil {
		return 0
	}
	if b.OutputSizeStats == nil {
		return outputSizeHintInt(uint64(b.StaticSize))
	}
	return b.OutputSizeStats.GrowHint(b.StaticSize)
}

func (b *Bytecode) ObserveOutputSize(actualSize int) {
	if b == nil || b.OutputSizeStats == nil {
		return
	}
	b.OutputSizeStats.Observe(actualSize)
}

func (b *Bytecode) LayoutOverheadGrowHint() int {
	if b == nil {
		return 0
	}
	if b.LayoutSizeStats == nil {
		return outputSizeHintInt(uint64(b.StaticSize))
	}
	return b.LayoutSizeStats.GrowHint(b.StaticSize)
}

func (b *Bytecode) PartialOutputGrowHint() int {
	if b == nil {
		return 0
	}
	if b.PartialSizeStats == nil {
		return outputSizeHintInt(uint64(b.StaticSize))
	}
	return b.PartialSizeStats.GrowHint(b.StaticSize)
}

func nextOutputSizeEstimate(current, actual, samples uint64) uint64 {
	if samples == 0 {
		return actual
	}
	if current == actual {
		return current
	}
	if current == 0 {
		return actual
	}
	if actual > current {
		limit := current * 4
		if limit < current {
			limit = math.MaxUint64
		}
		if actual > limit {
			actual = limit
		}
		delta := actual - current
		step := delta / 2
		if step == 0 {
			step = 1
		}
		return outputSizeHintUint64(current + step)
	}

	delta := current - actual
	step := delta / 8
	if step == 0 {
		step = 1
	}
	return outputSizeHintUint64(current - step)
}

func nextOutputSizeHeadroom(current, estimated, actual uint64) uint64 {
	if actual > estimated {
		shortfall := actual - estimated
		if shortfall > outputSizeStatsMaxHeadroom {
			shortfall = outputSizeStatsMaxHeadroom
		}
		if shortfall > current {
			return shortfall
		}
		return current
	}
	if current == 0 {
		return 0
	}
	step := current / 8
	if step == 0 {
		step = 1
	}
	return current - step
}

func outputSizeHintInt(value uint64) int {
	value = outputSizeHintUint64(value)
	if value > uint64(maxInt()) {
		return maxInt()
	}
	return int(value)
}

func outputSizeHintUint64(value uint64) uint64 {
	if value > outputSizeStatsMaxGrowHint {
		return outputSizeStatsMaxGrowHint
	}
	return value
}

func maxInt() int {
	return int(^uint(0) >> 1)
}
