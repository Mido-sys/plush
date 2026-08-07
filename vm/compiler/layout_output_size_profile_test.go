package compiler

import (
	"strconv"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func Test_LayoutOutputSizeProfile_Isolates_Yield_Size_Bands(t *testing.T) {
	var profile LayoutOutputSizeProfile

	small, smallBand := profile.Stats(31_694)
	medium, mediumBand := profile.Stats(35_179)
	require.Equal(t, "16k-32k", smallBand)
	require.Equal(t, "32k-64k", mediumBand)
	require.NotSame(t, small, medium)

	small.Observe(293_089)
	medium.Observe(358_432)
	require.Equal(t, 293_089, small.Estimate())
	require.Equal(t, 358_432, medium.Estimate())

	sameSmall, sameSmallBand := profile.Stats(20_000)
	require.Same(t, small, sameSmall)
	require.Equal(t, smallBand, sameSmallBand)

	lowerLarge, lowerLargeBand := profile.Stats(96 << 10)
	middleLarge, middleLargeBand := profile.Stats(191 << 10)
	upperLarge, upperLargeBand := profile.Stats(232 << 10)
	lowerExtraLarge, lowerExtraLargeBand := profile.Stats(300 << 10)
	upperExtraLarge, upperExtraLargeBand := profile.Stats(450 << 10)
	require.Equal(t, "64k-128k", lowerLargeBand)
	require.Equal(t, "128k-192k", middleLargeBand)
	require.Equal(t, "192k-256k", upperLargeBand)
	require.Equal(t, "256k-384k", lowerExtraLargeBand)
	require.Equal(t, "384k-512k", upperExtraLargeBand)
	require.NotSame(t, lowerLarge, upperLarge)
	require.NotSame(t, middleLarge, upperLarge)
	require.NotSame(t, lowerExtraLarge, upperExtraLarge)
}

func Test_LayoutOutputSizeProfile_Bands(t *testing.T) {
	tests := []struct {
		yieldSize int
		band      string
	}{
		{yieldSize: 0, band: "0-4k"},
		{yieldSize: 4 << 10, band: "0-4k"},
		{yieldSize: (4 << 10) + 1, band: "4k-16k"},
		{yieldSize: 16 << 10, band: "4k-16k"},
		{yieldSize: (16 << 10) + 1, band: "16k-32k"},
		{yieldSize: 32 << 10, band: "16k-32k"},
		{yieldSize: (32 << 10) + 1, band: "32k-64k"},
		{yieldSize: 64 << 10, band: "32k-64k"},
		{yieldSize: (64 << 10) + 1, band: "64k-128k"},
		{yieldSize: 128 << 10, band: "64k-128k"},
		{yieldSize: (128 << 10) + 1, band: "128k-192k"},
		{yieldSize: 192 << 10, band: "128k-192k"},
		{yieldSize: (192 << 10) + 1, band: "192k-256k"},
		{yieldSize: 256 << 10, band: "192k-256k"},
		{yieldSize: (256 << 10) + 1, band: "256k-384k"},
		{yieldSize: 384 << 10, band: "256k-384k"},
		{yieldSize: (384 << 10) + 1, band: "384k-512k"},
		{yieldSize: 512 << 10, band: "384k-512k"},
		{yieldSize: (512 << 10) + 1, band: "512k-1m"},
		{yieldSize: 1 << 20, band: "512k-1m"},
		{yieldSize: (1 << 20) + 1, band: "1m-4m"},
		{yieldSize: 4 << 20, band: "1m-4m"},
		{yieldSize: (4 << 20) + 1, band: "4m+"},
	}

	var profile LayoutOutputSizeProfile
	for _, test := range tests {
		_, band := profile.Stats(test.yieldSize)
		require.Equal(t, test.band, band)
	}
}

func Test_NextLayoutOutputRatio_Moves_Symmetrically(t *testing.T) {
	require.Equal(t, uint64(90), nextLayoutOutputRatio(80, 160, 1))
	require.Equal(t, uint64(150), nextLayoutOutputRatio(160, 80, 1))
	require.Equal(t, uint64(81), nextLayoutOutputRatio(80, 81, 1))
	require.Equal(t, uint64(79), nextLayoutOutputRatio(80, 79, 1))

	// Upward outliers are capped at four times the current ratio before moving.
	require.Equal(t, uint64(137), nextLayoutOutputRatio(100, 1_000, 1))
}

func Test_LayoutOutputSizeProfile_Selects_Ratio_For_Proportional_Overhead(t *testing.T) {
	var profile LayoutOutputSizeProfile

	_, first, _ := profile.Predict(100)
	require.Equal(t, LayoutOutputPredictorAbsolute, first.Predictor)
	profile.Observe(100, 100, first.Overhead, first, false)

	_, second, _ := profile.Predict(200)
	require.Equal(t, 100, second.Absolute)
	require.Equal(t, 200, second.Ratio)
	require.Equal(t, LayoutOutputPredictorAbsolute, second.Predictor)
	profile.Observe(200, 200, second.Overhead, second, false)

	_, third, _ := profile.Predict(300)
	require.Equal(t, LayoutOutputPredictorRatio, third.Predictor)
	require.Equal(t, 150, third.Absolute)
	require.Equal(t, 300, third.Ratio)
	require.Equal(t, 300, third.Overhead)
	require.Equal(t, uint64(250_000), third.AbsoluteErrorPPM)
	require.Zero(t, third.RatioErrorPPM)
}

func Test_LayoutOutputSizeProfile_Keeps_Absolute_For_Fixed_Overhead(t *testing.T) {
	var profile LayoutOutputSizeProfile

	_, first, _ := profile.Predict(100)
	profile.Observe(100, 100, first.Overhead, first, false)
	_, second, _ := profile.Predict(200)
	profile.Observe(200, 100, second.Overhead, second, false)

	_, third, _ := profile.Predict(300)
	require.Equal(t, LayoutOutputPredictorAbsolute, third.Predictor)
	require.Equal(t, 100, third.Absolute)
	require.Equal(t, 281, third.Ratio)
	require.Equal(t, 100, third.Overhead)
	require.Zero(t, third.AbsoluteErrorPPM)
	require.Equal(t, uint64(333_333), third.RatioErrorPPM)
}

func Test_LayoutOutputSizeProfile_Ratio_Requires_Clear_Error_Advantage(t *testing.T) {
	require.False(t, layoutRatioPredictorClearlyBetter(100, 88))
	require.True(t, layoutRatioPredictorClearlyBetter(100, 87))
}

func Test_LayoutOutputSizeProfile_Returns_To_Absolute_When_Output_Changes(t *testing.T) {
	var profile LayoutOutputSizeProfile

	for _, size := range []int{100, 200} {
		_, prediction, _ := profile.Predict(size)
		profile.Observe(size, size, prediction.Overhead, prediction, false)
	}
	_, proportional, _ := profile.Predict(300)
	require.Equal(t, LayoutOutputPredictorRatio, proportional.Predictor)

	for i := 0; i < 24; i++ {
		_, prediction, _ := profile.Predict(300)
		profile.Observe(300, 100, prediction.Overhead, prediction, false)
	}
	_, fixed, _ := profile.Predict(300)
	require.Equal(t, LayoutOutputPredictorAbsolute, fixed.Predictor)
	require.Less(t, fixed.AbsoluteErrorPPM, fixed.RatioErrorPPM)
}

func Test_LayoutOutputSizeProfile_Prediction_Error_Uses_Complete_Output(t *testing.T) {
	require.Equal(t, uint64(250_000), layoutPredictionErrorPPM(200, 100, 200))
	require.Zero(t, layoutPredictionErrorPPM(200, 200, 200))
	require.Zero(t, layoutPredictionErrorPPM(0, 0, 0))
}

func Test_LayoutOutputSizeProfile_ConcurrentStats(t *testing.T) {
	var profile LayoutOutputSizeProfile
	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			stats, _ := profile.Stats(50_796)
			stats.Observe(379_151)
		}()
	}
	wg.Wait()

	stats, _ := profile.Stats(50_796)
	require.Equal(t, uint64(workers), stats.Samples())
	require.Equal(t, 379_151, stats.Estimate())
}

func Test_LayoutOutputSizeProfile_ConcurrentPrediction(t *testing.T) {
	var profile LayoutOutputSizeProfile
	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(offset int) {
			defer wg.Done()
			yieldSize := 64_000 + offset
			_, prediction, _ := profile.Predict(yieldSize)
			profile.Observe(yieldSize, yieldSize*2, prediction.Overhead, prediction, false)
		}(i)
	}
	wg.Wait()

	stats, prediction, _ := profile.Predict(64_000)
	require.Equal(t, uint64(workers), stats.Samples())
	require.Positive(t, prediction.Absolute)
	require.Positive(t, prediction.Ratio)
}

func Test_LayoutOutputSizeProfile_Refines_Unstable_Band_And_Warms_Children(t *testing.T) {
	var profile LayoutOutputSizeProfile
	const lowerYield = 200 << 10
	const upperYield = 248 << 10
	const lowerOverhead = 100 << 10
	const upperOverhead = 800 << 10

	for i := 0; i < int(layoutOutputRefinementMinSamples); i++ {
		yieldSize, overhead := lowerYield, lowerOverhead
		if i%2 == 1 {
			yieldSize, overhead = upperYield, upperOverhead
		}
		observeLayoutOutputSize(&profile, yieldSize, overhead)
	}

	lowerStats, lowerPrediction, lowerBand := profile.Predict(lowerYield)
	upperStats, upperPrediction, upperBand := profile.Predict(upperYield)
	require.Equal(t, "192k-256k", lowerBand)
	require.Equal(t, lowerBand, upperBand)
	require.Equal(t, "192k-224k", lowerPrediction.RefinedBand)
	require.Equal(t, "224k-256k", upperPrediction.RefinedBand)
	require.Equal(t, 1, lowerPrediction.RefinementDepth)
	require.Equal(t, 1, upperPrediction.RefinementDepth)
	require.True(t, lowerPrediction.RefinementFallback)
	require.True(t, upperPrediction.RefinementFallback)
	require.Equal(t, lowerOverhead, lowerPrediction.RefinementFallbackMinimum)
	require.NotSame(t, lowerStats, upperStats)
	require.Equal(t, int32(2), profile.refinementChildren.Load())

	for i := uint64(0); i < layoutOutputRefinementWarmSamples; i++ {
		_, prediction, _ := profile.Predict(lowerYield)
		require.True(t, prediction.RefinementFallback)
		observeLayoutOutputSize(&profile, lowerYield, 300<<10)
	}
	_, warmed, _ := profile.Predict(lowerYield)
	require.False(t, warmed.RefinementFallback)
	require.Equal(t, layoutOutputRefinementWarmSamples, warmed.Samples)
	require.Equal(t, 300<<10, warmed.Overhead)
}

func Test_LayoutOutputSizeProfile_Refinement_Depth_Is_Bounded(t *testing.T) {
	var profile LayoutOutputSizeProfile
	seedUnstableLayoutRange(t, &profile, 226<<10, 250<<10)

	_, first, _ := profile.Predict(226 << 10)
	require.Equal(t, 1, first.RefinementDepth)
	require.Equal(t, "224k-256k", first.RefinedBand)

	seedUnstableLayoutRange(t, &profile, 226<<10, 238<<10)
	_, second, _ := profile.Predict(226 << 10)
	require.Equal(t, 2, second.RefinementDepth)
	require.Equal(t, "224k-240k", second.RefinedBand)

	seedUnstableLayoutRange(t, &profile, 226<<10, 230<<10)
	_, third, _ := profile.Predict(226 << 10)
	require.Equal(t, layoutOutputRefinementMaxDepth, third.RefinementDepth)
	require.Equal(t, "224k-232k", third.RefinedBand)

	seedUnstableLayoutRange(t, &profile, 226<<10, 227<<10)
	_, bounded, _ := profile.Predict(226 << 10)
	require.Equal(t, layoutOutputRefinementMaxDepth, bounded.RefinementDepth)
	require.Equal(t, "224k-232k", bounded.RefinedBand)
	require.Equal(t, int32(6), profile.refinementChildren.Load())
}

func Test_LayoutOutputSizeProfile_Refinement_Child_Budget_Is_Bounded(t *testing.T) {
	var profile LayoutOutputSizeProfile
	yields := []int{
		24 << 10,
		48 << 10,
		96 << 10,
		160 << 10,
		224 << 10,
		320 << 10,
		448 << 10,
		768 << 10,
		2 << 20,
	}

	for i, yieldSize := range yields {
		for sample := 0; sample < int(layoutOutputRefinementMinSamples); sample++ {
			overhead := 64 << 10
			if sample%2 == 1 {
				overhead = 512 << 10
			}
			observeLayoutOutputSize(&profile, yieldSize, overhead)
		}
		_, prediction, _ := profile.Predict(yieldSize)
		if i < int(layoutOutputRefinementMaxChildren/2) {
			require.Equal(t, 1, prediction.RefinementDepth)
		} else {
			require.Zero(t, prediction.RefinementDepth)
		}
	}

	require.Equal(t, layoutOutputRefinementMaxChildren, profile.refinementChildren.Load())
}

func Test_LayoutOutputSizeProfile_Concurrent_Refinement_Installs_Once(t *testing.T) {
	var profile LayoutOutputSizeProfile
	seedUnstableLayoutRange(t, &profile, 200<<10, 248<<10)

	const workers = 64
	stats := make(chan *layoutOutputSizeStats, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_, prediction, _ := profile.Predict(200 << 10)
			stats <- prediction.stats
		}()
	}
	wg.Wait()
	close(stats)

	var selected *layoutOutputSizeStats
	for current := range stats {
		if selected == nil {
			selected = current
			continue
		}
		require.Same(t, selected, current)
	}
	require.Equal(t, int32(2), profile.refinementChildren.Load())
}

func Test_LayoutOutputSizeProfile_In_Flight_Observation_Stays_On_Selected_Parent(t *testing.T) {
	var profile LayoutOutputSizeProfile
	for i := 0; i < int(layoutOutputRefinementMinSamples)-1; i++ {
		yieldSize, overhead := 200<<10, 64<<10
		if i%2 == 1 {
			yieldSize, overhead = 248<<10, 512<<10
		}
		observeLayoutOutputSize(&profile, yieldSize, overhead)
	}

	_, inFlight, _ := profile.Predict(200 << 10)
	_, threshold, _ := profile.Predict(248 << 10)
	profile.Observe(248<<10, 512<<10, threshold.Overhead, threshold, false)

	childStats, childPrediction, _ := profile.Predict(200 << 10)
	require.Equal(t, 1, childPrediction.RefinementDepth)
	require.Zero(t, childStats.Samples())

	profile.Observe(200<<10, 64<<10, inFlight.Overhead, inFlight, false)
	require.Equal(t, layoutOutputRefinementMinSamples+1, inFlight.stats.output.Samples())
	require.Zero(t, childStats.Samples())
	updated := inFlight.AfterObservation(200 << 10)
	require.Equal(t, layoutOutputRefinementMinSamples+1, updated.Samples)
	require.Positive(t, updated.Absolute)
}

func Test_LayoutOutputSizeProfile_Memory_Bounds_On_64_Bit(t *testing.T) {
	if strconv.IntSize != 64 {
		t.Skip("64-bit structural size assertion")
	}

	require.Equal(t, uintptr(16), unsafe.Sizeof(LayoutOutputSizeProfile{}))
	require.Equal(t, uintptr(864), unsafe.Sizeof(layoutOutputSizeBuckets{}))
	require.Equal(t, uintptr(184), unsafe.Sizeof(layoutOutputSizeRefinement{}))
}

func Test_LayoutOutputSizeProfile_Steady_State_Prediction_Does_Not_Allocate(t *testing.T) {
	var stable LayoutOutputSizeProfile
	for i := 0; i < 8; i++ {
		observeLayoutOutputSize(&stable, 200<<10, 300<<10)
	}
	require.Zero(t, testing.AllocsPerRun(1_000, func() {
		stable.Predict(200 << 10)
	}))

	var refined LayoutOutputSizeProfile
	seedUnstableLayoutRange(t, &refined, 200<<10, 248<<10)
	refined.Predict(200 << 10)
	require.Zero(t, testing.AllocsPerRun(1_000, func() {
		refined.Predict(200 << 10)
	}))
}

func observeLayoutOutputSize(profile *LayoutOutputSizeProfile, yieldSize, overhead int) {
	_, prediction, _ := profile.Predict(yieldSize)
	profile.Observe(yieldSize, overhead, prediction.Overhead, prediction, false)
}

func seedUnstableLayoutRange(t *testing.T, profile *LayoutOutputSizeProfile, lowerYield, upperYield int) {
	t.Helper()
	for i := 0; i < int(layoutOutputRefinementMinSamples); i++ {
		yieldSize, overhead := lowerYield, 64<<10
		if i%2 == 1 {
			yieldSize, overhead = upperYield, 512<<10
		}
		observeLayoutOutputSize(profile, yieldSize, overhead)
	}
}
