package deadline

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Deadline allows users to implement keep alive patterns where after inactivty one or more callbacks are triggerd. Can also
// extract a context for context aware application which require a keep-alive pattern. TTLs are reset on each Ping()
type Deadline struct {
	ctx       context.Context
	ctxCancel func()

	mutex    sync.RWMutex
	lastPing time.Time

	deadline        time.Duration
	resetDeadlineCh chan time.Duration
	callbacksDone   chan struct{}

	callbacks     []func()
	initCallbacks []func()

	maxPingRate time.Duration

	now func() time.Time
}

// New returns a deadline which will call each callback sequencially after maxIdle time has passed
// withouth deadline.Ping() being called.
// callbacks will not be invoked on ctx.Done unless callbackOnCtxDone == true
// Please note that due to how golang manages channels, there is a risk that Ping can be called and directly after each of
// the callbacks are invoked (even though the maxIdle hasn't been reached since the last ping). This is NOT a high resolution tool
func New(ttl time.Duration, options ...Option) *Deadline {
	ctx, cancel := context.WithCancel(context.Background())
	deadline := &Deadline{
		ctx:       ctx,
		ctxCancel: cancel,

		mutex: sync.RWMutex{},
		now:   time.Now,

		deadline:        ttl,
		resetDeadlineCh: make(chan time.Duration),
		callbacksDone:   make(chan struct{}),

		callbacks:     []func(){},
		initCallbacks: []func(){},
	}

	for _, option := range options {
		option(deadline)
	}

	for _, cb := range deadline.initCallbacks {
		cb()
	}
	// Set the initial ping with the possibly configured now override
	deadline.Ping()

	go deadline.monitor()

	return deadline
}

func (deadline *Deadline) monitor() {
	t := time.NewTimer(deadline.deadline)

	// Timer cleanup
	defer func() {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		deadline.runCallbacks()
	}()

	defer deadline.ctxCancel()

	for {
		select {
		// Change the deadline TTL
		case newDeadline := <-deadline.resetDeadlineCh:
			deadline.mutex.Lock()
			deadline.deadline = newDeadline
			deadline.lastPing = deadline.now()
			if !t.Stop() {
				<-t.C
			}
			t.Reset(newDeadline)
			deadline.mutex.Unlock()

		// Time expired, but we might have called Ping() which can extend the timer
		case <-t.C:
			now := deadline.now()

			deadline.mutex.RLock()
			tSinceLastPing := now.Sub(deadline.lastPing)

			// Did we reach the deadline?
			if tSinceLastPing >= deadline.deadline {
				deadline.ctxCancel()
				deadline.mutex.RUnlock()
				return
			}
			deadline.mutex.RUnlock()

			// If not reset it with a new ttl
			t.Reset(deadline.deadline - tSinceLastPing)

		case <-deadline.ctx.Done():
			return
		}
	}
}

// runCallbacks can only be invoked after deadline.ctxCancel()
// has been called.
func (deadline *Deadline) runCallbacks() {
	deadline.mutex.RLock()
	defer deadline.mutex.RUnlock()

	for _, cb := range deadline.callbacks {
		cb()
	}
	close(deadline.callbacksDone)
}

// Reset will set a new deadline duration and instantly call Ping()
// Reset returns if the dealine has already expiered and the reset operation aborted
func (deadline *Deadline) Reset(d time.Duration) error {
	select {
	case deadline.resetDeadlineCh <- d:
		return nil
	case <-deadline.ctx.Done():
		return deadline.ctx.Err()
	}
}

// Set is a convineince method to work with point in time deadlines rather than durations. Set can/will modify both LastPing and TTL
// values so using Set in combination with Ping() is not advised.
func (deadline *Deadline) Set(t time.Time) error {
	until := time.Until(t)

	// Do We need to reset our timer since it will not expire in time?
	deadline.mutex.Lock()
	if deadline.timeRemainging() > until {
		deadline.mutex.Unlock()
		return deadline.Reset(until)
	}

	// if the new deadline is larger then the time remaining we can wait for the
	// current time to expire and it will automatically reset to the new deadline
	deadline.deadline = until
	deadline.lastPing = time.Now()
	deadline.mutex.Unlock()

	return deadline.ctx.Err()
}

// Stop will terminate the deadline; call the callbacks and frees up resources.
func (deadline *Deadline) Stop() {
	if deadline == nil {
		return
	}
	deadline.ctxCancel()
	<-deadline.callbacksDone
}

// Cancel will terminate the deadline; but not call any callbacks. Returns if the
// callbacks have been called prior to calling Cancel() (or is currently running)
func (deadline *Deadline) Cancel() bool {
	// Precautious check that can avoid deadlocks if a callbacks would ever
	// (stupidly) call Cancel on itself..
	if deadline.Done() {
		// to cover the case when calling cancel twice, the second should incate that no callbacks have been triggered
		return deadline.callbacks != nil
	}

	deadline.mutex.Lock()
	defer deadline.mutex.Unlock()

	// we know that runCallbacks can't be called
	// before deadline.Done() == true so if done is false
	// we can safely clear the callbacks as runCallbacks
	// won't be able to read them until we're release our lock
	// if deadline.Done() is true however, we can't know if they
	// have run or will run; only that they're not running right now
	// (due to the lock)
	if deadline.Done() {
		return true
	}

	deadline.callbacks = nil
	deadline.ctxCancel()
	return false
}

// LastPing holds the timestamp of the last accepted Ping() call
func (deadline *Deadline) LastPing() time.Time {
	deadline.mutex.RLock()
	defer deadline.mutex.RUnlock()
	return deadline.lastPing
}

// Ctx returns a context that is cancelled after the deadline.
func (deadline *Deadline) Ctx() context.Context {
	return deadline.ctx
}

// Done checks if the deadline has expired
func (deadline *Deadline) Done() (res bool) {
	return deadline.ctx.Err() != nil
}

// TimeRemainging returns the duration until the callbacks will be triggerd if no more Ping() are called
func (deadline *Deadline) TimeRemainging() time.Duration {
	deadline.mutex.RLock()
	defer deadline.mutex.RUnlock()
	return deadline.timeRemainging()
}

func (deadline *Deadline) timeRemainging() time.Duration {
	return deadline.deadline - deadline.now().Sub(deadline.lastPing)
}

// Ping resets the idle timer to zero; non blocking
// Returns if the deadline got cancelled before Ping
// could complete it's reset.
func (deadline *Deadline) Ping() error {
	if deadline == nil {
		return fmt.Errorf("calling Ping() on nil *Deadline is a NOP")
	}

	now := deadline.now()

	// Locks are costly; we can redue the cost by only Read-locking if sufficient time has passed since the last timer reset
	deadline.mutex.RLock()
	lastPingCopy := deadline.lastPing
	deadline.mutex.RUnlock()

	if now.Sub(lastPingCopy) >= deadline.maxPingRate {
		deadline.mutex.Lock()
		deadline.lastPing = now
		deadline.mutex.Unlock()
	}

	return deadline.ctx.Err()
}
