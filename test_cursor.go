package main

import (
	"fmt"
	"fyne.io/fyne/v2/driver/desktop"
)

func main() {
	var c desktop.Cursor = desktop.HiddenCursor
	fmt.Println(c)
}
