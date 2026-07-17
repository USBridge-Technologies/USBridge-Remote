package test_qr_scanner

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run test_qr_scanner.go <qr_image.png>")
		fmt.Println("Example: go run test_qr_scanner.go /tmp/test_qr.png")
		os.Exit(1)
	}

	imagePath := os.Args[1]

	// Open the file
	file, err := os.Open(imagePath)
	if err != nil {
		fmt.Printf("Error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Decode the image
	img, _, err := image.Decode(file)
	if err != nil {
		fmt.Printf("Error decoding image: %v\n", err)
		os.Exit(1)
	}

	// Convert to the format expected by gozxing
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		fmt.Printf("Error creating bitmap: %v\n", err)
		os.Exit(1)
	}

	// Decode the QR code
	qrReader := qrcode.NewQRCodeReader()
	result, err := qrReader.Decode(bmp, nil)
	if err != nil {
		fmt.Printf("Error decoding QR code: %v\n", err)
		os.Exit(1)
	}

	// Print the result
	qrText := result.GetText()
	fmt.Printf("✓ QR code successfully scanned!\n")
	fmt.Printf("Data: %s\n", qrText)
}
