package mathutil

import (
	"testing"
)

// independentMurmur re-implements the MurmurHash3 fmix64 finalizer in a
// deliberately different style (explicit uint64 constants) so that a wrong
// constant in the production code cannot survive the comparison.
func independentMurmur(xIn uint64) uint64 {
	x := xIn
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return x
}

func TestMurmurHash3MatchesIndependentMixer(t *testing.T) {
	for _, in := range []int64{0, 1, -1, 42, 1234567890, -9223372036854775808, 9223372036854775807} {
		want := int64(independentMurmur(uint64(in)))
		if got := murmurHash3(in); got != want {
			t.Errorf("murmurHash3(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestMurmurHash3ZeroSeedLandsOnSafeState(t *testing.T) {
	// murmurHash3(0) == 0, so feeding a seed of 0 straight into the state would
	// produce (0, 0) - the absorbing state. That is exactly why the original
	// substitutes Long.MIN_VALUE for a zero seed.
	if murmurHash3(0) != 0 {
		t.Fatalf("murmurHash3(0) = %d, want 0", murmurHash3(0))
	}
	verbatim := NewRandState(murmurHash3(0), murmurHash3(0))
	for i := 0; i < 4; i++ {
		if v := verbatim.NextLongNew(); v != 0 {
			t.Fatalf("draw %d from the verbatim zero seed = %d, want 0", i, v)
		}
	}
	// SetSeed(0) takes the Long.MIN_VALUE branch and therefore stays alive.
	r := NewRandSeeded(0)
	for i := 0; i < 4; i++ {
		if v := r.NextLongNew(); v == 0 {
			t.Fatalf("draw %d from seed 0 is 0", i)
		}
	}
}

func TestDegenerateStateStaysDegenerate(t *testing.T) {
	r := NewRandState(0, 0)
	for i := 0; i < 8; i++ {
		if v := r.NextLongNew(); v != 0 {
			t.Fatalf("draw %d from (0,0) = %d, want 0", i, v)
		}
	}
}

func TestRandDeterministic(t *testing.T) {
	a := NewRandSeeded(0x12345678)
	b := NewRandSeeded(0x12345678)
	for i := 0; i < 64; i++ {
		if a.NextLongNew() != b.NextLongNew() {
			t.Fatalf("sequence diverged at draw %d", i)
		}
	}
	if NewRandSeeded(1).NextLongNew() == NewRandSeeded(2).NextLongNew() {
		t.Fatal("different seeds must produce different first draws")
	}
}

func TestRandNext(t *testing.T) {
	// Kotlin: (nextLongNew() and (1L shl bits) - 1).toInt()
	// The shift count is masked to 6 bits exactly like Java's long shifts, so
	// Next(33) is NOT the same call as Next(1) - only the shift count wraps.
	for _, bits := range []int{0, 1, 4, 17, 31, 32, 33, 63, 64, 65, -1} {
		masked := bits & 63
		want := int32(NewRandSeeded(7).NextLongNew() & ((int64(1) << masked) - 1))
		if got := NewRandSeeded(7).Next(bits); got != want {
			t.Fatalf("Next(%d) = %d, want %d", bits, got, want)
		}
	}
	// ... and Next(32) reaches the sign bit, unlike a 31-bit draw.
	high := 0
	for i := 0; i < 50; i++ {
		if NewRandSeeded(int64(i)).Next(32) < 0 {
			high++
		}
	}
	if high == 0 {
		t.Fatal("Next(32) must be able to return negative values")
	}
}

func TestNextIntIsTruncatedLong(t *testing.T) {
	// Kotlin: nextInt() = nextLongNew().toInt()
	a := NewRandSeeded(11)
	b := NewRandSeeded(11)
	if got, want := a.NextInt(), int32(b.NextLongNew()); got != want {
		t.Fatalf("NextInt = %d, want %d", got, want)
	}
}

func TestNextLongIsTwoNext32(t *testing.T) {
	// Inherited from java.util.Random: ((long)next(32) << 32) + next(32)
	a := NewRandSeeded(12)
	b := NewRandSeeded(12)
	hi := int64(a.Next(32))
	lo := int64(a.Next(32))
	want := (hi << 32) + lo
	if got := b.NextLong(); got != want {
		t.Fatalf("NextLong = %d, want %d", got, want)
	}
}

func TestNextLongBoundedBounds(t *testing.T) {
	r := NewRandSeeded(99)
	for _, n := range []int64{1, 2, 3, 7, 1000, 1 << 20, 1 << 40} {
		seen := make(map[int64]bool)
		for i := 0; i < 2000; i++ {
			v := r.NextLongBounded(n)
			if v < 0 || v >= n {
				t.Fatalf("NextLongBounded(%d) = %d, out of range", n, v)
			}
			seen[v] = true
		}
		if n > 1 && len(seen) < 2 {
			t.Fatalf("NextLongBounded(%d) produced only %d distinct values", n, len(seen))
		}
	}
}

func TestNextLongBoundedRejectsOverflowingDraws(t *testing.T) {
	// The original's acceptance test is bits - value + (n - 1) >= 0, i.e. it
	// discards draws whose modulo leaves an overflow in the remainder. For a
	// power-of-two bound the sum can never reach 2^63, so the loop needs an n
	// whose multiples straddle it: 3 * 2^61 makes ~a quarter of all draws fail.
	const n = int64(3) << 61
	r := NewRandSeeded(4242)
	accepted, rejected := 0, 0
	for i := 0; i < 5000; i++ {
		bits := int64(uint64(r.NextLongNew()) >> 1)
		value := bits % n
		if bits-value+(n-1) >= 0 {
			accepted++
		} else {
			rejected++
		}
	}
	if rejected == 0 {
		t.Fatal("expected at least one rejected draw for n = 3 * 2^61")
	}
	if accepted == 0 {
		t.Fatal("expected at least one accepted draw for n = 3 * 2^61")
	}
	// ... and the public method still only ever returns [0, n).
	for i := 0; i < 2000; i++ {
		if v := r.NextLongBounded(n); v < 0 || v >= n {
			t.Fatalf("NextLongBounded(%d) = %d, out of range", n, v)
		}
	}
}

func TestNextIntBoundedMatchesLongVariant(t *testing.T) {
	// Kotlin: nextInt(n) = nextLongNew(n.toLong()).toInt()
	for _, n := range []int{1, 2, 10, 1000, 1 << 30} {
		a := NewRandSeeded(555)
		b := NewRandSeeded(555)
		if got, want := a.NextIntBounded(n), int(b.NextLongBounded(int64(n))); got != want {
			t.Fatalf("NextIntBounded(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestNextBytesFillsWholeArray(t *testing.T) {
	// Kotlin fills every byte (the doc says the number of random bytes equals
	// the array length): 8 bytes per long, from the tail backwards.
	r := NewRandSeeded(2024)
	buf := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA, 0xAA}
	r.NextBytes(buf)
	untouched := 0
	for _, b := range buf {
		if b == 0xAA {
			untouched++
		}
	}
	if untouched > 0 {
		t.Fatalf("NextBytes left %d of %d bytes untouched", untouched, len(buf))
	}

	// Reproduce the layout independently: bytes[8] is the low byte of draw 1,
	// bytes[1] the high byte of draw 1, bytes[0] the low byte of draw 2.
	ref := NewRandSeeded(2024)
	d1 := uint64(ref.NextLongNew())
	d2 := uint64(ref.NextLongNew())
	want := []byte{byte(d2), byte(d1 >> 56), byte(d1 >> 48), byte(d1 >> 40), byte(d1 >> 32), byte(d1 >> 24), byte(d1 >> 16), byte(d1 >> 8), byte(d1)}
	for i := range want {
		if buf[i] != want[i] {
			t.Fatalf("byte %d = %#x, want %#x", i, buf[i], want[i])
		}
	}
}

func TestRandomIntClosedInterval(t *testing.T) {
	// Kotlin: random(min, max) = min + nextInt(max - min + 1) -> CLOSED [min, max].
	r := NewRandSeeded(1000)
	for i := 0; i < 3000; i++ {
		v := r.RandomInt(1000, 100000) // RelayRoom.getRelay's argument
		if v < 1000 || v > 100000 {
			t.Fatalf("RandomInt(1000, 100000) = %d, out of range", v)
		}
	}
	// The upper bound really is reachable.
	sawTop := false
	for i := 0; i < 200000 && !sawTop; i++ {
		if r.RandomInt(5, 6) == 6 {
			sawTop = true
		}
	}
	if !sawTop {
		t.Fatal("RandomInt(5, 6) never returned 6; the original's interval is closed")
	}
}

func TestRangeIntSymmetric(t *testing.T) {
	// Kotlin: range(amount) = nextInt(amount * 2 + 1) - amount, i.e. [-amount, amount]
	r := NewRandSeeded(2000)
	neg, pos := 0, 0
	for i := 0; i < 3000; i++ {
		v := r.RangeInt(3)
		if v < -3 || v > 3 {
			t.Fatalf("RangeInt(3) = %d, want [-3, 3]", v)
		}
		if v < 0 {
			neg++
		}
		if v > 0 {
			pos++
		}
	}
	if neg == 0 || pos == 0 {
		t.Fatalf("RangeInt(3) produced %d negative and %d positive values", neg, pos)
	}
	if v := r.RangeInt(0); v != 0 {
		t.Fatalf("RangeInt(0) = %d, want 0", v)
	}
}

func TestRangeFloatSymmetric(t *testing.T) {
	// Kotlin: range(amount) = nextFloat() * amount * 2 - amount, i.e. [-amount, amount)
	r := NewRandSeeded(3000)
	for i := 0; i < 2000; i++ {
		v := r.RangeFloat(1.5)
		if v < -1.5 || v >= 1.5 {
			t.Fatalf("RangeFloat(1.5) = %v, want [-1.5, 1.5)", v)
		}
	}
	if v := r.RangeFloat(0); v != 0 {
		t.Fatalf("RangeFloat(0) = %v, want 0", v)
	}
}

func TestNextDoubleAndFloatRanges(t *testing.T) {
	r := NewRandSeeded(4000)
	for i := 0; i < 2000; i++ {
		d := r.NextDouble()
		if d < 0 || d >= 1 {
			t.Fatalf("NextDouble = %v, want [0,1)", d)
		}
		f := r.NextFloat()
		if f < 0 || f >= 1 {
			t.Fatalf("NextFloat = %v, want [0,1)", f)
		}
	}
}

func TestNextBoolean(t *testing.T) {
	r := NewRandSeeded(5000)
	trues := 0
	for i := 0; i < 1000; i++ {
		if r.NextBoolean() {
			trues++
		}
	}
	if trues == 0 || trues == 1000 {
		t.Fatalf("NextBoolean returned %d/1000 trues", trues)
	}
}

func TestChance(t *testing.T) {
	r := NewRandSeeded(6000)
	for i := 0; i < 100; i++ {
		if !r.Chance(1) {
			t.Fatal("Chance(1) must always pass")
		}
		if r.Chance(0) {
			t.Fatal("Chance(0) must never pass")
		}
	}
}

func TestSetSeedReproducesSequence(t *testing.T) {
	r := NewRandSeeded(1)
	first := r.NextLongNew()
	r.SetSeed(1)
	if got := r.NextLongNew(); got != first {
		t.Fatalf("SetSeed(1) redraw = %d, want %d", got, first)
	}
}

func TestGetStateRoundTrip(t *testing.T) {
	r := NewRandSeeded(77)
	r.NextLongNew()
	r.NextLongNew()
	s0, s1 := r.GetState(0), r.GetState(1)
	if s0 == 0 && s1 == 0 {
		t.Fatal("GetState returned the absorbing zero state")
	}
	want0, want1 := r.NextLongNew(), r.NextLongNew()
	r.SetState(s0, s1)
	if got0, got1 := r.NextLongNew(), r.NextLongNew(); got0 != want0 || got1 != want1 {
		t.Fatalf("SetState did not replay: got (%d,%d), want (%d,%d)", got0, got1, want0, want1)
	}
}

func TestDefaultSeedsAreDistinct(t *testing.T) {
	// No sleeps: the seedUniquifier chain must keep NewRand distinct even when
	// the clock returns the same nanosecond twice.
	seen := make(map[int64]bool)
	for i := 0; i < 64; i++ {
		first := NewRand().NextLongNew()
		if seen[first] {
			t.Fatalf("duplicate first draw %d from NewRand", first)
		}
		seen[first] = true
	}
}
