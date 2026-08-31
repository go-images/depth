package depth

import (
	"image"
	"runtime"
	"sync"
)

// Views synthesises the two eyes from one picture and a depth map.
//
// Each pixel moves sideways by half the disparity its depth calls for, one eye
// each way, so that the original stays in the middle.
//
// It gathers: every OUTPUT pixel asks which source pixels could have reached it
// and keeps the nearest. That is the same rule as painting far pixels first and
// letting near ones land on top — last-writer-wins and highest-depth-wins are
// the same thing — but with no global sort and no two threads writing the same
// place, so it divides cleanly over the cores.
//
// A nil source or an invalid map returns nil, nil: this is called per frame and
// an error to check on every one of them is an error nobody checks.
func Views(src *image.RGBA, m Map, opts Options) (left, right *image.RGBA) {
	if src == nil || !m.Valid() {
		return nil, nil
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return nil, nil
	}
	maxShift := opts.maxShift()
	half := maxShift / 2
	left, right = rgba(w, h), rgba(w, h)

	overRows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				o := y*left.Stride + x*4

				// The left eye moves things RIGHT, so its candidates are at or
				// before x. Equal depths prefer the later candidate, which is
				// what a stable sort by depth would have painted last.
				best, bestD := -1, -1
				for k := 0; k <= half && k <= x; k++ {
					sx := x - k
					d := m.sample(sx, y, w, h)
					if d*maxShift/255/2 == k && d >= bestD {
						bestD, best = d, sx
					}
				}
				if best >= 0 {
					p := y*src.Stride + best*4
					copy(left.Pix[o:o+4], src.Pix[p:p+4])
				}

				best, bestD = -1, -1
				for k := 0; k <= half && x+k < w; k++ {
					sx := x + k
					d := m.sample(sx, y, w, h)
					if d*maxShift/255/2 == k && d >= bestD {
						bestD, best = d, sx
					}
				}
				if best >= 0 {
					p := y*src.Stride + best*4
					copy(right.Pix[o:o+4], src.Pix[p:p+4])
				}
			}
		}
	})
	fillHoles(left)
	fillHoles(right)
	return left, right
}

// fillHoles covers what the shift left empty.
//
// A hole is where a near object moved aside and revealed something the camera
// never saw. There is nothing correct to put there, so the nearest filled pixel
// on the SAME ROW is stretched into it — a slightly smeared edge, which is what
// every real-time converter does. Left black instead, it is a flickering
// outline that the eye finds instantly.
//
// Both directions, because a hole can open before any filled pixel on its row.
func fillHoles(img *image.RGBA) {
	h := img.Bounds().Dy()
	w := img.Bounds().Dx()
	overRows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			row := y * img.Stride
			last := -1
			for x := 0; x < w; x++ {
				p := row + x*4
				if img.Pix[p+3] != 0 {
					last = x
				} else if last >= 0 {
					copy(img.Pix[p:p+4], img.Pix[row+last*4:row+last*4+4])
				}
			}
			last = -1
			for x := w - 1; x >= 0; x-- {
				p := row + x*4
				if img.Pix[p+3] != 0 {
					last = x
				} else if last >= 0 {
					copy(img.Pix[p:p+4], img.Pix[row+last*4:row+last*4+4])
				}
			}
		}
	})
}

// overRows runs fn over stripes of rows, one per core. Rows are independent
// here — that is the property the gather bought — so there is nothing to lock.
func overRows(h int, fn func(y0, y1 int)) {
	n := min(runtime.GOMAXPROCS(0), h)
	if n < 1 {
		return
	}
	step := (h + n - 1) / n
	var wg sync.WaitGroup
	for y0 := 0; y0 < h; y0 += step {
		wg.Add(1)
		go func(a, b int) { defer wg.Done(); fn(a, b) }(y0, min(y0+step, h))
	}
	wg.Wait()
}
