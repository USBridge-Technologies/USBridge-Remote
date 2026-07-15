package main

import (
	"fmt"
	"os"

	qrcode "github.com/skip2/go-qrcode"
)

func main() {
	// Проверяем аргументы
	if len(os.Args) < 2 {
		fmt.Println("Использование: go run generate_qr.go <host:token> [output.png]")
		fmt.Println("Пример: go run generate_qr.go 192.168.88.244:supersecret qr_test.png")
		os.Exit(1)
	}

	data := os.Args[1]
	output := "qr_code.png"
	if len(os.Args) >= 3 {
		output = os.Args[2]
	}

	// Генерируем QR-код
	err := qrcode.WriteFile(data, qrcode.Medium, 256, output)
	if err != nil {
		fmt.Printf("Ошибка генерации QR-кода: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ QR-код сохранен в файл: %s\n", output)
	fmt.Printf("Данные: %s\n", data)
}
