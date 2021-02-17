package deadline

import (
	"context"
	"time"
)

// Option adds optional configurations to a deadline instance.
type Option func(*Deadline)

// WithContext adds cancellation by a context or Stop() whichever comes first.
func WithContext(ctx context.Context) Option {
	return func(deadline *Deadline) {
		deadline.ctx, deadline.ctxCancel = context.WithCancel(ctx)
	}
}

// WithNow sets a custom method used to get current time. Mostly used for testing.
func WithNow(now func() time.Time) Option {
	return func(deadline *Deadline) {
		deadline.now = now
	}
}

// WithCallback adds a callback to ge executed after the deadline occurs. Multiple callbacks will be executed
// in blocking sequence.
func WithCallback(callback func()) Option {
	return func(deadline *Deadline) {
		deadline.callbacks = append(deadline.callbacks, callback)
	}
}

// WithPingRateLimit will set the amount of time that needs to pass before accepting a new Ping(). If multiple
// Ping() calls takes place within the the duration only the first will be accepted. Defaults to 0.
func WithPingRateLimit(maxPingRate time.Duration) Option {
	return func(deadline *Deadline) {
		deadline.maxPingRate = maxPingRate
	}
}

// WithInitCallback adds a callback when a deadline is created. Should only be used with Index and the creation
// is depended on race conditions.
func WithInitCallback(callback func()) Option {
	return func(deadline *Deadline) {
		deadline.initCallbacks = append(deadline.initCallbacks, callback)
	}
}
