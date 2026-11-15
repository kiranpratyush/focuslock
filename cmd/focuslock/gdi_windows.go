//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// Small GDI helpers for the planning cue, which still draws with Win32.

type rectI RECT

func (r rectI) contains(x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

func drawText(hdc uintptr, s string, r RECT, flags uintptr) {
	p, _ := syscall.UTF16PtrFromString(s)
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags)
}

func mkFont(height int32, weight int) uintptr {
	h, _, _ := pCreateFontW.Call(
		uintptr(height), 0, 0, 0, uintptr(weight),
		0, 0, 0, 1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE_QUALITY*/, 0,
		uintptr(unsafe.Pointer(u16("Segoe UI"))),
	)
	return h
}

func brush(r, g, b byte) uintptr {
	h, _, _ := pCreateSolidBrush.Call(rgb(r, g, b))
	return h
}
