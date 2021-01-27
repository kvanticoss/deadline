# GoLang Deadline
A feature wrapper around [time.Timer](https://pkg.go.dev/time) with extra the support to restart (an active timer) and extend the timer in a thread safe manner. Useful to implement keep alive patterns where something should happen after some inactivity. Adds easy hooks to integrate with contexts.

## License
Apache 2.0

## Status
There are likely some races, reviews are welcome. API can be volatile until v1. Used in other projects. Not yet "tested in production".

## Usage

```
dl := deadline.New(
  10*time.Millisecond,
  deadline.WithCallback(func() {
     //deadline expired.
  }),
)

// Reset the deadline timer to 10ms (or whatever was configured.)
dl.Ping()
```

`dl.Stop()` stops the timer and executes callbacks. `dl.Cancel()` will stop the timer and remove any callbacks; note that this call is racy and can return false if the callbacks has already been invoked/running/scheduled

if multiple callbacks are added they will be called in blocking sequence. If you need multithreading it should be handled by the callback closure.

Hooks for context-usage such as `deadline.WithContext(ctx)` and `.Ctx()` allows for easy integration with existing context aware code bases.

Deadline-methods which doesn't return any value can be called on nil-instances without a panic.

## NB - Races
This library provides a best effort deadlines. It is fully possible that a callback hasn't been invoked before a `Ping()` but will be invoked before a `Ping()` call returns to the callee. That is just the nature of concurrent programming without shared locks. ¯\_(ツ)_/¯

## Usage - Index.
`deadline.Index` (created through `deadline.NewIndex(...)`) is a mutex protected map of deadlines for situations where mutliple dealines are required. Threadsafe to use but be aware that there will always be race conditions you have to manage. Mostly built to allow for later optimization of the use case.

`index := index.GetOrCreate(indexKey string, deadline time.Duration, options ...Option)` to initialize an index and to update it simply run `index.Get(key).Ping()`. Ping works even on nil-dealines but is then a NOP.


## Design considerations
Each deadline will spwan a go-thread. `Ping()` calls will not initialize a new `time.Timer` (used under the hood) until after it has expired. As such calling `Ping()` is a very cheap and thread safe operation.