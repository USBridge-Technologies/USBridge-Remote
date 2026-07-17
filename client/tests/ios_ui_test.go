package tests

import (
	"testing"
	"usbridge-client/internal/gui"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2/test"
)

func TestIOSMainWindowInitialization(t *testing.T) {
	// Initialize the test Fyne app (headless, no real window)
	testApp := test.NewApp()
	defer testApp.Quit()

	// Create a base config
	cfg := models.DefaultConfig()

	t.Log("Starting MainWindow initialization for iOS test...")

	// Attempt to initialize the main window
	// If there's a Go panic here (e.g. from focus handling or services), the test will fail
	mainWindow := gui.NewMainWindow(cfg)

	if mainWindow == nil {
		t.Fatal("MainWindow was not initialized (nil returned)")
	}

	t.Log("MainWindow successfully initialized without panics.")
}
