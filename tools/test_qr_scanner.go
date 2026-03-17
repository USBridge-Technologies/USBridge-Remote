package main

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
		fmt.Println("Использование: go run test_qr_scanner.go <qr_image.png>")
		fmt.Println("Пример: go run test_qr_scanner.go /tmp/test_qr.png")
		os.Exit(1)
	}

	imagePath := os.Args[1]

	// Открываем файл
	file, err := os.Open(imagePath)
	if err != nil {
		fmt.Printf("Ошибка открытия файла: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	// Декодируем изображение
	img, _, err := image.Decode(file)
	if err != nil {
		fmt.Printf("Ошибка декодирования изображения: %v\n", err)
		os.Exit(1)
	}

	// Конвертируем в формат для gozxing
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		fmt.Printf("Ошибка создания bitmap: %v\n", err)
		os.Exit(1)
	}

	// Декодируем QR-код
	qrReader := qrcode.NewQRCodeReader()
	result, err := qrReader.Decode(bmp, nil)
	if err != nil {
		fmt.Printf("Ошибка декодирования QR-кода: %v\n", err)
		os.Exit(1)
	}

	// Выводим результат
	qrText := result.GetText()
	fmt.Printf("✓ QR-код успешно отсканирован!\n")
	fmt.Printf("Данные: %s\n", qrText)
}
