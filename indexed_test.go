package deadline_test

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kvanticoss/deadline"
	"github.com/stretchr/testify/assert"
)

func setupIndex(
	numOfElements, createThreads int,
	nower func() time.Time,
	doneCallback func(),
) *deadline.Index {
	index := deadline.NewIndex()

	wgIds := sync.WaitGroup{}
	for i := 0; i < numOfElements; i++ {
		f := func(id int) {
			idStr := strconv.Itoa(id)

			options := []deadline.Option{
				deadline.WithCallback(doneCallback),
				deadline.WithNow(nower), // ensure we don't get races in our tests. Callbacks can not expire until nower progresses.
			}

			if id%2 == 0 {
				index.GetOrCreate(idStr, 5*time.Millisecond, options...)
			} else {
				index.PingOrCreate(idStr, 5*time.Millisecond, options...)
			}
		}

		// Simulate concurrent GetOrCreate
		for j := 0; j < createThreads; j++ {
			wgIds.Add(1)
			go func(id int) {
				f(id)
				wgIds.Done()
			}(i)
		}
	}

	// Ensure all concurrent GetOrCreate have been called.
	wgIds.Wait()

	return index
}

type testTimer struct {
	mutex sync.Mutex
	now   time.Time
}

func newTestTimer() *testTimer {
	return &testTimer{
		now: time.Now(),
	}
}

func (t *testTimer) Now() time.Time {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	return t.now
}

func (t *testTimer) Tick(d time.Duration) {
	t.mutex.Lock()
	t.now = t.now.Add(d)
	t.mutex.Unlock()
}

func TestGetOrCreate(t *testing.T) {

	testTimer := newTestTimer()

	var callbacks int64

	indices := 100
	index := setupIndex(indices, 10, testTimer.Now, func() {
		atomic.AddInt64(&callbacks, 1)
	})

	// Cancel half of them (ignores callbacks)
	index.CancelAll()

	// Allow callbacks to be triggered by progressing our timer
	testTimer.Tick(5 * time.Millisecond)

	assert.Equal(t, int64(0), atomic.LoadInt64(&callbacks), "Expected 0 of the callbacks to have been called after CancelAll")
}

func TestIndexedPing(t *testing.T) {

	testTimer := newTestTimer()

	var callbacks int64
	wgCallbacks := sync.WaitGroup{}

	indices := 100
	wgCallbacks.Add(indices)
	index := setupIndex(indices, 10, testTimer.Now, func() {
		atomic.AddInt64(&callbacks, 1)
		wgCallbacks.Done()
	})

	// Do some pings on existing and missing ids
	index.Get("1").Ping()
	index.Get("1000").Ping()

	// Allow callbacks to be triggered by progressing our timer
	testTimer.Tick(5 * time.Millisecond)

	// Wait for callbacks to finish, This will timeout the test or crash if not working properly
	wgCallbacks.Wait()

	assert.Equal(t, int64(indices), atomic.LoadInt64(&callbacks), "Expected all the callbacks to have been stopped")
}

func TestIndexedStop(t *testing.T) {

	testTimer := newTestTimer()

	var callbacks int64
	wgCallbacks := sync.WaitGroup{}

	indices := 100
	wgCallbacks.Add(indices)
	index := setupIndex(indices, 10, testTimer.Now, func() {
		atomic.AddInt64(&callbacks, 1)
		wgCallbacks.Done()
	})

	// Cancel half of them (ignores callbacks)
	for i := 0; i < indices/2; i++ {
		idStr := strconv.Itoa(i)
		index.Stop(idStr)
	}

	// Allow callbacks to be triggered by progressing our timer
	testTimer.Tick(5 * time.Millisecond)

	// Wait for callbacks to finish, This will timeout the test or crash if not working properly
	wgCallbacks.Wait()

	assert.Equal(t, int64(indices), atomic.LoadInt64(&callbacks), "Expected all the callbacks to have been stopped")
}

func TestIndexedStopAll(t *testing.T) {

	testTimer := newTestTimer()

	var callbacks int64
	wgCallbacks := sync.WaitGroup{}

	indices := 100
	wgCallbacks.Add(indices)
	index := setupIndex(indices, 10, testTimer.Now, func() {
		atomic.AddInt64(&callbacks, 1)
		wgCallbacks.Done()
	})

	// Cancel half of them (ignores callbacks)
	index.StopAll()

	// Allow callbacks to be triggered by progressing our timer
	testTimer.Tick(5 * time.Millisecond)

	// Wait for callbacks to finish, This will timeout the test or crash if not working properly
	wgCallbacks.Wait()

	assert.Equal(t, int64(indices), atomic.LoadInt64(&callbacks), "Expected all the callbacks to have been stopped")
}

func TestIndexedCancel(t *testing.T) {

	testTimer := newTestTimer()

	var callbacks int64
	wgCallbacks := sync.WaitGroup{}

	indices := 100
	wgCallbacks.Add(indices)
	index := setupIndex(indices, 10, testTimer.Now, func() {
		atomic.AddInt64(&callbacks, 1)
		wgCallbacks.Done()
	})

	// Cancel half of them (ignores callbacks)
	for i := 0; i < indices/2; i++ {
		idStr := strconv.Itoa(i)
		assert.False(t, index.Cancel(idStr), "no deadlines should have had time to trigger yet but %s wasn't found", idStr)
		wgCallbacks.Done()
	}

	// Allow callbacks to be triggered by progressing our timer
	testTimer.Tick(5 * time.Millisecond)

	// Wait for callbacks to finish, This will timeout the test or crash if not working properly
	wgCallbacks.Wait()

	assert.Equal(t, int64(indices/2), atomic.LoadInt64(&callbacks), "Expected half of the callbacks to have been called")
}

func TestIndexedCancelAll(t *testing.T) {

	testTimer := newTestTimer()

	var callbacks int64

	indices := 100
	index := setupIndex(indices, 10, testTimer.Now, func() {
		atomic.AddInt64(&callbacks, 1)
	})

	// Cancel half of them (ignores callbacks)
	index.CancelAll()

	// Allow callbacks to be triggered by progressing our timer
	testTimer.Tick(5 * time.Millisecond)

	assert.Equal(t, int64(0), atomic.LoadInt64(&callbacks), "Expected 0 of the callbacks to have been called after CancelAll")
}
