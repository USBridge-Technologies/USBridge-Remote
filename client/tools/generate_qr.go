package main

import (
	"fmt"
	"os"

	qrcode "github.com/skip2/go-qrcode"
)

func main() {
	// Check arguments
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run generate_qr.go <host:token> [output.png]")
		fmt.Println("Example: go run generate_qr.go 192.168.88.244:supersecret qr_test.png")
		os.Exit(1)
	}

	data := os.Args[1]
	output := "qr_code.png"
	if len(os.Args) >= 3 {
		output = os.Args[2]
	}

	// Generate the QR code
	err := qrcode.WriteFile(data, qrcode.Medium, 256, output)
	if err != nil {
		fmt.Printf("Error generating QR code: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ QR code saved to file: %s\n", output)
	fmt.Printf("Data: %s\n", data)
}
