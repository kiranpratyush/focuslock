// Package particles draws the cue's falling sparks: a few soft amber dots
// that drift down from the top line and fade away. It's only decoration, so
// it's kept faint and sparse.
//
// Everything here is plain Go drawing into a pixel buffer, so it can be
// tested anywhere. The Windows side (cmd/focuslock/cue_windows.go) just hands
// the finished buffer to the screen.
//
// Pixels are 32-bit BGRA with premultiplied alpha, the format Windows'
// UpdateLayeredWindow wants: each colour channel is already multiplied by the
// pixel's opacity, so a half-transparent amber is stored as half-bright amber.
package particles

import (
	"math"
	"math/rand"
)

type Color struct{ R, G, B uint8 }

type particle struct {
	x0, y     float64 // where it started across the width; how far down it is
	vy        float64 // falling speed, pixels per second
	r         float64 // radius in pixels
	peak      float64 // brightest opacity, 0..1
	sway      float64 // sideways drift, pixels
	phase     float64
	age, life float64 // seconds
}

// Field is the strip of screen the sparks fall through.
type Field struct {
	W, H  int     // size in pixels
	Rate  float64 // new sparks per second, across the whole width
	Max   int     // never more than this many at once
	Scale float64 // display scaling: 1 at 100%, 1.5 at 150%

	ps      []particle
	rng     *rand.Rand
	due     float64 // fractional sparks owed to the spawn rate
	Spawned int     // how many have been created (for tests)
}

// New makes a field w×h pixels. The defaults are deliberately sparse: about
// one spark a second per 650 pixels of width.
func New(w, h int, scale float64, seed int64) *Field {
	return &Field{
		W: w, H: h, Scale: scale,
		Rate: float64(w) / 650,
		Max:  max(6, w/110),
		rng:  rand.New(rand.NewSource(seed)),
	}
}

// Step moves time forward by dt seconds: sparks fall, fade, and new ones start.
func (f *Field) Step(dt float64) {
	if dt <= 0 {
		return
	}
	dt = math.Min(dt, 0.25) // after a pause, don't jump
	live := f.ps[:0]
	for _, p := range f.ps {
		p.age += dt
		p.y += p.vy * dt
		if p.age < p.life && p.y-p.r < float64(f.H) {
			live = append(live, p)
		}
	}
	f.ps = live

	f.due += f.Rate * dt
	for f.due >= 1 {
		f.due--
		if len(f.ps) < f.Max {
			f.ps = append(f.ps, f.spark())
			f.Spawned++
		}
	}
}

func (f *Field) spark() particle {
	r := f.rng.Float64
	s := f.Scale
	vy := (9 + 12*r()) * s // slow: 9-21 px a second at 100%
	return particle{
		x0:    r() * float64(f.W),
		y:     2 * s, // just under the line
		vy:    vy,
		r:     (0.8 + 0.9*r()) * s,
		peak:  0.35 + 0.35*r(), // never fully opaque
		sway:  (2 + 4*r()) * s,
		phase: r() * 2 * math.Pi,
		life:  float64(f.H) * (0.55 + 0.35*r()) / vy, // fades out before the bottom
	}
}

// Live is how many sparks are falling right now.
func (f *Field) Live() int { return len(f.ps) }

// opacity: a quick fade in, then a slow fade out toward the end of its life.
func (p particle) opacity() float64 {
	t := p.age / p.life
	in := math.Min(1, p.age/0.25)
	out := math.Pow(math.Max(0, 1-t), 1.6)
	return p.peak * in * out
}

// Draw paints every spark over buf (W*H pixels, row by row).
func (f *Field) Draw(buf []uint32, c Color) {
	for _, p := range f.ps {
		x := p.x0 + p.sway*math.Sin(p.phase+p.age*1.3)
		f.dot(buf, x, p.y, p.r, p.opacity(), c)
	}
}

// dot draws a soft disc: full opacity in the middle, a one-pixel soft edge.
func (f *Field) dot(buf []uint32, cx, cy, r, a float64, c Color) {
	x0, x1 := int(math.Floor(cx-r-1)), int(math.Ceil(cx+r+1))
	y0, y1 := int(math.Floor(cy-r-1)), int(math.Ceil(cy+r+1))
	for y := max(y0, 0); y <= min(y1, f.H-1); y++ {
		for x := max(x0, 0); x <= min(x1, f.W-1); x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			cover := math.Max(0, math.Min(1, r+0.5-d))
			if cover > 0 {
				i := y*f.W + x
				buf[i] = over(buf[i], c, a*cover)
			}
		}
	}
}

// Clear makes every pixel fully transparent.
func Clear(buf []uint32) {
	for i := range buf {
		buf[i] = 0
	}
}

// DrawLine paints the top `rows` rows of a w-wide buffer in c at opacity a.
func DrawLine(buf []uint32, w, rows int, c Color, a float64) {
	px := pixel(c, a)
	for i := 0; i < w*rows && i < len(buf); i++ {
		buf[i] = px
	}
}

// pixel is c at opacity a, premultiplied, as BGRA.
func pixel(c Color, a float64) uint32 {
	a = math.Max(0, math.Min(1, a))
	A := uint32(math.Round(a * 255))
	R := uint32(math.Round(float64(c.R) * a))
	G := uint32(math.Round(float64(c.G) * a))
	B := uint32(math.Round(float64(c.B) * a))
	return B | G<<8 | R<<16 | A<<24
}

// over draws c at opacity a on top of dst ("source over", premultiplied):
// result = source + destination × (1 − source opacity).
func over(dst uint32, c Color, a float64) uint32 {
	src := pixel(c, a)
	keep := 1 - float64(src>>24)/255
	ch := func(shift uint) uint32 {
		v := float64(src>>shift&0xFF) + float64(dst>>shift&0xFF)*keep
		return uint32(math.Min(255, math.Round(v)))
	}
	return ch(0) | ch(8)<<8 | ch(16)<<16 | ch(24)<<24
}
