package compiler

import (
	"math"
	"strconv"
	"sync/atomic"
)

const layoutOutputSizeBucketCount = 12
const layoutOutputRatioScale = uint64(1 << 20)
const layoutOutputErrorScale = uint64(1_000_000)
const layoutOutputErrorMaxPPM = uint64(1_000_000_000)
const layoutOutputRefinementMinSamples = uint64(32)
const layoutOutputRefinementWarmSamples = uint64(4)
const layoutOutputRefinementMaxDepth = 3
const layoutOutputRefinementMinWidth = 8 << 10
const layoutOutputRefinementMaxChildren = int32(16)

const LayoutOutputPredictorAbsolute = "absolute"
const LayoutOutputPredictorRatio = "ratio"

type LayoutOutputSizePrediction struct {
	Predictor                 string
	Overhead                  int
	Absolute                  int
	Ratio                     int
	AbsoluteErrorPPM          uint64
	RatioErrorPPM             uint64
	Samples                   uint64
	RefinedBand               string
	RefinementDepth           int
	RefinementChildren        int
	RefinementFallback        bool
	RefinementFallbackMinimum int
	stats                     *layoutOutputSizeStats
}

type LayoutOutputSizeProfile struct {
	buckets            atomic.Pointer[layoutOutputSizeBuckets]
	refinementChildren atomic.Int32
}

type layoutOutputSizeBuckets struct {
	stats [layoutOutputSizeBucketCount]layoutOutputSizeStats
}

type layoutOutputSizeStats struct {
	output           OutputSizeStats
	ratio            atomic.Uint64
	absoluteErrorPPM atomic.Uint64
	ratioErrorPPM    atomic.Uint64
	refinement       atomic.Pointer[layoutOutputSizeRefinement]
}

type layoutOutputSizeRefinement struct {
	midpoint  int
	lowerName string
	upperName string
	lower     layoutOutputSizeStats
	upper     layoutOutputSizeStats
}

type layoutOutputSizeRange struct {
	lower int
	upper int
	name  string
}

type layoutOutputSizeSelection struct {
	stats           *layoutOutputSizeStats
	baseBand        string
	refinedBand     string
	lower           int
	upper           int
	depth           int
	fallbackMinimum int
}

func (p *LayoutOutputSizeProfile) Stats(yieldSize int) (*OutputSizeStats, string) {
	selection := p.layoutStats(yieldSize)
	if selection.stats == nil {
		return nil, selection.baseBand
	}
	return &selection.stats.output, selection.baseBand
}

func (p *LayoutOutputSizeProfile) Predict(yieldSize int) (*OutputSizeStats, LayoutOutputSizePrediction, string) {
	selection := p.layoutStats(yieldSize)
	if selection.stats == nil {
		return nil, LayoutOutputSizePrediction{}, selection.baseBand
	}
	prediction := selection.stats.predict(yieldSize)
	prediction.RefinedBand = selection.refinedBand
	prediction.RefinementDepth = selection.depth
	prediction.RefinementChildren = int(p.refinementChildren.Load())
	prediction.RefinementFallback = selection.depth > 0 && prediction.Samples < layoutOutputRefinementWarmSamples
	if prediction.RefinementFallback {
		prediction.RefinementFallbackMinimum = selection.fallbackMinimum
	}
	prediction.stats = selection.stats
	return &selection.stats.output, prediction, selection.baseBand
}

func (p LayoutOutputSizePrediction) AfterObservation(yieldSize int) LayoutOutputSizePrediction {
	if p.stats == nil {
		return p
	}
	updated := p.stats.predict(yieldSize)
	updated.RefinedBand = p.RefinedBand
	updated.RefinementDepth = p.RefinementDepth
	updated.RefinementChildren = p.RefinementChildren
	updated.RefinementFallback = p.RefinementFallback
	updated.RefinementFallbackMinimum = p.RefinementFallbackMinimum
	updated.stats = p.stats
	return updated
}

func (p *LayoutOutputSizeProfile) Observe(
	yieldSize int,
	actualOverhead int,
	headroomBase int,
	prediction LayoutOutputSizePrediction,
	trackHeadroom bool,
) {
	stats := prediction.stats
	if stats == nil {
		stats = p.layoutStats(yieldSize).stats
	}
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

func (p *LayoutOutputSizeProfile) layoutStats(yieldSize int) layoutOutputSizeSelection {
	if p == nil {
		return layoutOutputSizeSelection{}
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
	sizeRange := layoutOutputSizeRanges[index]
	selection := layoutOutputSizeSelection{
		stats:       &buckets.stats[index],
		baseBand:    name,
		refinedBand: name,
		lower:       sizeRange.lower,
		upper:       sizeRange.upper,
	}
	for {
		refinement := selection.stats.refinement.Load()
		if refinement == nil && p.shouldRefine(selection) {
			refinement = p.refine(selection)
		}
		if refinement == nil {
			return selection
		}
		selection.fallbackMinimum, _ = selection.stats.output.Range()
		selection.depth++
		if yieldSize <= refinement.midpoint {
			selection.stats = &refinement.lower
			selection.upper = refinement.midpoint
			selection.refinedBand = refinement.lowerName
			continue
		}
		selection.stats = &refinement.upper
		selection.lower = refinement.midpoint
		selection.refinedBand = refinement.upperName
	}
}

func (p *LayoutOutputSizeProfile) shouldRefine(selection layoutOutputSizeSelection) bool {
	if selection.stats == nil || selection.depth >= layoutOutputRefinementMaxDepth || selection.upper <= selection.lower {
		return false
	}
	if selection.upper-selection.lower < 2*layoutOutputRefinementMinWidth {
		return false
	}
	return selection.stats.output.Samples() >= layoutOutputRefinementMinSamples && selection.stats.output.Unstable()
}

func (p *LayoutOutputSizeProfile) refine(selection layoutOutputSizeSelection) *layoutOutputSizeRefinement {
	if refinement := selection.stats.refinement.Load(); refinement != nil {
		return refinement
	}
	if !p.reserveRefinementChildren() {
		return nil
	}
	midpoint := selection.lower + (selection.upper-selection.lower)/2
	candidate := &layoutOutputSizeRefinement{
		midpoint:  midpoint,
		lowerName: layoutOutputSizeRangeName(selection.lower, midpoint),
		upperName: layoutOutputSizeRangeName(midpoint, selection.upper),
	}
	if selection.stats.refinement.CompareAndSwap(nil, candidate) {
		return candidate
	}
	p.refinementChildren.Add(-2)
	return selection.stats.refinement.Load()
}

func (p *LayoutOutputSizeProfile) reserveRefinementChildren() bool {
	for {
		current := p.refinementChildren.Load()
		if current+2 > layoutOutputRefinementMaxChildren {
			return false
		}
		if p.refinementChildren.CompareAndSwap(current, current+2) {
			return true
		}
	}
}

func layoutOutputSizeRangeName(lower, upper int) string {
	return layoutOutputSizeBoundaryName(lower) + "-" + layoutOutputSizeBoundaryName(upper)
}

func layoutOutputSizeBoundaryName(size int) string {
	if size%(1<<20) == 0 {
		return strconv.Itoa(size>>20) + "m"
	}
	return strconv.Itoa(size>>10) + "k"
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

var layoutOutputSizeRanges = [layoutOutputSizeBucketCount]layoutOutputSizeRange{
	{lower: 0, upper: 4 << 10, name: "0-4k"},
	{lower: 4 << 10, upper: 16 << 10, name: "4k-16k"},
	{lower: 16 << 10, upper: 32 << 10, name: "16k-32k"},
	{lower: 32 << 10, upper: 64 << 10, name: "32k-64k"},
	{lower: 64 << 10, upper: 128 << 10, name: "64k-128k"},
	{lower: 128 << 10, upper: 192 << 10, name: "128k-192k"},
	{lower: 192 << 10, upper: 256 << 10, name: "192k-256k"},
	{lower: 256 << 10, upper: 384 << 10, name: "256k-384k"},
	{lower: 384 << 10, upper: 512 << 10, name: "384k-512k"},
	{lower: 512 << 10, upper: 1 << 20, name: "512k-1m"},
	{lower: 1 << 20, upper: 4 << 20, name: "1m-4m"},
	{lower: 4 << 20, name: "4m+"},
}

func layoutOutputSizeBucket(yieldSize int) (int, string) {
	index := 0
	switch {
	case yieldSize <= 4<<10:
		index = 0
	case yieldSize <= 16<<10:
		index = 1
	case yieldSize <= 32<<10:
		index = 2
	case yieldSize <= 64<<10:
		index = 3
	case yieldSize <= 128<<10:
		index = 4
	case yieldSize <= 192<<10:
		index = 5
	case yieldSize <= 256<<10:
		index = 6
	case yieldSize <= 384<<10:
		index = 7
	case yieldSize <= 512<<10:
		index = 8
	case yieldSize <= 1<<20:
		index = 9
	case yieldSize <= 4<<20:
		index = 10
	default:
		index = 11
	}
	return index, layoutOutputSizeRanges[index].name
}
