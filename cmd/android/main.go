// +build android

package main

import (
	"flag"
	"fmt"
	"os"

	"usbridge-client/internal/gui"
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"

	"github.com/sirupsen/logrus"
)

const (
	appName = "usbridge-client"
	version = "1.0.0-android"
)

func main() {
	// Парсим аргументы командной строки
	var (
		logLevel    = flag.String("log-level", "info", "Уровень логирования (debug, info, warn, error)")
		showVersion = flag.Bool("version", false, "Показать версию")
	)
	flag.Parse()

	// Показываем версию если запрошено
	if *showVersion {
		fmt.Printf("%s version %s\n", appName, version)
		os.Exit(0)
	}

	// Настраиваем логирование для Android
	setupAndroidLogging(*logLevel)

	logrus.Infof("🚀 Запуск %s версии %s", appName, version)

	// Создаем конфигурацию по умолчанию для Android
	config := models.DefaultConfig()

	// Wire Android IME keyboard height into overlay popup layout so popups
	// (e.g. connection editor dialog) reposition above the on-screen keyboard.
	view.KeyboardHeight = graphics.GetLastIMEH

	// Создаем главное окно
	mainWindow := gui.NewMainWindow(config)

	// Настраиваем callback переключения сети
	platform.SetOnNetworkChangedCallback(func() {
		logrus.Info("🌐 Network change notification received, refreshing services...")
		mainWindow.RefreshNetworkState()
	})
	// Mark app as ready only after GUI has started to prevent early JNI callbacks from crashing
	mainWindow.SetOnReadyCallback(platform.SetAppReady)

	logrus.Infof("📋 Конфигурация:")
	logrus.Infof("  NBD порт: %d", config.NBDPort)
	logrus.Infof("  Видео UDP bind: %s:%d", config.VideoBindHost, config.VideoUDPPort)

	// Проверяем доступ к SD карте
	checkStorageAccess()

	// Запускаем приложение
	logrus.Info("🎨 Запуск графического интерфейса")
	mainWindow.Show()
}

// checkStorageAccess проверяет доступ к внешнему хранилищу
func checkStorageAccess() {
	testPaths := []string{
		"/storage/emulated/0",
		"/sdcard",
		os.Getenv("EXTERNAL_STORAGE"),
	}

	for _, path := range testPaths {
		if path == "" {
			continue
		}

		if _, err := os.Stat(path); err == nil {
			logrus.Infof("✅ Доступ к хранилищу: %s", path)
			return
		} else {
			logrus.Warnf("⚠️ Нет доступа к %s: %v", path, err)
		}
	}

	logrus.Warn("⚠️ Внимание: Нет доступа к внешнему хранилищу")
	logrus.Warn("⚠️ Дайте разрешение в Настройки → Приложения → USBridge Client → Разрешения → Файлы и медиа")
}

// setupAndroidLogging настраивает логирование для Android
func setupAndroidLogging(level string) {
	// Устанавливаем уровень логирования
	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		logrus.Warnf("Неверный уровень логирования %s, используем info", level)
		logLevel = logrus.InfoLevel
	}
	logrus.SetLevel(logLevel)

	// Настраиваем формат логов
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "15:04:05",
	})

	// Перенаправляем logrus в Android logcat (stdout на Android может не показываться в adb logcat)
	platform.SetupLogrusForAndroid()
	logrus.Info("📝 Логи записываются в Android logcat (adb logcat -s USBridge)")
}
