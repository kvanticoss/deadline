package deadline

import (
	"context"
	"sync"
	"time"
)

// Index manages multiple dealine instances indexed by a string. Will cleanup itself once an index is killed.
type Index struct {
	index map[string]*Deadline
	mu    sync.Mutex
}

// NewIndex returns a new Index
func NewIndex() *Index {
	return &Index{
		index: map[string]*Deadline{},
	}
}

// Get returns *Deadline if there is a deadline present at indexKey, otherwise nil
// Get is a mutex protected map lookup. It is safe to ping a nil deadline but other operations will panic.
func (ka *Index) Get(indexKey string) *Deadline {
	ka.mu.Lock()
	defer ka.mu.Unlock()

	return ka.get(indexKey)
}

// get is a mutex free version of Get
func (ka *Index) get(indexKey string) *Deadline {
	return ka.index[indexKey]
}

// GetOrCreate will return the deadline at the index or create it using the arguements
// Due to the async nature of this package it is possible that a deadline object is returned which is already
// terminated or in the process of being terminated; as such the user should do
// ka := index.GetOrCreate(....); if ka.Ping() != nil { re-create... }.
// This is autoamtically handled by the PingOrCreate method.
func (ka *Index) GetOrCreate(indexKey string, deadline time.Duration, options ...Option) *Deadline {
	ka.mu.Lock()
	defer ka.mu.Unlock()

	if k, ok := ka.index[indexKey]; ok {
		return k
	}

	ka.index[indexKey] = New(deadline, append(options, WithCallback(ka.getIndexCleanupHook(indexKey)))...)

	return ka.index[indexKey]
}

// PingOrCreate does the same as GetOrCreate but handles the case when the deadline object is killed while we return it.
// in such cases; PingOrCreate will recreate a new deadline Object. NOTE: it could still timeout before being returned
// the the invoking block (or while it is being retured)
func (ka *Index) PingOrCreate(indexKey string, maxIdle time.Duration, options ...Option) *Deadline {
	k := ka.GetOrCreate(indexKey, maxIdle, options...)
	if k.Ping() != nil { // It got cancelled before we could run our .Ping()
		return ka.GetOrCreate(indexKey, maxIdle, options...)
	}
	return k
}

// Stop terminates any deadline present on the key and calls the callbacks
// After Stop the key is guarranteed to be removed from the index
func (ka *Index) Stop(key string) {
	if dl := ka.Get(key); dl != nil {
		dl.Stop()
	}
}

// Cancel terminates any deadline present on the key and ignores the callbacks
// After Cancel the key is guarranteed to be removed from the index
func (ka *Index) Cancel(key string) (blockedCallbacks bool) {
	if dl := ka.Get(key); dl != nil {
		blockedCallbacks = dl.Cancel()

		//  We must manually remove ourselves from the index since callbacks doesn't trigger on cancel
		if !blockedCallbacks {
			ka.mu.Lock()
			delete(ka.index, key)
			ka.mu.Unlock()
		}
	}
	return false
}

// CancelAll terminates all deadlines and executes thier the callbacks
// After CancelAll the keys are guarranteed to be removed from the index
func (ka *Index) CancelAll() {
	ka.mu.Lock()
	defer ka.mu.Unlock()

	for _, ka := range ka.index {
		ka.Cancel()
	}
	ka.index = map[string]*Deadline{}
}

// StopAll terminates all deadlines and executes thier the callbacks concurrently
// waiting for all to complete. providate a cancelled context to not wait
// After StopAll the keys are guarranteed to be removed from the index
func (ka *Index) StopAll(ctx context.Context) {
	ka.mu.Lock()
	copy := ka.index
	ka.index = map[string]*Deadline{}
	ka.mu.Unlock()

	for _, ka := range copy {
		ka.ctxCancel()
	}
	for _, ka := range copy {
		select {
		case <-ctx.Done():
		case <-ka.callbacksDone:
		}
	}

}

func (ka *Index) getIndexCleanupHook(indexKey string) func() {
	return func() {
		ka.mu.Lock()
		defer ka.mu.Unlock()

		delete(ka.index, indexKey)
	}
}
