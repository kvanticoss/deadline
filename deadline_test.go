package deadline_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kvanticoss/deadline"
)

func TestDeadLineExpiration(t *testing.T) {
	// Ensure callback is called
	callbackCalled := make(chan struct{})
	callback2Called := make(chan struct{})

	deadline.New(1*time.Nanosecond,
		deadline.WithCallback(func() {
			callbackCalled <- struct{}{}
		}),
		deadline.WithCallback(func() {
			callback2Called <- struct{}{}
		}),
	)

	<-callbackCalled
	<-callback2Called
}

func TestDeadLineCancel(t *testing.T) {
	// Ensure callback is called
	callbackCalled := false

	dl := deadline.New(10*time.Millisecond,
		deadline.WithCallback(func() {
			callbackCalled = true
		}),
	)

	// Cancel shouldn't trigger the callback
	assert.Equal(t, dl.Cancel(), false)
	time.Sleep(15 * time.Millisecond)
	assert.Equal(t, callbackCalled, false)
}

func TestDeadLineStop(t *testing.T) {
	// Ensure callback is called
	callbackCalled := make(chan struct{})
	dl := deadline.New(10*time.Millisecond,
		deadline.WithCallback(func() {
			callbackCalled <- struct{}{}
		}),
	)

	// Stop should trigger the callback
	dl.Stop()
	<-callbackCalled
}

func TestDeadLineDone(t *testing.T) {
	// Ensure callback is called
	var dl *deadline.Deadline
	dl = deadline.New(10*time.Millisecond,
		deadline.WithCallback(func() {
			assert.Equal(t, dl.Done(), true)
		}),
	)

	// this test is racy...
	assert.Equal(t, dl.Done(), false)

	// Stop should trigger the callback
	dl.Stop()
}

func TestDeadLineCtx(t *testing.T) {
	// Ensure callback is called
	dl := deadline.New(10*time.Millisecond,
		deadline.WithCallback(func() {}),
	)

	// Stop should trigger the callback
	dl.Stop()

	// tests will timeout if this doesn't work
	<-dl.Ctx().Done()
}

func TestDeadLineWithCtx(t *testing.T) {

	ctx, cancel := context.WithCancel(context.Background())

	// Ensure callback is called
	callbackCalled := make(chan struct{})
	dl := deadline.New(10*time.Hour,
		deadline.WithCallback(func() {
			callbackCalled <- struct{}{}
		}),
		deadline.WithContext(ctx),
	)

	assert.Equal(t, dl.Done(), false)
	cancel()
	assert.Equal(t, dl.Done(), true)

	// tests will timeout if this doesn't work
	<-callbackCalled
}

func TestDeadLineResetIncreased(t *testing.T) {

	// Ensure callback is called
	callbackCalled := make(chan struct{})
	dl := deadline.New(1*time.Second,
		deadline.WithCallback(func() {
			callbackCalled <- struct{}{}
		}),
	)
	assert.Equal(t, dl.Done(), false)
	assert.Nil(t, dl.Reset(3*time.Second))

	// Asumes the test will progress from the previous line in < 1 second
	assert.GreaterOrEqual(t, dl.TimeRemainging(), 2*time.Second)
}

func TestDeadLineResetDecreased(t *testing.T) {
	// Ensure callback is called
	callbackCalled := make(chan struct{})
	dl := deadline.New(10*time.Hour,
		deadline.WithCallback(func() {
			callbackCalled <- struct{}{}
		}),
	)
	assert.Equal(t, dl.Done(), false)
	assert.Nil(t, dl.Reset(time.Millisecond))

	// tests will timeout if this doesn't work
	<-callbackCalled

	assert.Error(t, dl.Reset(time.Millisecond), "An error should be returned if invoked after callbacks ")
}

func TestDeadLineLastPing(t *testing.T) {
	// Ensure callback is called
	callbackCalled := make(chan struct{})

	now := time.Now()
	firstTs := now
	nower := func() time.Time {
		return now
	}

	dl := deadline.New(1*time.Second,
		deadline.WithCallback(func() {
			callbackCalled <- struct{}{}
		}),
		deadline.WithNow(nower),
		deadline.WithPingRateLimit(10*time.Millisecond),
	)

	for i := 0; i <= 10; i++ {
		dl.Ping()
		now = now.Add(time.Nanosecond * 1)
	}

	// when running multiple ping withing the ratelimit, only the first should be keept
	assert.Equal(t, firstTs, dl.LastPing())

	now = now.Add(10 * time.Millisecond)
	dl.Ping()

	// After the ratelimit has passed, the ping should be updated
	assert.NotEqual(t, firstTs, dl.LastPing())

	// Stop should trigger the callback
	dl.Stop()
	<-callbackCalled
}

func TestDeadLinePing(t *testing.T) {
	callbackCalled := make(chan struct{})
	now := time.Now()
	nowMutex := sync.Mutex{}
	nower := func() time.Time {
		nowMutex.Lock()
		defer nowMutex.Unlock()
		return now
	}

	dl := deadline.New(10*time.Millisecond,
		deadline.WithCallback(func() {
			callbackCalled <- struct{}{}
		}),
		deadline.WithNow(nower),
	)

	// Simulate pings happening halfway through the deadline.
	for i := 0; i < 5; i++ {
		nowMutex.Lock()
		now = now.Add(5 * time.Millisecond)
		nowMutex.Unlock()

		dl.Ping()
		assert.Equal(t, dl.Done(), false)
	}

	// Allow make the dealines trigger
	nowMutex.Lock()
	now = now.Add(10 * time.Millisecond)
	nowMutex.Unlock()

	before := time.Now()
	<-callbackCalled
	waitDuration := time.Since(before)
	assert.GreaterOrEqual(t, waitDuration, 10*time.Millisecond, "We should wait at least 10 ms from last ping")
	assert.Less(t, waitDuration, 1.5*10*time.Millisecond, "the timer should finish within close to the 10 ms")

}

func TestDeadNilPing(t *testing.T) {
	assert.NotPanics(t, func() {
		var dl *deadline.Deadline
		dl.Ping()
	}, "pinging a nil deadline should not panic")
}

func TestDeadNilStop(t *testing.T) {
	assert.NotPanics(t, func() {
		var dl *deadline.Deadline
		dl.Stop()
	}, "Stoping a nil deadline should not panic but be an NOP")
}

func TestInitCallback(t *testing.T) {
	callbackCalled := make(chan struct{}, 1)
	deadline.New(10*time.Millisecond,
		deadline.WithInitCallback(func() {
			callbackCalled <- struct{}{}
		}),
	)
	<-callbackCalled
}

func TestInitWithTimeRemaining(t *testing.T) {
	timer := newTestTimer()

	dl := deadline.New(10*time.Millisecond,
		deadline.WithNow(timer.Now),
	)

	timer.Tick(1 * time.Millisecond)
	assert.Equal(t, 9*time.Millisecond, dl.TimeRemainging())
}

func TestCancelStopped(t *testing.T) {
	dl := deadline.New(1 * time.Second)

	dl.Stop()
	assert.Equal(t, true, dl.Cancel(), "if a deadline is stopped with .Stop(), additional .Stop() calls should report that callbacks have already been called")

	dl = deadline.New(1 * time.Second)
	dl.Cancel()
	assert.Equal(t, false, dl.Cancel(), "if a deadline is stopped with .Cancel(); additional Cancel() calls should report that callbacks have not already been called")

}

func BenchmarkPingsNoRatelimit(b *testing.B) {
	dl := deadline.New(10 * time.Millisecond)
	for i := 0; i < b.N; i++ {
		dl.Ping()
	}
}

func BenchmarkPingsWithRatelimit(b *testing.B) {
	now := time.Now()
	nower := func() time.Time {
		return now
	}

	threads := 1000

	tests := []struct {
		name      string
		ratelimit time.Duration
	}{
		{
			name:      "1us",
			ratelimit: time.Microsecond,
		},
		{
			name:      "10us",
			ratelimit: 10 * time.Microsecond,
		},
		{
			name:      "1ms",
			ratelimit: time.Millisecond,
		},
		{
			name:      "10ms",
			ratelimit: 10 * time.Millisecond,
		},
		{
			name:      "1s",
			ratelimit: 1 * time.Second,
		},
	}

	for _, test := range tests {
		b.Run("BenchmarkPingsWithRatelimit_"+test.name, func(b *testing.B) {
			dl := deadline.New(10*time.Millisecond,
				deadline.WithPingRateLimit(test.ratelimit),
				deadline.WithNow(nower),
			)
			for i := 0; i < b.N; i++ {
				wg := sync.WaitGroup{}
				for j := 0; j < threads; j++ {
					wg.Add(1)
					go func() {
						dl.Ping()
						wg.Done()
					}()
				}
				wg.Wait()
			}
		})
	}

}
