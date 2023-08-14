// Package analytics provides structures and mechanisms to analyze performance.
package analytics

import "sync"

// timeCountPair is a pair/tuple of a Unix timestamp and a count.
type timeCountPair struct {
	ts    int64
	count uint
}

// Analytics holds information to measure performance.
type Analytics struct {
	totalCount uint
	timeCount  []timeCountPair

	concurrencySafe bool
	mutex           sync.RWMutex
}

// New returns an initialized Analytics.
func New(concurrencySafe bool) *Analytics {
	return &Analytics{
		timeCount: make([]timeCountPair, 2),

		concurrencySafe: concurrencySafe,
	}
}

// TotalCount returns the total analyzed count.
func (a *Analytics) TotalCount() uint {
	if a.concurrencySafe {
		a.mutex.RLock()
		defer a.mutex.RUnlock()
	}

	return a.totalCount
}

// CountPerSecond returns the last-known full rate of analyzed counts, per-second.
func (a *Analytics) CountPerSecond() uint {
	if a.concurrencySafe {
		a.mutex.RLock()
		defer a.mutex.RUnlock()
	}

	// Return the oldest data, as it's "complete"
	return a.timeCount[0].count
}

// IncrForTime takes a Unix timestamp and increments the internal total count
// AND tracks the count for the specified time. It returns the total analyzed
// count, and the analyzed count-per-second rate.
func (a *Analytics) IncrForTime(unixTime int64) (uint, uint) {
	if a.concurrencySafe {
		a.mutex.Lock()
		defer a.mutex.Unlock()
	}

	a.totalCount++
	a.setTimeCount(unixTime)

	return a.totalCount, a.timeCount[0].count
}

func (a *Analytics) setTimeCount(unixTime int64) {
	latestIndex := len(a.timeCount) - 1

	if a.timeCount[latestIndex].ts != unixTime {
		// Remove the oldest, add a latest
		a.timeCount = a.timeCount[1:]
		a.timeCount = append(a.timeCount, timeCountPair{ts: unixTime})
	}

	a.timeCount[latestIndex].count++
}
