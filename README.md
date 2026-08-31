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

`Views` takes any depth map, so a better one can be substituted without
touching anything else. On a Mac that is a real depth network on the Neural
Engine ([go-macos/coreml](https://github.com/go-macos/coreml)) and the same
synthesis as compute kernels ([go-macos/metal](https://github.com/go-macos/metal)).
This package is the answer where neither exists, and the definition of what
they must compute.

## The synthesis gathers, it does not scatter

The obvious way to move pixels sideways is to paint them — far ones first, so
near ones land on top, which is the whole of the occlusion handling. But that
needs a global sort of every pixel by depth, and it forbids two threads from
sharing a row.

Turned around, each **output** pixel asks which source pixels could have
reached it and keeps the nearest. Last-writer-wins and highest-depth-wins are
the same rule, so it is the same answer — with no sort and no shared write.

At 4K on sixteen cores that was 2.2× faster, and checked against a GPU
implementation of the painting version it agreed on **0 bytes out of
66 355 200**.

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
