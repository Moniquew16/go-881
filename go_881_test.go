package go_881

import (
	"math"
	"testing"
	"time"
)

// fakeClock is a controllable Clock for deterministic tests.
// It never touches the system clock, so tests are independent of wall time.
type fakeClock struct {
	t time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time { return f.t }

func (f *fakeClock) advance(d time.Duration) { f.t = f.t.Add(d) }

const eps = 1e-9

func floatEq(a, b float64) bool {
	return math.Abs(a-b) < eps
}

func TestNewPanicsOnZeroRate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for zero rate")
		}
	}()
	New(0, 5)
}

func TestNewPanicsOnNegativeRate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for negative rate")
		}
	}()
	New(-1, 5)
}

func TestNewPanicsOnZeroBurst(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for zero burst")
		}
	}()
	New(5, 0)
}

func TestInitialBurstAvailable(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 5, clock)
	for i := 0; i < 5; i++ {
		if !lim.Allow() {
			t.Fatalf("expected Allow #%d to succeed on full bucket", i+1)
		}
	}
	if lim.Allow() {
		t.Fatal("6th Allow should fail, bucket is empty")
	}
}

func TestRefillAccruesOverTime(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 5, clock)
	// Drain everything.
	for i := 0; i < 5; i++ {
		lim.Allow()
	}
	if lim.Allow() {
		t.Fatal("bucket should be empty")
	}
	// 0.1s at 10 rps => exactly 1 token.
	clock.advance(100 * time.Millisecond)
	if !lim.Allow() {
		t.Fatal("expected 1 token after 100ms at 10rps")
	}
	if lim.Allow() {
		t.Fatal("only 1 token should have accrued")
	}
}

func TestRefillCapsAtBurst(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 3, clock)
	// Drain everything.
	for i := 0; i < 3; i++ {
		lim.Allow()
	}
	// Wait long enough to overflow the bucket.
	clock.advance(10 * time.Second)
	tok := lim.Tokens()
	if !floatEq(tok, 3.0) {
		t.Fatalf("expected tokens capped at burst 3, got %f", tok)
	}
}

func TestSubSecondAccrualAccumulates(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(100, 1, clock)
	lim.Allow() // bucket now empty
	// 5ms at 100 rps => 0.5 tokens. Not enough yet.
	clock.advance(5 * time.Millisecond)
	if lim.Allow() {
		t.Fatal("0.5 tokens should not allow")
	}
	// Another 5ms => 0.5 more, total 1.0.
	clock.advance(5 * time.Millisecond)
	if !lim.Allow() {
		t.Fatal("1.0 token accumulated should allow")
	}
}

func TestAllowNGreaterThanBurstFails(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 3, clock)
	if lim.AllowN(4) {
		t.Fatal("AllowN(4) with burst 3 should fail without consuming")
	}
	// Verify no tokens were consumed.
	tok := lim.Tokens()
	if !floatEq(tok, 3.0) {
		t.Fatalf("failed AllowN should not consume tokens, got %f", tok)
	}
}

func TestAllowNConsumesExactCount(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 5, clock)
	if !lim.AllowN(3) {
		t.Fatal("AllowN(3) on full bucket should succeed")
	}
	if !floatEq(lim.Tokens(), 2.0) {
		t.Fatalf("expected 2 tokens remaining, got %f", lim.Tokens())
	}
}

func TestAllowNZeroIsFree(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 2, clock)
	if !lim.AllowN(0) {
		t.Fatal("AllowN(0) should always return true without consuming")
	}
	if !floatEq(lim.Tokens(), 2.0) {
		t.Fatalf("AllowN(0) should not consume, got %f", lim.Tokens())
	}
}

func TestAllowNNegativeIsFree(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 2, clock)
	if !lim.AllowN(-1) {
		t.Fatal("AllowN(-1) should return true without consuming")
	}
	if !floatEq(lim.Tokens(), 2.0) {
		t.Fatalf("AllowN(-1) should not consume, got %f", lim.Tokens())
	}
}

func TestCloseBlocksAllows(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 5, clock)
	if !lim.Allow() {
		t.Fatal("Allow should succeed before close")
	}
	lim.Close()
	if lim.Allow() {
		t.Fatal("Allow should fail after close")
	}
	if lim.AllowN(1) {
		t.Fatal("AllowN should fail after close")
	}
	if lim.Tokens() != 0 {
		t.Fatalf("Tokens should be 0 after close, got %f", lim.Tokens())
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 5, clock)
	lim.Close()
	lim.Close()
	lim.Close()
	if lim.Allow() {
		t.Fatal("Allow should still fail after multiple closes")
	}
}

func TestRateAndBurstAccessors(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(42.5, 7, clock)
	if !floatEq(lim.Rate(), 42.5) {
		t.Fatalf("Rate() = %f, want 42.5", lim.Rate())
	}
	if lim.Burst() != 7 {
		t.Fatalf("Burst() = %d, want 7", lim.Burst())
	}
}

func TestBackwardClockDoesNotGrantTokens(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 5, clock)
	// Drain bucket.
	for i := 0; i < 5; i++ {
		lim.Allow()
	}
	// Move clock backwards.
	clock.advance(-5 * time.Second)
	if lim.Allow() {
		t.Fatal("backward clock should not grant tokens")
	}
	tok := lim.Tokens()
	if tok < 0 || tok > eps {
		t.Fatalf("backward clock should leave tokens near 0, got %f", tok)
	}
}

func TestNewWithNilClockUsesSystem(t *testing.T) {
	lim := NewWithClock(1000, 100, nil)
	// Should be usable immediately without panicking.
	if lim.Rate() != 1000 {
		t.Fatalf("Rate = %f, want 1000", lim.Rate())
	}
	if lim.Burst() != 100 {
		t.Fatalf("Burst = %d, want 100", lim.Burst())
	}
	// Don't assert on Allow here because wall-clock timing varies;
	// we only verify construction with the nil fallback.
}

func TestSteadyRateAllowsExpectedCount(t *testing.T) {
	clock := newFakeClock()
	lim := NewWithClock(10, 1, clock)
	allowed := 0
	// Simulate 10 ticks of 100ms each at 10 rps.
	// Each tick accrues 1 token, and we consume one per tick.
	for i := 0; i < 10; i++ {
		clock.advance(100 * time.Millisecond)
		if lim.Allow() {
			allowed++
		}
	}
	if allowed != 10 {
		t.Fatalf("expected 10 allows over 1s at 10rps, got %d", allowed)
	}
}
