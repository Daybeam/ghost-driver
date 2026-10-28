//go:build !windows

package browser

import "fmt"

func winMove(x, y int) error {
	return fmt.Errorf("winMove not supported on this platform")
}

func winMouseDown() error {
	return fmt.Errorf("winMouseDown not supported on this platform")
}

func winMouseUp() error {
	return fmt.Errorf("winMouseUp not supported on this platform")
}

func winType(text string) error {
	return fmt.Errorf("winType not supported on this platform")
}

func winScroll(dy int) error {
	return fmt.Errorf("winScroll not supported on this platform")
}
