//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// Reads the active tab's URL from the browser's address bar using
// UI Automation (the accessibility API screen readers use).
// No browser extension, no admin rights.

var (
	ole32    = syscall.NewLazyDLL("ole32.dll")
	oleaut32 = syscall.NewLazyDLL("oleaut32.dll")

	pCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")
	pSysAllocString   = oleaut32.NewProc("SysAllocString")
	pVariantClear     = oleaut32.NewProc("VariantClear")
)

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	clsidCUIAutomation = GUID{0xFF48DBA4, 0x60EF, 0x4201, [8]byte{0xAA, 0x87, 0x54, 0x10, 0x3E, 0xEF, 0x59, 0x4E}}
	iidIUIAutomation   = GUID{0x30CBE57D, 0xD9D0, 0x452A, [8]byte{0xAB, 0x13, 0x7A, 0xC5, 0xAC, 0x48, 0x25, 0xEE}}
)

const (
	uiaControlTypeProperty      = 30003
	uiaNameProperty             = 30005
	uiaHasKeyboardFocusProperty = 30008
	uiaValueValueProperty       = 30045
	uiaEditControlType          = 50004
	treeScopeDescendants        = 4

	vtI4   = 3
	vtBSTR = 8
	vtBool = 11

	// IUIAutomation vtable slots
	autoElementFromHandle       = 6
	autoCreatePropertyCondition = 23
	autoCreateAndCondition      = 25
	// IUIAutomationElement vtable slots
	elemFindFirst               = 5
	elemGetCurrentPropertyValue = 10
	// IUnknown
	comRelease = 2
)

// comObj is any COM interface pointer: its first field is the vtable.
type comObj struct{ vtbl *[64]uintptr }

func (o *comObj) call(slot int, args ...uintptr) uintptr {
	all := append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)
	r, _, _ := syscall.SyscallN(o.vtbl[slot], all...)
	return r
}

func (o *comObj) release() {
	if o != nil {
		o.call(comRelease)
	}
}

// VARIANT (24 bytes on 64-bit). Passed by pointer on x64, as the ABI requires.
type variant struct {
	VT  uint16
	_   [3]uint16
	Ptr unsafe.Pointer
	_   uintptr
}

func (v *variant) clear() { pVariantClear.Call(uintptr(unsafe.Pointer(v))) }

func variantI4(n int32) variant {
	v := variant{VT: vtI4}
	*(*int32)(unsafe.Pointer(&v.Ptr)) = n
	return v
}

func variantBSTR(s string) variant {
	p, _, _ := pSysAllocString.Call(uintptr(unsafe.Pointer(u16(s))))
	v := variant{VT: vtBSTR}
	*(*uintptr)(unsafe.Pointer(&v.Ptr)) = p
	return v
}

func (v *variant) str() string {
	if v.VT != vtBSTR || v.Ptr == nil {
		return ""
	}
	n := *(*uint32)(unsafe.Add(v.Ptr, -4)) / 2 // BSTR byte length prefix
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(v.Ptr), n))
}

func (v *variant) boolean() bool {
	return v.VT == vtBool && *(*int16)(unsafe.Pointer(&v.Ptr)) != 0
}

type urlReader struct {
	auto     *comObj
	condName *comObj // Edit control named like an address bar
	condEdit *comObj // any Edit control (fallback, e.g. Firefox)
	bars     map[uintptr]*comObj
	lastURL  map[uintptr]string
}

// Chrome, Edge, Brave and Opera name their address bar this. Other browsers
// fall back to the first Edit control in the window, which is the address bar.
const addressBarName = "Address and search bar"

// newURLReader must run on the UI thread. Returns nil if UIA is unavailable.
func newURLReader() *urlReader {
	pCoInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED
	r := &urlReader{bars: map[uintptr]*comObj{}, lastURL: map[uintptr]string{}}
	hr, _, _ := pCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, 1, /*CLSCTX_INPROC_SERVER*/
		uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&r.auto)))
	if hr != 0 || r.auto == nil {
		logf("UI Automation unavailable (hr=0x%x); using titles only", hr)
		return nil
	}
	edit := variantI4(uiaEditControlType)
	r.auto.call(autoCreatePropertyCondition, uiaControlTypeProperty,
		uintptr(unsafe.Pointer(&edit)), uintptr(unsafe.Pointer(&r.condEdit)))

	// condName = Edit AND name == addressBarName
	nameV := variantBSTR(addressBarName)
	var nameCond *comObj
	r.auto.call(autoCreatePropertyCondition, uiaNameProperty,
		uintptr(unsafe.Pointer(&nameV)), uintptr(unsafe.Pointer(&nameCond)))
	nameV.clear()
	if r.condEdit != nil && nameCond != nil {
		r.auto.call(autoCreateAndCondition, uintptr(unsafe.Pointer(r.condEdit)),
			uintptr(unsafe.Pointer(nameCond)), uintptr(unsafe.Pointer(&r.condName)))
	}
	nameCond.release()
	if r.condEdit == nil {
		logf("UI Automation: could not build conditions; using titles only")
		return nil
	}
	return r
}

func (r *urlReader) addressBar(hwnd uintptr) *comObj {
	if el, ok := r.bars[hwnd]; ok {
		return el
	}
	var root *comObj
	if r.auto.call(autoElementFromHandle, hwnd, uintptr(unsafe.Pointer(&root))) != 0 || root == nil {
		return nil
	}
	defer root.release()
	var el *comObj
	if r.condName != nil {
		root.call(elemFindFirst, treeScopeDescendants, uintptr(unsafe.Pointer(r.condName)), uintptr(unsafe.Pointer(&el)))
	}
	if el == nil {
		root.call(elemFindFirst, treeScopeDescendants, uintptr(unsafe.Pointer(r.condEdit)), uintptr(unsafe.Pointer(&el)))
	}
	if el == nil {
		return nil
	}
	if len(r.bars) > 50 {
		r.reset()
	}
	r.bars[hwnd] = el
	return el
}

func (r *urlReader) reset() {
	for _, el := range r.bars {
		el.release()
	}
	r.bars = map[uintptr]*comObj{}
	r.lastURL = map[uintptr]string{}
}

func (r *urlReader) forget(hwnd uintptr) {
	r.bars[hwnd].release()
	delete(r.bars, hwnd)
	delete(r.lastURL, hwnd)
}

// URL returns the current address bar text for a browser window, or "".
// While you're typing in the address bar it returns the last real URL,
// so half-typed text never gets classified.
func (r *urlReader) URL(hwnd uintptr) string {
	if r == nil {
		return ""
	}
	el := r.addressBar(hwnd)
	if el == nil {
		return ""
	}
	var focus variant
	if el.call(elemGetCurrentPropertyValue, uiaHasKeyboardFocusProperty, uintptr(unsafe.Pointer(&focus))) != 0 {
		r.forget(hwnd) // window closed or tree rebuilt; find it again next tick
		return ""
	}
	typing := focus.boolean()
	focus.clear()
	if typing {
		return r.lastURL[hwnd]
	}
	var val variant
	if el.call(elemGetCurrentPropertyValue, uiaValueValueProperty, uintptr(unsafe.Pointer(&val))) != 0 {
		r.forget(hwnd)
		return ""
	}
	u := val.str()
	val.clear()
	r.lastURL[hwnd] = u
	return u
}
