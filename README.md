# depth

[![Go Reference](https://pkg.go.dev/badge/github.com/go-images/depth.svg)](https://pkg.go.dev/github.com/go-images/depth)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue.svg)](LICENSE)
[![Pure Go](https://img.shields.io/badge/pure%20Go-CGO%3D0-00ADD8?logo=go&logoColor=white)](https://github.com/go-images/depth)

**One picture in, two eyes out.** Portable Go, no cgo, no model file — the same
code on Linux, Windows, macOS and in a browser.

```go
m := depth.Cues(img)                 // no model, no network
m = depth.Soften(m, 2)
left, right := depth.Views(img, m, depth.Options{MaxShift: 24})
```

A player has the same two eyes every frame, and at 4K a fresh pair is
sixty-six megabytes. `ViewsInto` writes into pictures that already exist:

```go
err := depth.ViewsInto(left, right, img, m, depth.Options{MaxShift: 24})
```

`Views` takes any depth map, so a better one can be substituted without
touching anything else. On a Mac that is a real depth network on the Neural
Engine ([go-macos/coreml](https://github.com/go-macos/coreml)) and the same
synthesis as compute kernels ([go-macos/metal](https://github.com/go-macos/metal)).
This package is the answer where neither exists, and the definition of what
they must compute.

## One pass over the source, per row

The textbook way to move pixels sideways is to paint them all — far ones
first, so near ones land on top, which is the whole of the occlusion handling.
That needs a global sort of every pixel by depth.

What the sort is avoiding is two threads writing the same place. Splitting the
work **by row** already prevents that, and then each source pixel simply has
one destination in each eye, with the nearer winning where two collide.

Asking instead what could have **reached** each output column needs no sort
either, and that is what this package did first — but it costs a short search
per pixel and samples the depth map thirteen times as often. Replacing it was
worth **five times the speed** at 4K.

The answer did not change. Checked against a GPU implementation of the
painting rule, the two agree on **0 bytes out of 86 999 040**.

## Speed

Per frame, whole chain: depth from cues, softened, and both eyes synthesised.

| | 960×540 | 1920×1080 | 3840×2160 |
|---|---|---|---|
| M4 Max, 16 cores | 0.6 ms | 1.9 ms | 6.3 ms |
| Snapdragon 8+ Gen 1, 6 cores | 5.0 ms | 17.2 ms | 46.9 ms |

A telephone converts 1080p video to 3D **faster than it plays**, in portable
Go with no hardware acceleration of any kind — which matters, because there is
none to be had there: reaching Vulkan or the neural unit from Go needs cgo and
the Android NDK.

Those numbers are `ViewsInto`. Allocating a fresh pair per frame costs about
twice as much on the telephone at 540p, and nothing at all on the Mac.

## `Soften` is not cosmetic

A step in a depth map crossing the pixel grid makes an edge pixel jump about
five pixels of disparity between one frame and the next. An edge that jumps
like that reads as boiling, and it sits exactly where an eye looks.

It is **real geometry, not noise** — the pixel genuinely belonged to the near
surface and now belongs to the far one. That is why nothing temporal fixes it:
measured on a slow pan of a real photograph, a median over three frames removed
*none* of it, and an exponential average bought a fifth of it for four frames of
lag.

Softening the step spreads the jump over several frames instead:

| radius | worst edge movement | relief kept |
|---|---|---|
| 0 | 4.94 px | 3.76 |
| 1 | 1.79 px | 3.67 |
| **2** | **1.10 px** | **3.58** |
| 4 | 0.63 px | 3.44 |

Radius 2 is the trade worth making: four fifths of the boiling gone for five
per cent of the relief. Below half a pixel there is nothing left to see.

## The map is interpolated, not snapped

A depth map is routinely a different size from the picture — a network has its
own input size and does not care what it was given. At 4K one map pixel covers
four to seven of the image's.

Taking the **nearest** map pixel manufactures a depth step every time the
picture crosses a map pixel boundary. Measured on one photograph at 1080p:
**2099 depth steps where the map itself holds 28** — on a regular grid,
answering to nothing in the picture, and reading as a staircase across every
smooth surface. At 4K it was 3694 against 20.

The arithmetic is **integer throughout**, and that is not an optimisation: the
same synthesis exists as GPU kernels, and the two are checked against each
other byte for byte. Floating point would agree almost always, which is the
worst kind of agreement.

Both axes sample at **pixel centres**. Off by half a map pixel is three and a
half pixels of disparity in the wrong place at 4K.

## Reshaping depth before it becomes disparity

VITURE's own player reshapes the depth at this point — `sigmoidCoef` in its
Metal library — and this offers the same, as a **table of 256 entries** rather
than a formula. A table because the GPU implementation is checked against this
one byte for byte, and both can index the same bytes; a formula would have to
be reimplemented there in floating point and would agree almost always, which
is the worst kind of agreement.

```go
opts := depth.Options{MaxShift: 24, Curve: depth.Sigmoid(4)}
fmt.Println(depth.DisparityOf(opts.Curve, opts)) // what it costs, in pixels
```

`Sigmoid` flattens both ends of the range and expands the middle. Read that
precisely, because the obvious reading is wrong: what is compressed is the
**range** at each end, not the disparity there. A near object ends up with
slightly **more** disparity, while the differences *among* near objects shrink.
The background flattens into one plane, the foreground into another, and the
middle — where the subject usually is — gets the relief they gave up. It is not
a comfort control.

**And at a comfortable disparity it barely matters.** Twenty-four pixels
between the eyes is twelve each way, so the whole depth range has thirteen
distinct shifts to spend, and quantisation swamps any reshaping of it: measured
at the default, a sigmoid of strength 4 changes the disparity of fewer than half
the depth values, by one pixel. It earns its keep only when the disparity is
large. `DisparityOf` is there so that can be checked rather than assumed.

## `Cues` is three honest guesses

Where there is no model, depth is estimated from the picture alone: what is
lower in the frame is usually nearer, what is sharp is usually what was focused
on, and what is neither very bright nor very dark tends to be the subject
rather than sky or shadow.

It is wrong about a photograph of a wall and wrong about a picture taken
looking down, and it costs a fraction of a millisecond. A real depth network is
not comparable — but it needs a Mac, or a GPU, or a download, and this needs
none of them.

## Both eyes move

Each pixel moves by half its disparity, one eye each way, so the original stays
in the middle. Generating only the second eye is cheaper and wrong: the whole
film then feels as though it has slid sideways.

`MaxShift` is small on purpose. A large disparity makes an impressive still and
an unwatchable film, because the eyes must converge differently on every cut.

## Holes

Where a near object moves aside it reveals something the camera never saw.
There is nothing correct to put there, so the nearest filled pixel on the same
row is stretched into it — a slightly smeared edge, which is what every
real-time converter does. Left black instead, it is a flickering outline the
eye finds instantly. Both directions, because a hole can open before any filled
pixel on its row.

## Install

```
go get github.com/go-images/depth
```

CGO_ENABLED=0, no dependencies at all, 100% test coverage on every platform.
