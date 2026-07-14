package input

import (
	"os"
	"sync"
)

const absoluteCoordinateMax = 32767

type Controller struct {
	mu          sync.Mutex
	buttonState uint8
	mouseFile   *os.File
	kbdFile     *os.File
}

func scaleAbsoluteCoordinate(value uint16, span int) int {
	if span <= 1 {
		return 0
	}
	if value > absoluteCoordinateMax {
		value = absoluteCoordinateMax
	}
	return int(value) * (span - 1) / absoluteCoordinateMax
}

func modifierHIDKeys(modifiers uint8) []uint8 {
	out := make([]uint8, 0, 4)
	if modifiers&0x01 != 0 {
		out = append(out, 224)
	}
	if modifiers&0x02 != 0 {
		out = append(out, 225)
	}
	if modifiers&0x04 != 0 {
		out = append(out, 226)
	}
	if modifiers&0x08 != 0 {
		out = append(out, 227)
	}
	return out
}
