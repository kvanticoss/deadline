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

	return ka.index[indexKey]
}

// GetOrCreate will return the deadline object situation at the index or create it using the arguements
// Due to the async nature of this package it is possible that a deadline object is returned which is already
// done; as such the user should do ka := index.GetOrCreate(....); ka.Ping(); if ka.Done() { retry... }.
// This is autoamtically handled by the PingOrCreate method. This is the preferred method.
func (ka *Index) GetOrCreate(indexKey string, deadline time.Duration, options ...Option) *Deadline {
	ka.mu.Lock()
	defer ka.mu.Unlock()

	if k, ok := ka.index[indexKey]; ok {
		return k
	}

	options = append(options, WithCallback(ka.getIndexCleanupHook(indexKey)))
	ka.index[indexKey] = New(deadline, options...)

	return ka.index[indexKey]
}

// PingOrCreate does the same as GetOrCreate but handles the case when the deadline object is killed while we return it.
// in such cases; PingOrCreate will recreate a new deadline Object.
func (ka *Index) PingOrCreate(indexKey string, maxIdle time.Duration, options ...Option) *Deadline {
	k := ka.GetOrCreate(indexKey, maxIdle, options...)
	k.Ping()
	if k.Done() { // It got cancelled before we could run our .Ping()
		return ka.GetOrCreate(indexKey, maxIdle, options...)
	}
	return k
}

// Stop terminates any deadline present on the key and calls the callbacks
func (ka *Index) Stop(key string) {
	if dl := ka.Get(key); dl != nil {
		dl.Stop() // the callback will remove the index but this can be racy.
		ka.mu.Lock()
		delete(ka.index, key)
		ka.mu.Unlock()
	}
}

// Cancel terminates any deadline present on the key and ignores the callbacks
func (ka *Index) Cancel(key string) (blockedCallbacks bool) {
	if dl := ka.Get(key); dl != nil {
		blockedCallbacks = dl.Cancel() // the callback will NOT remove the index since it is never called on cancel
		if !blockedCallbacks {
			ka.mu.Lock()
			delete(ka.index, key)
			ka.mu.Unlock()
		}
	}
	return false
}

// CancelAll terminates all deadlines and executes thier the callbacks
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
