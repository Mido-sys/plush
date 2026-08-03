package compiler

import "sync/atomic"

type LoopSizeStats struct {
	bytesPerItem atomic.Uint64
	samples      atomic.Uint64
}

func (s *LoopSizeStats) Observe(bytesPerItem int) {
	if s == nil || bytesPerItem < 0 {
		return
	}
	actual := outputSizeHintUint64(uint64(bytesPerItem))
	for {
		current := s.bytesPerItem.Load()
		next := nextOutputSizeEstimate(current, actual, s.samples.Load())
		if s.bytesPerItem.CompareAndSwap(current, next) {
			s.samples.Add(1)
			return
		}
	}
}

func (s *LoopSizeStats) BytesPerItem() int {
	if s == nil {
		return 0
	}
	return outputSizeHintInt(s.bytesPerItem.Load())
}

func (s *LoopSizeStats) Samples() uint64 {
	if s == nil {
		return 0
	}
	return s.samples.Load()
}
