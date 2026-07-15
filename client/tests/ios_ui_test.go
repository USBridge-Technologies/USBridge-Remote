package tests

import (
	"testing"
	"usbridge-client/internal/gui"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2/test"
)

func TestIOSMainWindowInitialization(t *testing.T) {
	// Инициализируем тестовое Fyne приложение (headless, без реального окна)
	testApp := test.NewApp()
	defer testApp.Quit()

	// Создаем базовый конфиг
	cfg := models.DefaultConfig()

	t.Log("Starting MainWindow initialization for iOS test...")

	// Попытка инициализировать главное окно
	// Если здесь есть Go паника (например из-за фокуса или сервисов), тест упадет
	mainWindow := gui.NewMainWindow(cfg)

	if mainWindow == nil {
		t.Fatal("MainWindow was not initialized (nil returned)")
	}

	t.Log("MainWindow successfully initialized without panics.")
}
