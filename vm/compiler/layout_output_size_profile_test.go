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
