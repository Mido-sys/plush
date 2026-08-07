package compiler

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_OutputSizeStats_GrowHint_Observe_And_Slow_Decay(t *testing.T) {
	var stats OutputSizeStats

	require.Equal(t, 12, stats.GrowHint(12))
	require.Zero(t, stats.Estimate())
	require.Zero(t, stats.Samples())
	stats.Observe(0)
	stats.Observe(-1)
	require.Zero(t, stats.Estimate())
	require.Zero(t, stats.Samples())

	stats.Observe(100)
	require.Equal(t, 100, stats.Estimate())
	require.Equal(t, uint64(1), stats.Samples())
	require.Equal(t, 100, stats.GrowHint(12))

	stats.Observe(300)
	require.Equal(t, 200, stats.Estimate())
	require.Equal(t, uint64(2), stats.Samples())

	stats.Observe(120)
	require.Equal(t, 190, stats.Estimate())
	require.Equal(t, uint64(3), stats.Samples())
}

func Test_OutputSizeStats_Headroom_Tracks_Underestimates_And_Decays(t *testing.T) {
	var stats OutputSizeStats

	stats.Observe(100)
	require.Zero(t, stats.Headroom(stats.GrowHint(0)))

	stats.ObserveWithHeadroom(110, 100)
	require.Equal(t, 105, stats.Estimate())
	require.Equal(t, 10, stats.Headroom(stats.GrowHint(0)))

	stats.ObserveWithHeadroom(105, 115)
	require.Equal(t, 9, stats.Headroom(stats.GrowHint(0)))
}

func Test_OutputSizeStats_Headroom_Is_Bounded_By_Percentage_And_Absolute_Limits(t *testing.T) {
	var stats OutputSizeStats

	stats.Observe(1_000)
	stats.ObserveWithHeadroom(200_000, 1_000)

	require.Equal(t, 100, stats.Headroom(1_000))
	require.Equal(t, outputSizeStatsMaxHeadroom, stats.Headroom(1<<20))
}

func Test_OutputSizeStats_Outlier_And_Hint_Caps(t *testing.T) {
	var stats OutputSizeStats

	stats.Observe(100)
	stats.Observe(100_000)
	require.Equal(t, 250, stats.Estimate())
	require.Equal(t, uint64(2), stats.Samples())

	stats.Observe(outputSizeStatsMaxGrowHint * 2)
	require.LessOrEqual(t, stats.Estimate(), outputSizeStatsMaxGrowHint)
	require.Equal(t, outputSizeStatsMaxGrowHint, stats.GrowHint(outputSizeStatsMaxGrowHint*2))
}

func Test_OutputSizeStats_ConcurrentObserve(t *testing.T) {
	var stats OutputSizeStats
	var wg sync.WaitGroup

	const workers = 16
	const observations = 128
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < observations; i++ {
				stats.Observe(100 + offset + i%8)
			}
		}(worker)
	}
	wg.Wait()

	require.Equal(t, uint64(workers*observations), stats.Samples())
	require.Positive(t, stats.Estimate())
	require.LessOrEqual(t, stats.Estimate(), outputSizeStatsMaxGrowHint)
	minimum, maximum := stats.Range()
	require.Equal(t, 100, minimum)
	require.Equal(t, 122, maximum)
	require.False(t, stats.Unstable())
}

func Test_OutputSizeStats_ConcurrentHeadroomObserve(t *testing.T) {
	var stats OutputSizeStats
	stats.Observe(1_000)

	var wg sync.WaitGroup
	const workers = 16
	const observations = 128
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < observations; i++ {
				stats.ObserveWithHeadroom(1_000+offset+i%8, 1_000)
			}
		}(worker)
	}
	wg.Wait()

	require.Equal(t, uint64(1+workers*observations), stats.Samples())
	require.Positive(t, stats.Headroom(stats.Estimate()))
	require.LessOrEqual(t, stats.Headroom(stats.Estimate()), stats.Estimate()/outputSizeStatsHeadroomDivisor)
}

func Test_OutputSizeStats_Detects_Unstable_Output_Range(t *testing.T) {
	var stats OutputSizeStats

	stats.Observe(100)
	require.False(t, stats.Unstable())
	stats.Observe(399)
	require.False(t, stats.Unstable())
	stats.Observe(400)
	require.True(t, stats.Unstable())
	require.Equal(t, 324, stats.Estimate())
	minimum, maximum := stats.Range()
	require.Equal(t, 100, minimum)
	require.Equal(t, 400, maximum)
}
