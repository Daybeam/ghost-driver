//go:build windows

package browser

import (
	"syscall"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procMouseEvent   = user32.NewProc("mouse_event")
	procKeybdEvent   = user32.NewProc("keybd_event")
	procVkKeyScanW   = user32.NewProc("VkKeyScanW")
)

const (
	mouseEventfLeftDown = 0x0002
	mouseEventfLeftUp   = 0x0004
	mouseEventfWheel    = 0x0800
	keyEventfUnicode    = 0x0004
	keyEventfKeyUp      = 0x0002
	vkShift             = 0x10
)

func winMove(x, y int) error {
	r, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	if r == 0 {
		return syscall.GetLastError()
	}
	return nil
}

func winMouseDown() error {
	procMouseEvent.Call(mouseEventfLeftDown, 0, 0, 0, 0)
	return nil
}

func winMouseUp() error {
	procMouseEvent.Call(mouseEventfLeftUp, 0, 0, 0, 0)
	return nil
}

func winScroll(dy int) error {
	procMouseEvent.Call(mouseEventfWheel, 0, 0, uintptr(-dy), 0)
	return nil
}

func winType(text string) error {
	for _, ch := range text {
		if ch > 127 {
			procKeybdEvent.Call(0, uintptr(ch), keyEventfUnicode, 0)
			procKeybdEvent.Call(0, uintptr(ch), keyEventfUnicode|keyEventfKeyUp, 0)
		} else {
			r, _, _ := procVkKeyScanW.Call(uintptr(ch))
			vk := uintptr(r & 0xff)
			shift := r&0x100 != 0
			if shift {
				procKeybdEvent.Call(vkShift, 0, 0, 0)
			}
			procKeybdEvent.Call(vk, 0, 0, 0)
			procKeybdEvent.Call(vk, 0, keyEventfKeyUp, 0)
			if shift {
				procKeybdEvent.Call(vkShift, 0, keyEventfKeyUp, 0)
			}
		}
	}
	return nil
}
