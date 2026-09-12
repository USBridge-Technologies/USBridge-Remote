package main

import (
	"fmt"
	"fyne.io/fyne/v2/widget"
)

func main() {
	btn := widget.NewButton("Test", func() {})
	fmt.Printf("Text: %s, Visible: %v, Disabled: %v\n", btn.Text, btn.Visible(), btn.Disabled())
}
