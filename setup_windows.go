//go:build windows

package main

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// The start pop-up: "focuslock start" asks how long, what you're working
// on, and which extra sites and apps this session needs. Enter in the
// duration box or Ctrl+Enter anywhere starts; Esc or closing cancels.

const setupClass = "FocusLockSetup"

type setupWindow struct {
	hwnd                                 uintptr
	duration, task, domains, apps        uintptr // EDIT controls
	fontTitle, fontLabel, fontHint, font uintptr
	brushBg, brushEdit, brushStart       uintptr
	brushCancel                          uintptr
	scale                                func(int32) int32
	start, cancel                        rectI
	ration                               string // "Rationed: Email every 30 min..."
	errText                              string
	result                               SessionSetup
	ok, done                             bool
}

var sw setupWindow

// askSetup shows the pop-up pre-filled with initial. ok is false if cancelled.
func askSetup(initial SessionSetup, cfg Config) (SessionSetup, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pSetProcessDPIAware.Call()

	dpi := int32(96)
	if pGetDpiForSystem.Find() == nil {
		if d, _, _ := pGetDpiForSystem.Call(); d != 0 {
			dpi = int32(d)
		}
	}
	sw = setupWindow{scale: func(v int32) int32 { return v * dpi / 96 }}
	s := sw.scale

	inst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, 32512)
	wc := WNDCLASSEXW{WndProc: syscall.NewCallback(setupProc), Instance: inst, Cursor: cursor, ClassName: u16(setupClass)}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	sw.fontTitle = mkFont(-s(24), 700)
	sw.fontLabel = mkFont(-s(16), 600)
	sw.fontHint = mkFont(-s(14), 400)
	sw.font = mkFont(-s(16), 400)
	sw.brushBg = brush(20, 20, 26)
	sw.brushEdit = brush(40, 40, 48)
	sw.brushStart = brush(250, 204, 21)
	sw.brushCancel = brush(55, 55, 64)
	var names []string
	for _, r := range cfg.Rations {
		names = append(names, fmt.Sprintf("%s for %d min every %d", r.Name, r.ForMinutes, r.EveryMinutes))
	}
	if len(names) > 0 {
		sw.ration = "Rationed (focus.json): " + strings.Join(names, " · ")
	}

	w, h := s(640), s(600)
	sx, _, _ := pGetSystemMetrics.Call(SM_CXSCREEN)
	sy, _, _ := pGetSystemMetrics.Call(SM_CYSCREEN)
	sw.hwnd, _, _ = pCreateWindowExW.Call(WS_EX_TOPMOST,
		uintptr(unsafe.Pointer(u16(setupClass))), uintptr(unsafe.Pointer(u16("FocusLock — start a session"))),
		WS_CAPTION|WS_SYSMENU,
		uintptr((int32(sx)-w)/2), uintptr((int32(sy)-h)/2), uintptr(w), uintptr(h),
		0, 0, inst, 0)

	var rc RECT
	pGetClientRect.Call(sw.hwnd, uintptr(unsafe.Pointer(&rc)))
	x, cw := s(28), rc.Right-s(56)
	edit := func(y, height int32, multi bool, text string) uintptr {
		style := uintptr(WS_CHILD | WS_VISIBLE | WS_BORDER | WS_TABSTOP)
		if multi {
			style |= ES_MULTILINE | ES_AUTOVSCROLL | ES_WANTRETURN | WS_VSCROLL
			text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
		} else {
			style |= ES_AUTOHSCROLL
		}
		e, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16("EDIT"))), uintptr(unsafe.Pointer(u16(text))),
			style, uintptr(x), uintptr(y), uintptr(cw), uintptr(height), sw.hwnd, 0, inst, 0)
		pSendMessageW.Call(e, WM_SETFONT, sw.font, 1)
		return e
	}
	sw.duration = edit(s(104), s(32), false, initial.Duration)
	sw.task = edit(s(186), s(110), true, initial.Task)
	sw.domains = edit(s(354), s(52), true, strings.Join(initial.AllowDomains, ", "))
	sw.apps = edit(s(464), s(32), false, strings.Join(initial.AllowApps, ", "))
	pSendMessageW.Call(sw.duration, WM_SETFONT, sw.font, 1)
	sw.start = rectI{rc.Right - s(28) - s(140), rc.Bottom - s(56), rc.Right - s(28), rc.Bottom - s(18)}
	sw.cancel = rectI{sw.start.Left - s(12) - s(110), sw.start.Top, sw.start.Left - s(12), sw.start.Bottom}

	pShowWindow.Call(sw.hwnd, SW_SHOW)
	pSetForegroundWindow.Call(sw.hwnd)
	focus := sw.task
	if strings.TrimSpace(initial.Task) != "" {
		focus = sw.duration
	}
	pSetFocus.Call(focus)

	edits := []uintptr{sw.duration, sw.task, sw.domains, sw.apps}
	var msg MSG
	for !sw.done {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if msg.Message == WM_KEYDOWN {
			ctrl := keyDown(VK_CONTROL)
			switch {
			case msg.WParam == VK_ESCAPE:
				sw.done = true
				continue
			case msg.WParam == VK_RETURN && (ctrl || msg.Hwnd == sw.duration || msg.Hwnd == sw.apps):
				submitSetup()
				continue
			case msg.WParam == VK_TAB:
				i := indexOf(edits, msg.Hwnd)
				if i >= 0 {
					if keyDown(VK_SHIFT) {
						i += len(edits) - 1
					} else {
						i++
					}
					pSetFocus.Call(edits[i%len(edits)])
					continue
				}
			case msg.WParam == 'A' && ctrl: // select all in the box
				pSendMessageW.Call(msg.Hwnd, EM_SETSEL, 0, ^uintptr(0))
				continue
			}
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
	pDestroyWindow.Call(sw.hwnd)
	return sw.result, sw.ok
}

func submitSetup() {
	dur := strings.TrimSpace(editText(sw.duration))
	if _, err := parseDuration(dur); err != nil {
		sw.errText = "How long? Try 50m, 1h30m, or 45 (minutes)."
		pInvalidateRect.Call(sw.hwnd, 0, 1)
		pSetFocus.Call(sw.duration)
		return
	}
	task := strings.Join(strings.Fields(strings.ReplaceAll(editText(sw.task), "\r\n", " ")), " ")
	sw.result = SessionSetup{
		Duration:     dur,
		Task:         task,
		AllowDomains: parseDomainList(editText(sw.domains)),
		AllowApps:    parseAppList(editText(sw.apps)),
	}
	sw.ok, sw.done = true, true
}

func setupProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_PAINT:
		paintSetup(hwnd)
		return 0
	case WM_CTLCOLOREDIT:
		pSetTextColor.Call(wParam, rgb(244, 244, 245))
		pSetBkColor.Call(wParam, rgb(40, 40, 48))
		return sw.brushEdit
	case WM_LBUTTONDOWN:
		x, y := int32(int16(lParam&0xFFFF)), int32(int16(lParam>>16&0xFFFF))
		switch {
		case sw.start.contains(x, y):
			submitSetup()
		case sw.cancel.contains(x, y):
			sw.done = true
		}
		return 0
	case WM_CLOSE:
		sw.done = true
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

func paintSetup(hwnd uintptr) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc RECT
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	pSetBkMode.Call(hdc, TRANSPARENT)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), sw.brushBg)

	s := sw.scale
	x, r := s(28), rc.Right-s(28)
	text := func(str string, y, height int32, font, color uintptr) {
		pSelectObject.Call(hdc, font)
		pSetTextColor.Call(hdc, color)
		drawText(hdc, str, RECT{x, y, r, y + height}, DT_LEFT|DT_SINGLELINE|DT_VCENTER|DT_NOPREFIX|DT_END_ELLIPSIS)
	}
	label := func(str string, y int32) { text(str, y, s(22), sw.fontLabel, rgb(228, 228, 231)) }
	hint := func(str string, y int32) { text(str, y, s(20), sw.fontHint, rgb(140, 140, 150)) }

	text("Start a focus session", s(20), s(36), sw.fontTitle, rgb(250, 204, 21))
	hint("The judge checks every tab and app against what you write here.", s(54))
	label("How long?", s(80))
	label("What are you working on? More detail helps the judge.", s(146))
	hint("e.g. Fix pagination in the orders API: cursor-based paging, Postgres indexes, Go tests", s(164))
	label("Sites this session needs", s(310))
	hint("Besides focus.json. e.g. portal.azure.com, dev.azure.com/myorg, console.aws.amazon.com", s(330))
	label("Apps this session needs", s(418))
	hint("Exe or app names, comma-separated. e.g. Postman, docker desktop, ssms", s(438))
	if sw.ration != "" {
		hint(sw.ration, s(506))
	}
	if sw.errText != "" {
		text(sw.errText, s(528), s(22), sw.fontHint, rgb(251, 146, 60))
	}

	button := func(b rectI, br uintptr, label string, color uintptr) {
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&b)), br)
		pSelectObject.Call(hdc, sw.fontLabel)
		pSetTextColor.Call(hdc, color)
		drawText(hdc, label, RECT(b), DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	}
	button(sw.start, sw.brushStart, "Start  (Ctrl+Enter)", rgb(24, 24, 27))
	button(sw.cancel, sw.brushCancel, "Cancel", rgb(228, 228, 231))
}

// editText reads an EDIT control's whole text (windowTitle stops at 512).
func editText(h uintptr) string {
	n, _, _ := pGetWindowTextLengthW.Call(h)
	buf := make([]uint16, n+1)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func keyDown(vk uintptr) bool {
	r, _, _ := pGetKeyState.Call(vk)
	return int16(r) < 0
}

func indexOf(xs []uintptr, x uintptr) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
