# go-881

A token bucket rate limiter for Go. Controls how many operations per second are allowed, with burst support.

```go
package main

import (
    "fmt"

    go881 "go-881"
)

func main() {
    lim := go881.New(10, 5) // 10 ops/sec, burst of 5

    for i := 0; i < 10; i++ {
        if lim.Allow() {
            fmt.Println("allowed")
        } else {
            fmt.Println("denied")
        }
    }
}
```

The `Limiter` refills tokens at `rate` per second up to a `burst` ceiling. Each `Allow()` consumes one token; `AllowN(n)` consumes `n`. When the bucket is empty, calls are denied until enough time elapses to accrue the requested amount.

## Why this exists

Most rate limiter libraries bundle features you rarely need (distributed state, Redis backends, middleware adapters). This one does one thing: a local, in-memory, goroutine-safe token bucket. The trade-off is simplicity and a tiny dependency surface — no third-party packages, no network, no configuration files.

Tokens are tracked as a float64 internally so sub-second accrual is preserved across calls. At 1000 rps, 5ms yields 5 tokens — not zero. Integer truncation at each call would silently throttle high-rate buckets, and that is the kind of bug that only surfaces in production under load.

A `Clock` interface is accepted in `NewWithClock` so tests can advance time without sleeping. The default `New` constructor uses the system clock. If you pass `nil`, the system clock is used.

## Edges you will hit

- `AllowN` returns `false` (without consuming tokens) if `n` exceeds `burst`. A single request can never drain more than the full bucket — if you need to allow a batch larger than burst, split it.
- `AllowN(0)` and `AllowN(-1)` return `true` without touching the bucket. A zero-cost check should not consume quota.
- `New` panics if `rate <= 0` or `burst < 1`. A limiter that can never allow anything is almost always a configuration bug; failing loudly is better than silently denying everything.
- `Close()` permanently disables the limiter; afterwards `Allow`, `AllowN`, and `Tokens` all report empty.
- If the system clock moves backwards (NTP adjustment, manual change), the limiter snaps its reference time forward but does **not** grant extra tokens. This prevents a backward jump from unlocking a throttled caller.

## Exported names

- `Clock` — interface with `Now() time.Time`.
- `SystemClock` — default `Clock` backed by `time.Now`.
- `Limiter` — the token bucket.
- `New(rate float64, burst int) *Limiter` — construct with the system clock.
- `NewWithClock(rate float64, burst int, clock Clock) *Limiter` — construct with a custom clock.
- `(*Limiter).Allow() bool` — consume one token.
- `(*Limiter).AllowN(n int) bool` — consume `n` tokens.
- `(*Limiter).Tokens() float64` — current available tokens (refills first).
- `(*Limiter).Rate() float64` — configured refill rate.
- `(*Limiter).Burst() int` — configured burst capacity.
- `(*Limiter).Close()` — permanently disable the limiter.
