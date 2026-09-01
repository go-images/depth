package depth

import (
	"fmt"
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
// an error to check on every one of them is an error nobody checks. Use
// [ViewsInto] when the reason matters, or when the pictures are being reused.
func Views(src *image.RGBA, m Map, opts Options) (left, right *image.RGBA) {
	if src == nil {
		return nil, nil
	}
	b := src.Bounds()
	left, right = rgba(b.Dx(), b.Dy()), rgba(b.Dx(), b.Dy())
	if err := ViewsInto(left, right, src, m, opts); err != nil {
		return nil, nil
	}
	return left, right
}

// ViewsInto is [Views] writing into pictures the caller already has.
//
// At 4K a pair of eyes is sixty-six megabytes. Allocating them per frame is
// most of a gigabyte a second of garbage, and on a telephone — where this
// package is the only path, there being no GPU binding and no neural engine —
// it was measured as three quarters of the frame time. Reuse the same two
// pictures and that cost disappears.
//
// left and right must be the same size as src, and neither may be src.
func ViewsInto(left, right, src *image.RGBA, m Map, opts Options) error {
	if src == nil || left == nil || right == nil {
		return fmt.Errorf("depth: a view was not given a picture to write into")
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return fmt.Errorf("depth: a source picture of %dx%d", w, h)
	}
	if !m.Valid() {
		return fmt.Errorf("depth: a depth map of %dx%d does not match its %d bytes",
			m.Width, m.Height, len(m.At))
	}
	for _, dst := range []*image.RGBA{left, right} {
		if d := dst.Bounds(); d.Dx() != w || d.Dy() != h {
			return fmt.Errorf("depth: writing a %dx%d view into a %dx%d picture",
				w, h, d.Dx(), d.Dy())
		}
		if dst == src {
			return fmt.Errorf("depth: a view cannot be written into its own source")
		}
	}

	maxShift := opts.maxShift()

	overRows(h, func(y0, y1 int) {
		// One scratch pair per stripe of rows, not per row: which SOURCE
		// column each output column takes its pixel from, or none.
		//
		// Working in indices rather than in pixels is what makes the holes
		// cheap. Filling them here costs a pass over w integers; doing it
		// afterwards on the picture costs reading and writing four bytes for
		// every pixel of both eyes, twice.
		//
		// It is also more correct. Marking a hole by leaving the pixel
		// transparent cannot tell a hole from a source pixel that was
		// transparent to begin with.
		from := make([]int32, 2*w)
		lf, rf := from[:w], from[w:]
		deep := make([]int16, 2*w)
		ld, rd := deep[:w], deep[w:]
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				lf[x], rf[x] = -1, -1
				ld[x], rd[x] = -1, -1
			}
			// One pass over the SOURCE columns, not a search per output
			// column. Each source pixel has exactly one destination in each
			// eye, and where two land on the same place the nearer wins --
			// which, walking left to right and keeping the last of equal
			// depths, is the same rule as painting far pixels first.
			//
			// Scattering within a row is not what the sort was avoiding: the
			// threads are split BY ROW, so nothing here is shared. Asking
			// instead what could have reached each output column costs a
			// short loop per pixel, and the depth map is sampled thirteen
			// times as often.
			for x := 0; x < w; x++ {
				d := m.sample(x, y, w, h)
				k := d * maxShift / 255 / 2
				if t := x + k; t < w && int16(d) >= ld[t] {
					ld[t], lf[t] = int16(d), int32(x)
				}
				if t := x - k; t >= 0 && int16(d) >= rd[t] {
					rd[t], rf[t] = int16(d), int32(x)
				}
			}
			closeHoles(lf)
			closeHoles(rf)
			gather(left, src, lf, y, w)
			gather(right, src, rf, y, w)
		}
	})
	return nil
}

// closeHoles covers the output columns nothing reached.
//
// A hole is where a near object moved aside and revealed something the camera
// never saw. There is nothing correct to put there, so the nearest filled
// column on the SAME ROW is stretched into it — a slightly smeared edge, which
// is what every real-time converter does. Left black instead, it is a
// flickering outline the eye finds instantly.
//
// Both directions, because a hole can open before any filled column on its row.
func closeHoles(from []int32) {
	last := int32(-1)
	for x, v := range from {
		if v >= 0 {
			last = v
		} else if last >= 0 {
			from[x] = last
		}
	}
	last = -1
	for x := len(from) - 1; x >= 0; x-- {
		if from[x] >= 0 {
			last = from[x]
		} else if last >= 0 {
			from[x] = last
		}
	}
}

// gather writes one row, taking each pixel from the column that reached it.
func gather(dst, src *image.RGBA, from []int32, y, w int) {
	drow := y*dst.Stride + dst.Rect.Min.X*4
	srow := y*src.Stride + src.Rect.Min.X*4
	for x := 0; x < w; x++ {
		if v := from[x]; v >= 0 {
			p, q := drow+x*4, srow+int(v)*4
			copy(dst.Pix[p:p+4], src.Pix[q:q+4])
		}
	}
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
