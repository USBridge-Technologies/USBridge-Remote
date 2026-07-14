package main

import (
	"fmt"
	"image/png"
	"os"
	"github.com/kbinani/screenshot"
)

func main() {
	if screenshot.NumActiveDisplays() == 0 {
		fmt.Println("No active displays")
		return
	}
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	f, _ := os.Create("test_screen.png")
	defer f.Close()
	png.Encode(f, img)
	fmt.Println("Saved to test_screen.png")
}
