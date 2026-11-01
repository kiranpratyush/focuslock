package particles

import (
	"math"
	"testing"
)

var amber = Color{250, 204, 21}

func TestSparseAndBounded(t *testing.T) {
	f := New(2560, 56, 1, 1)
	for i := 0; i < 25*30; i++ { // 30 seconds at 25 frames a second
		f.Step(1.0 / 25)
		if f.Live() > f.Max {
			t.Fatalf("frame %d: %d sparks, max %d", i, f.Live(), f.Max)
		}
	}
	// About one a second per 650 px: ~3.9/s on a 2560 px screen.
	if perSec := float64(f.Spawned) / 30; perSec < 3 || perSec > 4.5 {
		t.Errorf("spawn rate %.1f/s, want about 3.9", perSec)
	}
	for _, p := range f.ps {
		if p.y > float64(f.H)+p.r || p.age > p.life {
			t.Errorf("spark should be gone: %+v", p)
		}
	}
}

func TestFadesInAndOut(t *testing.T) {
	p := particle{peak: 0.6, life: 3}
	at := func(age float64) float64 { p.age = age; return p.opacity() }
	if at(0) != 0 || at(0.1) >= at(0.25) {
		t.Errorf("should fade in: %.2f %.2f %.2f", at(0), at(0.1), at(0.25))
	}
	if !(at(0.5) > at(1.5) && at(1.5) > at(2.8) && at(3) == 0) {
		t.Errorf("should fade out: %.2f %.2f %.2f %.2f", at(0.5), at(1.5), at(2.8), at(3))
	}
	if at(0.3) > 0.6 {
		t.Errorf("never brighter than its peak")
	}
}

func TestPixelsArePremultiplied(t *testing.T) {
	f := New(400, 56, 1.5, 7)
	for i := 0; i < 100; i++ {
		f.Step(0.04)
	}
	buf := make([]uint32, f.W*f.H)
	DrawLine(buf, f.W, 3, amber, 0.5)
	f.Draw(buf, amber)
	painted := 0
	for i, px := range buf {
		a, r, g, b := px>>24, px>>16&0xFF, px>>8&0xFF, px&0xFF
		if r > a || g > a || b > a {
			t.Fatalf("pixel %d not premultiplied: a=%d r=%d g=%d b=%d", i, a, r, g, b)
		}
		if a > 0 && i >= 3*f.W {
			painted++
		}
	}
	if painted == 0 {
		t.Error("no sparks drawn below the line")
	}
}

func TestLineAndOver(t *testing.T) {
	buf := make([]uint32, 10*4)
	DrawLine(buf, 10, 2, amber, 1)
	if buf[0] != pixel(amber, 1) || buf[19] != pixel(amber, 1) || buf[20] != 0 {
		t.Errorf("line rows: %08x %08x %08x", buf[0], buf[19], buf[20])
	}
	// Half-transparent amber over opaque amber stays opaque amber.
	if got := over(pixel(amber, 1), amber, 0.5); got != pixel(amber, 1) {
		t.Errorf("over: %08x want %08x", got, pixel(amber, 1))
	}
	// Over nothing, it's just the source.
	if got := over(0, amber, 0.5); math.Abs(float64(got>>24)-128) > 1 {
		t.Errorf("over transparent: alpha %d", got>>24)
	}
	Clear(buf)
	for _, px := range buf {
		if px != 0 {
			t.Fatal("Clear left a pixel")
		}
	}
}
