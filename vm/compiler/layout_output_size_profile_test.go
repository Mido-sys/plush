package compiler

import (
	"sync"
	"testing"

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
}

func Test_LayoutOutputSizeProfile_Bands(t *testing.T) {
	tests := []struct {
		yieldSize int
		band      string
	}{
		{yieldSize: 0, band: "0-4k"},
		{yieldSize: 4 << 10, band: "0-4k"},
		{yieldSize: (4 << 10) + 1, band: "4k-16k"},
		{yieldSize: 32 << 10, band: "16k-32k"},
		{yieldSize: (32 << 10) + 1, band: "32k-64k"},
		{yieldSize: 4 << 20, band: "1m-4m"},
		{yieldSize: (4 << 20) + 1, band: "4m+"},
	}

	var profile LayoutOutputSizeProfile
	for _, test := range tests {
		_, band := profile.Stats(test.yieldSize)
		require.Equal(t, test.band, band)
	}
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
