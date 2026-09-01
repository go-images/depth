// Package depth turns one picture into two, for a headset or a 3D display.
//
// It does two separable things. It estimates how far away each pixel is —
// either from cues in the picture itself, with no model and no network, or
// from a Map you supply from something better. And it synthesises the two eyes
// from a picture and a Map.
//
//	m := depth.Cues(img)
//	m = depth.Soften(m, 2)
//	left, right := depth.Views(img, m, depth.Options{MaxShift: 24})
//
// Everything here is portable Go with no cgo: it runs the same on Linux, on
// Windows, in a browser and on a Mac. On a Mac, a far better Map comes from a
// depth network on the Neural Engine (go-macos/coreml) and the synthesis can
// be done by compute kernels (go-macos/metal) — this package is the answer
// where those are not available, and the definition of what they must compute.
//
// # Three things here were measured rather than assumed
//
// The synthesis is ONE PASS OVER THE SOURCE columns, per row. The textbook way
// to move pixels sideways is to paint them all, far ones first, so near ones
// land on top -- but that needs a global sort of every pixel by depth. What
// the sort avoids is two threads writing the same place, and splitting the
// work BY ROW already prevents it: within one row each source pixel has one
// destination in each eye, and where two collide the nearer wins.
//
// Asking instead what could have REACHED each output column needs no sort
// either, which is what this package did first -- but it costs a short search
// per pixel and samples the depth map thirteen times as often. Replacing it
// was worth five times the speed at 4K, and the answer did not change:
// checked against a GPU implementation of the painting rule, the two agree on
// 0 bytes out of 86 999 040.
//
// Soften is not cosmetic. A step in a depth map crossing the pixel grid makes
// an edge pixel jump five pixels of disparity between one frame and the next —
// which reads as an edge boiling. It is REAL GEOMETRY, not noise: a temporal
// median removes none of it. Softening the step spreads the jump over several
// frames. Measured over a slow pan, radius 2 took the worst edge movement from
// 4.94 pixels to 1.10, for 4.8% less relief in the map.
//
// MaxShift is small on purpose. A large disparity makes an impressive still
// and an unwatchable film, because the eyes must converge differently on every
// cut.
package depth

import "image"

// Map is how far away each pixel is: one byte each, 0 furthest, 255 nearest.
//
// The scale is relative and says nothing about metres. It only has to order
// the surfaces correctly, which is all the synthesis asks of it.
type Map struct {
	Width, Height int
	At            []byte
}

// Valid reports whether the map's size and its bytes agree. A Map built by
// hand from a network's output is the likely caller, and one whose stride is
// wrong produces a picture that shears rather than an error.
func (m Map) Valid() bool {
	return m.Width > 0 && m.Height > 0 && len(m.At) == m.Width*m.Height
}

// Options control the synthesis.
type Options struct {
	// MaxShift is the disparity of the nearest thing, in pixels of the
	// source: the total between the two eyes, half of it each way, so that
	// the original stays in the middle. Zero means 24, which is comfortable
	// for a headset at arm's length.
	//
	// Generating only the second eye and leaving the first alone is cheaper
	// and wrong: the whole film then feels as though it has slid sideways.
	MaxShift int

	// Curve reshapes depth before it becomes disparity: 256 entries, indexed by
	// the depth byte. Nil means none, which is a straight proportional shift.
	//
	// Build one with Sigmoid, and see what it does with DisparityOf. A table
	// rather than a formula because the GPU implementation of this synthesis is
	// checked against it byte for byte, and both can index the same table.
	//
	// A slice of any other length is ignored rather than refused: this is on
	// the path of every frame, and a per-frame error is an error nobody checks.
	Curve []byte
}

func (o Options) maxShift() int {
	if o.MaxShift <= 0 {
		return 24
	}
	return o.MaxShift
}

// sample reads the map at a picture coordinate, interpolating between the four
// map pixels around it.
//
// The map is routinely a different SIZE from the picture — a depth network has
// its own input size and does not care what it was given — so this scales
// rather than assuming. Taking the NEAREST map pixel instead is what a first
// version did, and it manufactures a depth step every time the picture crosses
// a map pixel boundary: measured on one photograph at 1080p, 2099 steps where
// the map itself contains 28. They sit on a regular grid, they answer to
// nothing in the picture, and they read as a staircase across every smooth
// surface.
//
// The arithmetic is INTEGER throughout, and that is not an optimisation: the
// same synthesis is implemented on a GPU, and the two are checked against each
// other byte for byte. Floating point would agree almost always, which is the
// worst kind of agreement.
func (m Map) sample(x, y, w, h int) int {
	hi, ti, di := lerp(x, w, m.Width)
	hj, tj, dj := lerp(y, h, m.Height)
	lo_i, lo_j := max(hi, 0), max(hj, 0)
	hi_i, hi_j := min(hi+1, m.Width-1), min(hj+1, m.Height-1)
	at := func(a, b int) int { return int(m.At[b*m.Width+a]) }
	top := at(lo_i, lo_j)*(di-ti) + at(hi_i, lo_j)*ti
	bot := at(lo_i, hi_j)*(di-ti) + at(hi_i, hi_j)*ti
	return (top*(dj-tj) + bot*tj + di*dj/2) / (di * dj)
}

// lerp places picture coordinate v inside a map of size to, and returns the
// lower map index with the fraction to the next one as an exact ratio.
//
// Both samples are taken at PIXEL CENTRES — the halves below — or the whole
// image shifts by half a map pixel, which at 4K is three and a half pixels of
// disparity in the wrong place.
func lerp(v, from, to int) (idx, num, den int) {
	den = 2 * from
	p := (2*v+1)*to - from // = den * (centre in map coordinates - 0.5)
	idx = p / den
	if p < 0 && p%den != 0 {
		idx--
	}
	return idx, p - idx*den, den
}

func rgba(w, h int) *image.RGBA { return image.NewRGBA(image.Rect(0, 0, w, h)) }

// SampleAt exposes the map's own interpolation, at a picture coordinate in a
// picture of the given size.
//
// It exists because the alternative — a measurement tool reimplementing the
// sampling it means to measure — measures the reimplementation.
func (m Map) SampleAt(x, y, w, h int) int {
	if !m.Valid() || w < 1 || h < 1 {
		return 0
	}
	return m.sample(min(max(x, 0), w-1), min(max(y, 0), h-1), w, h)
}
