package deadline

import (
	"context"
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

	callbacks     []func()
	initCallbacks []func()

	maxPingRate time.Duration

	now func() time.Time
}

// Option adds optional configurations to a deadline instance.
type Option func(*Deadline)

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

		callbacks:     []func(){},
		initCallbacks: []func(){},
	}

	for _, option := range options {
		option(deadline)
	}

	for _, cb := range deadline.initCallbacks {
		cb()
	}

	// Set the initial with the possibly configured now override
	deadline.Ping()

	go deadline.monitor()

	return deadline
}

func (deadline *Deadline) monitor() {
	t := time.NewTimer(deadline.deadline)
	defer func() {
		if !t.Stop() {
			select {
			case <-t.C:
			default:
			}
		}
		deadline.runCallbacks()
	}()

	for {
		select {
		case newDeadline := <-deadline.resetDeadlineCh:
			deadline.mutex.Lock()
			deadline.deadline = newDeadline
			deadline.lastPing = deadline.now()
			if !t.Stop() {
				<-t.C
			}
			t.Reset(newDeadline)
			deadline.mutex.Unlock()

		case <-t.C:
			now := deadline.now()
			deadline.mutex.RLock()
			tSinceLastReset := now.Sub(deadline.lastPing)
			deadline.mutex.RUnlock()

			if tSinceLastReset >= deadline.deadline {
				deadline.Stop()
				continue
			}
			t.Reset(deadline.deadline - tSinceLastReset)

		case <-deadline.ctx.Done():
			return
		}
	}
}

func (deadline *Deadline) runCallbacks() {
	deadline.mutex.Lock()
	callbacksCopy := deadline.callbacks
	// Prevent callbacks from being invoked twice and give us a signal that we have processed callbacks
	deadline.callbacks = deadline.callbacks[0:0]
	deadline.mutex.Unlock()

	for _, cb := range callbacksCopy {
		cb()
	}
}

// Reset will set a new deadline duration and instantly call Ping()
// Reset returns if the dealine has already expiered and the reset operation aborted
func (deadline *Deadline) Reset(d time.Duration) bool {
	select {
	case deadline.resetDeadlineCh <- d:
		return false
	case <-deadline.ctx.Done():
		return true
	}
}

// Stop will terminate the deadline; call the callbacks and frees up resources.
func (deadline *Deadline) Stop() {
	if deadline == nil {
		return
	}
	deadline.ctxCancel()
}

// Cancel will terminate the deadline; but not call any callbacks. Returns if the callbacks have been called prior to calling Cancel()
func (deadline *Deadline) Cancel() bool {
	// Lockfree check
	if deadline.Done() {
		return deadline.callbacks != nil
	}

	deadline.mutex.Lock()
	defer deadline.mutex.Unlock()
	// ensure it still hasn't cancelled
	// we know that runCallbacks can't be called
	// before deadline.Done() == true so if done is false
	// we can safely clear the callbacks as runCallbacks
	// won't be able to read them until we're done
	if deadline.Done() {
		return deadline.callbacks != nil
	}

	deadline.callbacks = nil
	deadline.ctxCancel()
	return false
}

// LastPing holds the timestamp of the last ping / timer done
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

	return deadline.deadline - deadline.now().Sub(deadline.lastPing)
}

// Ping resets the idle timer to zero; non blocking
func (deadline *Deadline) Ping() {
	if deadline == nil {
		return
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
}
