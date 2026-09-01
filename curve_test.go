package depth

import (
	"math"
	"testing"
)

func TestSigmoidRefusesAStrengthItCannotUse(t *testing.T) {
	for _, s := range []float64{0, -1, -0.0001, math.NaN(), math.Inf(1)} {
		if got := Sigmoid(s); got != nil {
			t.Errorf("Sigmoid(%v) built a curve of %d entries", s, len(got))
		}
	}
}

func TestASigmoidCompressesTheEndsAndExpandsTheMiddle(t *testing.T) {
	c := Sigmoid(4)
	if len(c) != 256 {
		t.Fatalf("the curve has %d entries", len(c))
	}
	if c[0] != 0 || c[255] != 255 {
		t.Fatalf("the ends moved: %d..%d", c[0], c[255])
	}
	for i := 1; i < 256; i++ {
		if c[i] < c[i-1] {
			t.Fatalf("the curve goes backwards at %d: %d then %d", i, c[i-1], c[i])
		}
	}
	// The point of it: the same fifteen steps of depth cover a SMALLER range at
	// the near end than they would without a curve, and a larger one in the
	// middle. Not the same as giving the near end less disparity -- it gets
	// more, and DisparityOf says so.
	near := int(c[255]) - int(c[240])
	mid := int(c[135]) - int(c[120])
	if near >= 15 {
		t.Errorf("the near end was not compressed: 15 steps of depth became %d", near)
	}
	if mid <= 15 {
		t.Errorf("the middle was not expanded: 15 steps of depth became %d", mid)
	}
	// A stronger curve compresses harder. Without this, a curve that ignored
	// its argument would pass everything above.
	if hard := int(Sigmoid(10)[255]) - int(Sigmoid(10)[240]); hard >= near {
		t.Errorf("strength 10 compressed the near end to %d, no more than strength 4 did (%d)", hard, near)
	}
}

func TestDisparityOfSaysWhatACurveCostsInPixels(t *testing.T) {
	// At the default disparity a curve barely matters, and that is worth
	// pinning rather than discovering twice: twenty-four pixels between the
	// eyes is twelve each way, so the whole depth range has THIRTEEN distinct
	// shifts to spend. Quantisation swamps any reshaping of it.
	plain := DisparityOf(nil, Options{MaxShift: 24})
	curved := DisparityOf(Sigmoid(4), Options{MaxShift: 24})
	if plain[0] != 0 || plain[255] != 12 {
		t.Fatalf("without a curve, 0..255 maps to %d..%d, want 0..12", plain[0], plain[255])
	}
	moved := 0
	for d := range plain {
		if plain[d] != curved[d] {
			moved++
		}
	}
	if moved > 128 {
		t.Errorf("at the default disparity the curve moved %d of 256 depths; it was meant to be swamped", moved)
	}

	// Given room, it does what it says. Ninety-six pixels is far too much for a
	// film and exactly right for seeing whether the shape is real.
	wide := Options{MaxShift: 96}
	plain, curved = DisparityOf(nil, wide), DisparityOf(Sigmoid(4), wide)
	if curved[255] != plain[255] {
		t.Errorf("the nearest thing moved: %d against %d", curved[255], plain[255])
	}
	// A near object gets MORE disparity, not less. This assertion caught a
	// wrong description of what a sigmoid does: it was first written the other
	// way round, on the assumption that an S-curve eases the near field, and
	// the arithmetic said no.
	if curved[230] <= plain[230] {
		t.Errorf("at depth 230 the curve gives %d, no more than the %d it replaces",
			curved[230], plain[230])
	}
	// What it really buys: the near end FLATTENS -- the spread of disparity
	// among near things shrinks -- and the middle gains that spread.
	if near, was := curved[255]-curved[200], plain[255]-plain[200]; near >= was {
		t.Errorf("the near end spans %d pixels, no less than the %d it replaces", near, was)
	}
	if mid, was := curved[160]-curved[96], plain[160]-plain[96]; mid <= was {
		t.Errorf("the middle spans %d pixels, no more than the %d it replaces", mid, was)
	}
}

func TestACurveOfTheWrongLengthIsIgnoredRatherThanRefused(t *testing.T) {
	// This is on the path of every frame. An error to check per frame is an
	// error nobody checks, so a table that is not 256 entries is simply not a
	// table.
	if got := shiftFor([]byte{1, 2, 3}, 255, 24); got != 12 {
		t.Errorf("a three-entry curve changed the shift to %d, want 12", got)
	}
	if got := shiftFor(nil, 255, 24); got != 12 {
		t.Errorf("no curve gave %d, want 12", got)
	}
}

func TestTheSynthesisActuallyUsesTheCurve(t *testing.T) {
	const w, h = 64, 4
	src := stripes(w, h)
	// Depth 230 everywhere: 230*24/255/2 = 10 without a curve, less with one.
	plain, _ := Views(src, flat(w, h, 230), Options{MaxShift: 24})
	curved, _ := Views(src, flat(w, h, 230), Options{MaxShift: 24, Curve: Sigmoid(4)})
	if plain == nil || curved == nil {
		t.Fatal("no views")
	}
	const x = 40
	p, c := plain.Pix[2*plain.Stride+x*4], curved.Pix[2*curved.Stride+x*4]
	if p == c {
		t.Fatalf("the curve changed nothing: both eyes took the pixel from %d", p)
	}
	// It must move things FURTHER here, not merely differently: at depth 230
	// the S-curve is above the diagonal.
	if int(x)-int(c) <= int(x)-int(p) {
		t.Fatalf("with the curve the pixel came from %d, no further than %d", c, p)
	}
}
