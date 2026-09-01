package depth

import "math"

// A curve reshapes depth before it becomes disparity.
//
// The depth a network gives is relative and roughly uniform; turning it
// straight into a sideways shift spends the same amount of the disparity
// budget on the last centimetre in front of the camera as on the middle of the
// scene, where the subject usually is. VITURE's own player reshapes it at
// exactly this point (`sigmoidCoef` in its Metal library).
//
// It is a TABLE of 256 entries, not a formula, and that is deliberate. The
// same synthesis runs as GPU kernels and the two are checked against each
// other byte for byte; a formula would have to be reimplemented there in
// floating point and would agree almost always, which is the worst kind of
// agreement. A table is indexed identically by both.

// Sigmoid builds an S-curve: it flattens both ends of the depth range and
// expands the middle.
//
// Read that precisely, because the obvious reading is wrong. What is
// compressed is the RANGE at each end, not the disparity there: a near object
// ends up with slightly MORE disparity than a straight proportional shift
// would give it, while the differences AMONG near objects shrink. The far
// background flattens into one plane, the near foreground into another, and
// the middle — where the subject usually is — gets the relief they gave up.
//
// So this makes the subject stand out. It is not a comfort control: a curve
// that made a close object easier on the eyes would have to lower the top of
// the range, and this raises it.
//
// strength is how hard: 0 or less returns nil, which means no curve at all.
// Around 4 is a gentle S; 10 is severe. There is no measured "right" value
// here and this package does not pretend otherwise — what it costs and buys is
// visible in DisparityOf, and what it LOOKS like needs a headset and a person.
func Sigmoid(strength float64) []byte {
	// Infinity is rejected with the rest: at exactly the middle it would ask
	// for Inf times zero, which is not a number, and the table would fill with
	// whatever a NaN converts to.
	if strength <= 0 || math.IsNaN(strength) || math.IsInf(strength, 1) {
		return nil
	}
	s := func(x float64) float64 { return 1 / (1 + math.Exp(-strength*(x-0.5))) }
	lo, hi := s(0), s(1)
	// span cannot be zero for a finite positive strength: s is strictly
	// increasing, so s(1) > s(0). No guard here, because a branch no test can
	// reach reads as a case that happens.
	span := hi - lo
	out := make([]byte, 256)
	for i := range out {
		v := (s(float64(i)/255) - lo) / span
		out[i] = byte(min(max(v, 0), 1)*255 + 0.5)
	}
	return out
}

// DisparityOf is what a curve does, in the unit that matters: how many pixels
// apart the two eyes put a thing at each depth.
//
// It exists so a curve can be judged by a number rather than by adjectives.
func DisparityOf(curve []byte, opts Options) [256]int {
	var out [256]int
	maxShift := opts.maxShift()
	for d := range out {
		out[d] = shiftFor(curve, d, maxShift)
	}
	return out
}

// shiftFor is the whole of the depth-to-disparity rule, in one place so the
// synthesis and the measurement cannot drift apart.
func shiftFor(curve []byte, d, maxShift int) int {
	if len(curve) == 256 {
		d = int(curve[d])
	}
	return d * maxShift / 255 / 2
}
