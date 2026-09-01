package depth

import (
	"image"
	"testing"
)

// stripes is a picture whose every pixel says where it came from, so a view
// synthesised from it can be checked by reading it rather than by eye.
func stripes(w, h int) *image.RGBA {
	img := rgba(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := y*img.Stride + x*4
			img.Pix[p], img.Pix[p+1], img.Pix[p+2], img.Pix[p+3] = byte(x), byte(x), byte(x), 255
		}
	}
	return img
}

func flat(w, h int, v byte) Map {
	m := Map{Width: w, Height: h, At: make([]byte, w*h)}
	for i := range m.At {
		m.At[i] = v
	}
	return m
}

func TestAMapKnowsWhetherItHangsTogether(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    Map
		want bool
	}{
		{"a real map", Map{Width: 2, Height: 2, At: make([]byte, 4)}, true},
		{"no width", Map{Width: 0, Height: 2, At: make([]byte, 4)}, false},
		{"no height", Map{Width: 2, Height: 0, At: make([]byte, 4)}, false},
		{"too few bytes", Map{Width: 2, Height: 2, At: make([]byte, 3)}, false},
		{"too many bytes", Map{Width: 2, Height: 2, At: make([]byte, 5)}, false},
		{"nothing at all", Map{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.Valid(); got != tc.want {
				t.Fatalf("Valid = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheDefaultDisparityIsUsedWhenNoneIsAsked(t *testing.T) {
	if got := (Options{}).maxShift(); got != 24 {
		t.Errorf("no MaxShift gave %d, want 24", got)
	}
	if got := (Options{MaxShift: -5}).maxShift(); got != 24 {
		t.Errorf("a negative MaxShift gave %d, want 24", got)
	}
	if got := (Options{MaxShift: 8}).maxShift(); got != 8 {
		t.Errorf("MaxShift 8 gave %d", got)
	}
}

func TestTheMapIsSampledByScaleNotByIndex(t *testing.T) {
	// A depth network has its own input size and does not care what it was
	// given, so a map is routinely a different size from the picture. Indexing
	// it directly would read the wrong pixel and shear the whole image.
	m := Map{Width: 2, Height: 1, At: []byte{10, 200}}
	if got := m.sample(0, 0, 8, 4); got != 10 {
		t.Errorf("left of a wide picture sampled %d, want 10", got)
	}
	if got := m.sample(7, 3, 8, 4); got != 200 {
		t.Errorf("right of a wide picture sampled %d, want 200", got)
	}
}

func TestEachEyeMovesThePictureTheOtherWayByTheDepthItWasGiven(t *testing.T) {
	const w, h, maxShift = 64, 4, 24
	src := stripes(w, h)
	// Depth 255 everywhere, so 255*24/255/2 = 12 pixels each way: the expected
	// answer is arithmetic rather than a guess.
	left, right := Views(src, flat(w, h, 255), Options{MaxShift: maxShift})
	if left == nil || right == nil {
		t.Fatal("no views")
	}
	const k = 12
	for _, x := range []int{20, 40, 50} {
		if got := left.Pix[2*left.Stride+x*4]; got != byte(x-k) {
			t.Errorf("left at x=%d is %d, want %d (the pixel from x-%d)", x, got, x-k, k)
		}
		if got := right.Pix[2*right.Stride+x*4]; got != byte(x+k) {
			t.Errorf("right at x=%d is %d, want %d (the pixel from x+%d)", x, got, x+k, k)
		}
	}
}

func TestDepthZeroLeavesThePictureExactlyWhereItWas(t *testing.T) {
	// The negative control. Without it, a synthesis that moved everything by a
	// fixed amount regardless of depth would pass the test above.
	const w, h = 64, 4
	src := stripes(w, h)
	left, right := Views(src, flat(w, h, 0), Options{MaxShift: 24})
	for x := 0; x < w; x++ {
		if left.Pix[x*4] != byte(x) || right.Pix[x*4] != byte(x) {
			t.Fatalf("at x=%d, depth zero moved the picture to %d/%d", x, left.Pix[x*4], right.Pix[x*4])
		}
	}
}

func TestNoHoleIsLeftBehind(t *testing.T) {
	// Every pixel must end up opaque. A hole left transparent is a black
	// flickering outline, and it is the first thing an eye finds.
	const w, h = 96, 8
	src := stripes(w, h)
	m := Map{Width: w, Height: h, At: make([]byte, w*h)}
	for y := 0; y < h; y++ {
		for x := 31; x < 60; x++ {
			m.At[y*w+x] = 255 // a near block against a far background
		}
	}
	left, right := Views(src, m, Options{MaxShift: 24})
	for i := 3; i < len(left.Pix); i += 4 {
		if left.Pix[i] == 0 || right.Pix[i] == 0 {
			t.Fatalf("a hole survived at byte %d", i)
		}
	}
}

func TestAHoleAtTheVeryStartOfARowIsFilledFromTheRight(t *testing.T) {
	// The left-to-right pass has nothing to copy from until it meets its first
	// filled column, so a hole at x=0 is only covered by the second pass. With
	// one pass there is a black column down the edge of every frame.
	from := []int32{-1, -1, 7, 9, -1, -1}
	closeHoles(from)
	want := []int32{7, 7, 7, 9, 9, 9}
	for i := range want {
		if from[i] != want[i] {
			t.Fatalf("closeHoles gave %v, want %v", from, want)
		}
	}

	// A row nothing reached at all stays empty rather than inventing a column.
	none := []int32{-1, -1, -1}
	closeHoles(none)
	for i, v := range none {
		if v != -1 {
			t.Fatalf("an empty row filled itself at %d with %d", i, v)
		}
	}
}

func TestViewsRefusesWhatItCannotSynthesise(t *testing.T) {
	good := stripes(8, 8)
	for _, tc := range []struct {
		name string
		src  *image.RGBA
		m    Map
	}{
		{"no picture", nil, flat(8, 8, 128)},
		{"no map", good, Map{}},
		{"a map that does not hang together", good, Map{Width: 8, Height: 8, At: make([]byte, 3)}},
		{"a picture of nothing", rgba(0, 0), flat(8, 8, 128)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if l, r := Views(tc.src, tc.m, Options{}); l != nil || r != nil {
				t.Fatal("it produced views anyway")
			}
		})
	}
	if l, _ := Views(good, flat(8, 8, 128), Options{}); l == nil {
		t.Fatal("a valid call was refused")
	}
}

func TestSofteningFlattensAStepWithoutMovingIt(t *testing.T) {
	m := Map{Width: 8, Height: 1, At: []byte{0, 0, 0, 0, 255, 255, 255, 255}}
	got := Soften(m, 1)
	if got.At[3] == 0 || got.At[4] == 255 {
		t.Errorf("the step was not softened: %v", got.At)
	}
	if got.At[0] > got.At[3] || got.At[4] > got.At[7] {
		t.Errorf("softening reordered the map: %v", got.At)
	}
	if got.At[0] >= got.At[7] {
		t.Errorf("softening flattened the relief away: %v", got.At)
	}
}

func TestSofteningLeavesAloneWhatItCannotHelp(t *testing.T) {
	m := flat(4, 4, 100)
	if got := Soften(m, 0); &got.At[0] != &m.At[0] {
		t.Error("radius 0 copied the map instead of returning it")
	}
	bad := Map{Width: 4, Height: 4, At: make([]byte, 3)}
	if got := Soften(bad, 2); got.Width != 4 || len(got.At) != 3 {
		t.Error("a map that does not hang together was altered")
	}
}

func TestCuesPutsTheBottomOfTheFrameNearerThanTheTop(t *testing.T) {
	// The ground cue carries most of the weight, so on a picture with nothing
	// else in it the bottom must come out nearer. If this ever inverts, every
	// scene turns inside out and no smaller test would notice.
	src := rgba(64, 64)
	for i := 3; i < len(src.Pix); i += 4 {
		src.Pix[i] = 255
	}
	m := Cues(src)
	if !m.Valid() {
		t.Fatalf("Cues gave %dx%d with %d bytes", m.Width, m.Height, len(m.At))
	}
	if bottom, top := m.At[(m.Height-1)*m.Width], m.At[0]; bottom <= top {
		t.Fatalf("bottom %d is not nearer than top %d", bottom, top)
	}
}

func TestCuesSurvivesPicturesTooSmallToQuarter(t *testing.T) {
	// Quartering a picture of twelve pixels leaves three, and the sharpness
	// pass needs a neighbour on each side. The divisor has to give way.
	for _, size := range []int{1, 3, 12, 40} {
		if m := Cues(stripes(size, size)); !m.Valid() {
			t.Errorf("a %dx%d picture gave %dx%d with %d bytes",
				size, size, m.Width, m.Height, len(m.At))
		}
	}
	if m := Cues(nil); m.Valid() {
		t.Error("no picture gave a valid map")
	}
	if m := Cues(rgba(0, 0)); m.Valid() {
		t.Error("a picture of nothing gave a valid map")
	}
}

func TestOverRowsDoesNothingWithNoRows(t *testing.T) {
	called := false
	overRows(0, func(int, int) { called = true })
	if called {
		t.Error("it ran a stripe of no rows")
	}
}

func TestTheLuminanceCuePrefersTheMiddleToEitherExtreme(t *testing.T) {
	// The third cue says a mid-grey surface is more likely to be the subject
	// than sky or shadow. On a uniform picture it is the ONLY term that can
	// differ, so this measures it alone -- and it is the branch a picture made
	// of dark stripes never reaches.
	depthOf := func(v byte) byte {
		src := rgba(64, 64)
		for i := 0; i < len(src.Pix); i += 4 {
			src.Pix[i], src.Pix[i+1], src.Pix[i+2], src.Pix[i+3] = v, v, v, 255
		}
		m := Cues(src)
		return m.At[m.Width/2] // top row, so the ground cue is out of the way
	}
	grey, white, black := depthOf(128), depthOf(255), depthOf(0)
	if grey <= white || grey <= black {
		t.Fatalf("mid-grey %d is not nearer than white %d or black %d", grey, white, black)
	}
}

func TestViewsIntoReusesPicturesAndSaysWhyWhenItCannot(t *testing.T) {
	const w, h = 32, 8
	src := stripes(w, h)
	left, right := rgba(w, h), rgba(w, h)
	if err := ViewsInto(left, right, src, flat(w, h, 255), Options{MaxShift: 24}); err != nil {
		t.Fatal(err)
	}
	// The same pair again, over the top of the last answer. Reuse is the whole
	// point of this entry point, and a synthesis that relied on its
	// destination starting empty would leave the previous frame showing
	// through wherever this one has a hole.
	if err := ViewsInto(left, right, src, flat(w, h, 0), Options{MaxShift: 24}); err != nil {
		t.Fatal(err)
	}
	for x := 0; x < w; x++ {
		if got := left.Pix[x*4]; got != byte(x) {
			t.Fatalf("after reuse, left at x=%d is %d, want %d", x, got, x)
		}
	}

	small := rgba(w/2, h)
	for _, tc := range []struct {
		name             string
		left, right, src *image.RGBA
		m                Map
	}{
		{"no left", nil, right, src, flat(w, h, 128)},
		{"no right", left, nil, src, flat(w, h, 128)},
		{"no source", left, right, nil, flat(w, h, 128)},
		{"a source of nothing", left, right, rgba(0, 0), flat(w, h, 128)},
		{"a map that does not hang together", left, right, src, Map{Width: w, Height: h, At: make([]byte, 3)}},
		{"a left eye of the wrong size", small, right, src, flat(w, h, 128)},
		{"a right eye of the wrong size", left, small, src, flat(w, h, 128)},
		{"writing over the source", src, right, src, flat(w, h, 128)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ViewsInto(tc.left, tc.right, tc.src, tc.m, Options{})
			if err == nil {
				t.Fatal("it was accepted")
			}
			if err.Error() == "" {
				t.Error("the refusal says nothing")
			}
		})
	}
}

func TestSampleAtInterpolatesRatherThanSnapping(t *testing.T) {
	// A two-pixel map stretched across eight picture pixels. Taking the
	// nearest map pixel gives four 10s then four 200s -- one step, on a grid
	// the picture knows nothing about. Interpolating gives a ramp.
	m := Map{Width: 2, Height: 1, At: []byte{10, 200}}
	var got []int
	for x := 0; x < 8; x++ {
		got = append(got, m.SampleAt(x, 0, 8, 1))
	}
	steps := 0
	for i := 1; i < len(got); i++ {
		if d := got[i] - got[i-1]; d > 60 {
			steps++
		}
		if got[i] < got[i-1] {
			t.Fatalf("the ramp goes backwards at %d: %v", i, got)
		}
	}
	if steps != 0 {
		t.Fatalf("%d abrupt step(s) in %v; interpolation should have none", steps, got)
	}
	if got[0] != 10 || got[7] != 200 {
		t.Fatalf("the ends moved: %v", got)
	}

	// Both samples are taken at PIXEL CENTRES. If they were not, the whole
	// picture shifts by half a map pixel -- three and a half pixels of
	// disparity in the wrong place at 4K.
	if mid := m.SampleAt(3, 0, 8, 1); mid < 80 || mid > 130 {
		t.Errorf("the middle of the ramp is %d, which is not near the middle", mid)
	}

	// And it must answer safely for a map or a size it cannot use.
	if v := (Map{}).SampleAt(0, 0, 4, 4); v != 0 {
		t.Errorf("an invalid map sampled %d", v)
	}
	if v := m.SampleAt(0, 0, 0, 0); v != 0 {
		t.Errorf("a picture of no size sampled %d", v)
	}
	// Out of range coordinates are pinned rather than read out of bounds.
	if m.SampleAt(-5, -5, 8, 1) != m.SampleAt(0, 0, 8, 1) {
		t.Error("a negative coordinate was not pinned to the edge")
	}
	if m.SampleAt(99, 99, 8, 1) != m.SampleAt(7, 0, 8, 1) {
		t.Error("a coordinate past the end was not pinned to the edge")
	}
}
