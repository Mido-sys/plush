package compiler

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_LoopSizeStats_Learn_And_Adapt(t *testing.T) {
	var stats LoopSizeStats

	require.Zero(t, stats.BytesPerItem())
	require.Zero(t, stats.Samples())

	stats.Observe(100)
	require.Equal(t, 100, stats.BytesPerItem())
	require.Equal(t, uint64(1), stats.Samples())

	stats.Observe(300)
	require.Equal(t, 200, stats.BytesPerItem())

	stats.Observe(120)
	require.Equal(t, 190, stats.BytesPerItem())
	require.Equal(t, uint64(3), stats.Samples())

	stats.Observe(-1)
	require.Equal(t, uint64(3), stats.Samples())
}

func Test_LoopSizeStats_ConcurrentObserve(t *testing.T) {
	var stats LoopSizeStats
	var wg sync.WaitGroup

	const workers = 16
	const observations = 128
	wg.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func(offset int) {
			defer wg.Done()
			for i := 0; i < observations; i++ {
				stats.Observe(80 + offset + i%8)
			}
		}(worker)
	}
	wg.Wait()

	require.Equal(t, uint64(workers*observations), stats.Samples())
	require.Positive(t, stats.BytesPerItem())
}
