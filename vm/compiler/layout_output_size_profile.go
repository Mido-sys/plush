package compiler

import (
	"math"
	"sync/atomic"
)

const layoutOutputSizeBucketCount = 10
const layoutOutputRatioScale = uint64(1 << 20)
const layoutOutputErrorScale = uint64(1_000_000)
const layoutOutputErrorMaxPPM = uint64(1_000_000_000)

const LayoutOutputPredictorAbsolute = "absolute"
const LayoutOutputPredictorRatio = "ratio"

type LayoutOutputSizePrediction struct {
	Predictor        string
	Overhead         int
	Absolute         int
	Ratio            int
	AbsoluteErrorPPM uint64
	RatioErrorPPM    uint64
	Samples          uint64
}

type LayoutOutputSizeProfile struct {
	buckets atomic.Pointer[layoutOutputSizeBuckets]
}

type layoutOutputSizeBuckets struct {
	stats [layoutOutputSizeBucketCount]layoutOutputSizeStats
}

type layoutOutputSizeStats struct {
	output           OutputSizeStats
	ratio            atomic.Uint64
	absoluteErrorPPM atomic.Uint64
	ratioErrorPPM    atomic.Uint64
}

func (p *LayoutOutputSizeProfile) Stats(yieldSize int) (*OutputSizeStats, string) {
	stats, name := p.layoutStats(yieldSize)
	if stats == nil {
		return nil, name
	}
	return &stats.output, name
}

func (p *LayoutOutputSizeProfile) Predict(yieldSize int) (*OutputSizeStats, LayoutOutputSizePrediction, string) {
	stats, name := p.layoutStats(yieldSize)
	if stats == nil {
		return nil, LayoutOutputSizePrediction{}, name
	}
	return &stats.output, stats.predict(yieldSize), name
}

func (p *LayoutOutputSizeProfile) Observe(
	yieldSize int,
	actualOverhead int,
	headroomBase int,
	prediction LayoutOutputSizePrediction,
	trackHeadroom bool,
) {
	stats, _ := p.layoutStats(yieldSize)
	if stats == nil || actualOverhead <= 0 {
		return
	}
	stats.observePrediction(yieldSize, actualOverhead, prediction)
	if trackHeadroom {
		stats.output.ObserveWithHeadroom(actualOverhead, headroomBase)
		return
	}
	stats.output.Observe(actualOverhead)
}

func (p *LayoutOutputSizeProfile) layoutStats(yieldSize int) (*layoutOutputSizeStats, string) {
	if p == nil {
		return nil, ""
	}
	buckets := p.buckets.Load()
	if buckets == nil {
		candidate := &layoutOutputSizeBuckets{}
		if p.buckets.CompareAndSwap(nil, candidate) {
			buckets = candidate
		} else {
			buckets = p.buckets.Load()
		}
	}
	index, name := layoutOutputSizeBucket(yieldSize)
	return &buckets.stats[index], name
}

func (s *layoutOutputSizeStats) predict(yieldSize int) LayoutOutputSizePrediction {
	prediction := LayoutOutputSizePrediction{
		Predictor:        LayoutOutputPredictorAbsolute,
		Absolute:         s.output.Estimate(),
		AbsoluteErrorPPM: s.absoluteErrorPPM.Load(),
		RatioErrorPPM:    s.ratioErrorPPM.Load(),
		Samples:          s.output.Samples(),
	}
	prediction.Ratio = layoutRatioOverhead(yieldSize, s.ratio.Load())
	prediction.Overhead = prediction.Absolute
	if prediction.Samples >= 2 && prediction.Ratio > 0 && layoutRatioPredictorClearlyBetter(prediction.AbsoluteErrorPPM, prediction.RatioErrorPPM) {
		prediction.Predictor = LayoutOutputPredictorRatio
		prediction.Overhead = prediction.Ratio
	}
	return prediction
}

func (s *layoutOutputSizeStats) observePrediction(yieldSize, actualOverhead int, prediction LayoutOutputSizePrediction) {
	if prediction.Samples > 0 {
		errorSamples := prediction.Samples - 1
		updateLayoutPredictionError(
			&s.absoluteErrorPPM,
			layoutPredictionErrorPPM(yieldSize, prediction.Absolute, actualOverhead),
			errorSamples,
		)
		if prediction.Ratio > 0 {
			updateLayoutPredictionError(
				&s.ratioErrorPPM,
				layoutPredictionErrorPPM(yieldSize, prediction.Ratio, actualOverhead),
				errorSamples,
			)
		}
	}
	if yieldSize <= 0 {
		return
	}
	actualRatio := layoutOutputRatio(actualOverhead, yieldSize)
	for {
		current := s.ratio.Load()
		next := nextLayoutOutputRatio(current, actualRatio, prediction.Samples)
		if s.ratio.CompareAndSwap(current, next) {
			return
		}
	}
}

func updateLayoutPredictionError(score *atomic.Uint64, actual uint64, samples uint64) {
	for {
		current := score.Load()
		next := nextLayoutPredictionError(current, actual, samples)
		if score.CompareAndSwap(current, next) {
			return
		}
	}
}

func nextLayoutPredictionError(current, actual, samples uint64) uint64 {
	if samples == 0 {
		return actual
	}
	if current == actual {
		return current
	}
	if actual > current {
		step := (actual - current) / 8
		if step == 0 {
			step = 1
		}
		return current + step
	}
	step := (current - actual) / 8
	if step == 0 {
		step = 1
	}
	return current - step
}

func nextLayoutOutputRatio(current, actual, samples uint64) uint64 {
	if samples == 0 || current == 0 {
		return actual
	}
	if current == actual {
		return current
	}
	if actual > current {
		limit := current * 4
		if limit < current {
			limit = math.MaxUint64
		}
		if actual > limit {
			actual = limit
		}
		step := (actual - current) / 8
		if step == 0 {
			step = 1
		}
		return current + step
	}
	step := (current - actual) / 8
	if step == 0 {
		step = 1
	}
	return current - step
}

func layoutOutputRatio(overhead, yieldSize int) uint64 {
	if overhead <= 0 || yieldSize <= 0 {
		return 0
	}
	value := outputSizeHintUint64(uint64(overhead))
	return value * layoutOutputRatioScale / uint64(yieldSize)
}

func layoutRatioOverhead(yieldSize int, ratio uint64) int {
	if yieldSize <= 0 || ratio == 0 {
		return 0
	}
	yield := uint64(yieldSize)
	maximum := uint64(outputSizeStatsMaxGrowHint)
	if ratio > maximum*layoutOutputRatioScale/yield {
		return outputSizeStatsMaxGrowHint
	}
	return outputSizeHintInt(yield * ratio / layoutOutputRatioScale)
}

func layoutPredictionErrorPPM(yieldSize, predictedOverhead, actualOverhead int) uint64 {
	predicted := layoutOutputTotal(yieldSize, predictedOverhead)
	actual := layoutOutputTotal(yieldSize, actualOverhead)
	if actual == 0 {
		return 0
	}
	difference := predicted
	if actual > predicted {
		difference = actual - predicted
	} else {
		difference -= actual
	}
	if difference > math.MaxUint64/layoutOutputErrorScale {
		return layoutOutputErrorMaxPPM
	}
	errorPPM := difference * layoutOutputErrorScale / actual
	if errorPPM > layoutOutputErrorMaxPPM {
		return layoutOutputErrorMaxPPM
	}
	return errorPPM
}

func layoutOutputTotal(yieldSize, overhead int) uint64 {
	if yieldSize < 0 {
		yieldSize = 0
	}
	if overhead < 0 {
		overhead = 0
	}
	return uint64(yieldSize) + uint64(overhead)
}

func layoutRatioPredictorClearlyBetter(absoluteErrorPPM, ratioErrorPPM uint64) bool {
	return ratioErrorPPM*8 < absoluteErrorPPM*7
}

func layoutOutputSizeBucket(yieldSize int) (int, string) {
	switch {
	case yieldSize <= 4<<10:
		return 0, "0-4k"
	case yieldSize <= 16<<10:
		return 1, "4k-16k"
	case yieldSize <= 32<<10:
		return 2, "16k-32k"
	case yieldSize <= 64<<10:
		return 3, "32k-64k"
	case yieldSize <= 128<<10:
		return 4, "64k-128k"
	case yieldSize <= 256<<10:
		return 5, "128k-256k"
	case yieldSize <= 512<<10:
		return 6, "256k-512k"
	case yieldSize <= 1<<20:
		return 7, "512k-1m"
	case yieldSize <= 4<<20:
		return 8, "1m-4m"
	default:
		return 9, "4m+"
	}
}
