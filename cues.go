package depth

import "image"

// Cues estimates depth from the picture alone, with no model and no network.
//
// It is three cheap agreements about how photographs are usually taken, and
// nothing more:
//
//   - what is lower in the frame is usually nearer, which is the ground under
//     the camera and carries most of the weight;
//   - what is sharp is usually what was focused on, and therefore nearer;
//   - what is neither very bright nor very dark tends to be the subject rather
//     than sky or shadow.
//
// It is wrong about a photograph of a wall and wrong about a picture taken
// looking down, and it costs a fraction of a millisecond. Where a real depth
// network is available — go-macos/coreml on a Mac — its map is not comparable,
// and Views takes either.
//
// The estimate is made at quarter resolution and smoothed, because the
// sharpness term is per-pixel noise until it is: an unsmoothed cue map moves
// individual pixels sideways and looks like grain crawling over the picture.
func Cues(src *image.RGBA) Map {
	if src == nil {
		return Map{}
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return Map{}
	}
	div := 4
	for div > 1 && (w/div < 8 || h/div < 8) {
		div--
	}
	sw, sh := max(w/div, 1), max(h/div, 1)

	lum := make([]byte, sw*sh)
	overRows(sh, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < sw; x++ {
				p := (y*div)*src.Stride + (x*div)*4
				lum[y*sw+x] = byte((int(src.Pix[p])*54 + int(src.Pix[p+1])*183 + int(src.Pix[p+2])*19) >> 8)
			}
		}
	})

	// Local detail: the mean absolute difference from the four neighbours,
	// which is a Laplacian without the multiplications. A separable Sobel plus
	// a magnitude costs more than the map it feeds.
	sharp := make([]byte, sw*sh)
	overRows(sh, func(y0, y1 int) {
		for y := max(y0, 1); y < min(y1, sh-1); y++ {
			for x := 1; x < sw-1; x++ {
				i := y*sw + x
				c := int(lum[i])
				d := (abs(int(lum[i-1])-c) + abs(int(lum[i+1])-c) +
					abs(int(lum[i-sw])-c) + abs(int(lum[i+sw])-c)) * 4
				sharp[i] = byte(min(d, 255))
			}
		}
	})

	out := Map{Width: sw, Height: sh, At: make([]byte, sw*sh)}
	den := max(sh-1, 1)
	overRows(sh, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			ground := float64(y) / float64(den)
			for x := 0; x < sw; x++ {
				i := y*sw + x
				l := float64(lum[i]) / 255
				// No clamp: each of the three terms is in [0,1] by construction
				// and the weights sum to one, so d cannot leave [0,1]. A guard
				// here would be a branch no test could ever reach, which is
				// worse than none -- it reads as a case that happens.
				d := 0.62*ground + 0.28*float64(sharp[i])/255 + 0.10*(1-absF(l-0.5)*2)
				out.At[i] = byte(d*255 + 0.5)
			}
		}
	})
	return Soften(out, max(12/div, 1))
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
