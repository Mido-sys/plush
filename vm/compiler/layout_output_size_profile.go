package compiler

import "sync/atomic"

const layoutOutputSizeBucketCount = 8

type LayoutOutputSizeProfile struct {
	buckets atomic.Pointer[layoutOutputSizeBuckets]
}

type layoutOutputSizeBuckets struct {
	stats [layoutOutputSizeBucketCount]OutputSizeStats
}

func (p *LayoutOutputSizeProfile) Stats(yieldSize int) (*OutputSizeStats, string) {
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
	case yieldSize <= 256<<10:
		return 4, "64k-256k"
	case yieldSize <= 1<<20:
		return 5, "256k-1m"
	case yieldSize <= 4<<20:
		return 6, "1m-4m"
	default:
		return 7, "4m+"
	}
}
