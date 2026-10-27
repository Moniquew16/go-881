package go_881

import (
	"sync"
	"time"
)

// Clock is an interface for obtaining the current time.
// It exists so tests can control time progression deterministically
// without sleeping or depending on wall-clock behaviour.
type Clock interface {
	// Now returns the current instant used for rate calculations.
	Now() time.Time
}

// SystemClock is the default Clock implementation backed by time.Now.
type SystemClock struct{}

// Now returns the real wall-clock time.
func (SystemClock) Now() time.Time { return time.Now() }

// defaultClock lazily constructs a SystemClock so callers pay nothing
// unless they actually use the limiter.
var (
	defaultClock     SystemClock
	defaultClockInit sync.Once
	sharedClock      Clock
)

func getDefaultClock() Clock {
	defaultClockInit.Do(func() {
		sharedClock = defaultClock
	})
	return sharedClock
}

// Limiter is a token bucket rate limiter.
//
// Tokens regenerate at rate tokens per second up to a ceiling of burst.
// Each Allow consumes one token; when the bucket is empty, Allow returns
// false until enough time has elapsed to accrue at least one token.
//
// The implementation keeps a monotonic token count as a float64 so that
// sub-second accrual (e.g. 0.25 tokens after 10ms at 25 rps) is honoured
// across calls rather than discarded by integer truncation.
type Limiter struct {
	clock Clock
	rate  float64       // tokens per second
	burst float64       // max tokens the bucket can hold
	mu    sync.Mutex
	tokens float64
	last   time.Time
	closed bool
}

// New returns a Limiter that refills at rate tokens per second and can
// hold up to burst tokens at once. New panics if rate is not positive or
// burst is less than 1, because a limiter with no rate or no burst can
// never allow anything and almost always indicates a configuration bug.
func New(rate float64, burst int) *Limiter {
	return NewWithClock(rate, burst, nil)
}

// NewWithClock is like New but uses a custom Clock. If clock is nil the
// system clock is used. This constructor exists primarily for tests.
func NewWithClock(rate float64, burst int, clock Clock) *Limiter {
	if rate <= 0 {
		panic("go_881: rate must be positive")
	}
	if burst < 1 {
		panic("go_881: burst must be at least 1")
	}
	if clock == nil {
		clock = getDefaultClock()
	}
	return &Limiter{
		clock:  clock,
		rate:   rate,
		burst:  float64(burst),
		tokens: float64(burst),
		last:   clock.Now(),
	}
}

// Rate returns the configured refill rate in tokens per second.
func (l *Limiter) Rate() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rate
}

// Burst returns the configured burst capacity in tokens.
func (l *Limiter) Burst() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return int(l.burst)
}

// Close disables the limiter. After Close, Allow always returns false.
// Close is idempotent and safe to call concurrently.
func (l *Limiter) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
}

// refillLocked advances the token count based on elapsed time since the
// last refill. It must be called with l.mu held.
func (l *Limiter) refillLocked(now time.Time) {
	if now.Before(l.last) {
		// Clock moved backwards — guard against unbounded accrual by
		// simply snapping the reference time forward. We do not touch
		// the token count so a transient backward jump cannot grant
		// extra allowance.
		l.last = now
		return
	}
	elapsed := now.Sub(l.last).Seconds()
	if elapsed > 0 {
		l.tokens += elapsed * l.rate
		if l.tokens > l.burst {
			l.tokens = l.burst
		}
		l.last = now
	}
}

// Allow reports whether one operation may proceed. It consumes one token
// when it returns true and consumes nothing when it returns false.
func (l *Limiter) Allow() bool {
	return l.AllowN(1)
}

// AllowN reports whether n operations may proceed at once and consumes n
// tokens if so. If n exceeds the burst capacity, AllowN always returns
// false — a single request can never drain more than the full bucket.
// If n is zero or negative, AllowN returns true without touching the
// bucket; a zero-cost check should not consume quota.
func (l *Limiter) AllowN(n int) bool {
	if n <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return false
	}
	nf := float64(n)
	if nf > l.burst {
		return false
	}
	now := l.clock.Now()
	l.refillLocked(now)
	if l.tokens >= nf {
		l.tokens -= nf
		return true
	}
	return false
}

// Tokens returns the number of tokens currently available. It refills
// based on elapsed time first, so the value reflects up-to-the-moment
// accrual. The result is a float64 because partial tokens matter at high
// rates (e.g. 10ms at 1000 rps adds 10 tokens).
func (l *Limiter) Tokens() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0
	}
	l.refillLocked(l.clock.Now())
	return l.tokens
}
