package depth

// Soften blurs a depth map, which is not cosmetic.
//
// A step in the map crossing the pixel grid makes an edge pixel jump about
// five pixels of disparity between one frame and the next, and an edge that
// jumps like that reads as boiling. It is REAL GEOMETRY rather than noise —
// the pixel genuinely belonged to the near surface and now belongs to the far
// one — which is why nothing temporal touches it: a median over three frames
// removed NONE of it, and an exponential average bought a fifth of it for four
// frames of lag.
//
// Softening the step spreads that jump over several frames instead. Measured
// over a slow pan of a real photograph, with a depth network's own map:
//
//	radius   worst edge movement   relief kept
//	     0             4.94 px          3.76
//	     1             1.79 px          3.67
//	     2             1.10 px          3.58
//	     4             0.63 px          3.44
//
// Radius 2 is the trade worth making: four fifths of the boiling gone for five
// per cent of the relief. Below half a pixel there is nothing left to see, so
// radius 4 buys little more.
//
// A box blur run twice separably, over the map rather than the picture, so it
// costs a fraction of a millisecond whatever size the frame is.
func Soften(m Map, radius int) Map {
	if !m.Valid() || radius < 1 {
		return m
	}
	tmp := make([]byte, len(m.At))
	out := make([]byte, len(m.At))
	w, h := m.Width, m.Height

	overRows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				sum, n := 0, 0
				for k := -radius; k <= radius; k++ {
					if sx := x + k; sx >= 0 && sx < w {
						sum += int(m.At[y*w+sx])
						n++
					}
				}
				tmp[y*w+x] = byte(sum / n)
			}
		}
	})
	overRows(h, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			for x := 0; x < w; x++ {
				sum, n := 0, 0
				for k := -radius; k <= radius; k++ {
					if sy := y + k; sy >= 0 && sy < h {
						sum += int(tmp[sy*w+x])
						n++
					}
				}
				out[y*w+x] = byte(sum / n)
			}
		}
	})
	return Map{Width: w, Height: h, At: out}
}
