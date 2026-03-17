package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"usbridge-client/internal/models"
	"usbridge-client/internal/ui"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const (
	appName = "usbridge-client"
	version = "1.0.0"
)

func main() {
	// Парсим аргументы командной строки
	var (
		configFile  = flag.String("config", "", "Путь к файлу конфигурации")
		logLevel    = flag.String("log-level", "info", "Уровень логирования (debug, info, warn, error)")
		showVersion = flag.Bool("version", false, "Показать версию")
	)
	flag.Parse()

	// Показываем версию если запрошено
	if *showVersion {
		fmt.Printf("%s version %s\n", appName, version)
		os.Exit(0)
	}

	// Настраиваем логирование
	setupLogging(*logLevel)

	logrus.Infof("🚀 Запуск %s версии %s", appName, version)

	// Загружаем конфигурацию
	config, err := loadConfig(*configFile)
	if err != nil {
		logrus.Fatalf("Ошибка загрузки конфигурации: %v", err)
	}

	logrus.Infof("📋 Конфигурация загружена:")
	//logrus.Infof("  USB Bridge 2: %s:%d", config.USBHost, config.USBPort)
	logrus.Infof("  NBD порт: %d", config.NBDPort)
	logrus.Infof("  MediaMTX: %s:%d", config.MediaMTXHost, config.MediaMTXWebRTC)

	// Создаем главное окно (локализация будет загружена внутри)
	mainWindow := ui.NewMainWindow(config)

	// Запускаем приложение
	logrus.Info("🎨 Запуск графического интерфейса")
	mainWindow.Show()
}

// setupLogging настраивает логирование
func setupLogging(level string) {
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
		TimestampFormat: "2006-01-02 15:04:05",
	})

	// Создаем директорию для логов если не существует
	logDir := os.Getenv("USBRIDGE_LOG_DIR")
	if logDir == "" {
		if wd, err := os.Getwd(); err == nil {
			logDir = filepath.Join(wd, "logs")
		} else if exePath, err := os.Executable(); err == nil {
			logDir = filepath.Join(filepath.Dir(exePath), "logs")
		} else {
			logDir = filepath.Join(".", "logs")
		}
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logDir = filepath.Join(os.TempDir(), "usbridge-client-logs")
		_ = os.MkdirAll(logDir, 0755)
	}

	logFilePath := filepath.Join(logDir, "app.log")
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		logrus.SetOutput(os.Stdout)
		logrus.Warnf("Не удалось открыть лог-файл: %v", err)
		return
	}
	logrus.SetOutput(io.MultiWriter(os.Stdout, logFile))
}

// loadConfig загружает конфигурацию
func loadConfig(configFile string) (*models.AppConfig, error) {
	// Создаем конфигурацию по умолчанию
	config := models.DefaultConfig()

	// Если указан файл конфигурации, загружаем его
	if configFile != "" {
		if err := loadConfigFromFile(config, configFile); err != nil {
			return nil, fmt.Errorf("ошибка загрузки файла конфигурации: %v", err)
		}
		logrus.Infof("📁 Конфигурация загружена из %s", configFile)
	} else {
		// Пытаемся загрузить из стандартных мест
		configPaths := []string{
			"./config.yaml",
			"./config.yml",
			"./config.json",
			"./config.toml",
			filepath.Join(os.Getenv("HOME"), ".config", appName, "config.yaml"),
			filepath.Join(os.Getenv("HOME"), ".config", appName, "config.yml"),
			"/etc/" + appName + "/config.yaml",
		}

		for _, path := range configPaths {
			if _, err := os.Stat(path); err == nil {
				if err := loadConfigFromFile(config, path); err == nil {
					logrus.Infof("📁 Конфигурация загружена из %s", path)
					break
				}
			}
		}
	}

	return config, nil
}

// loadConfigFromFile загружает конфигурацию из файла
func loadConfigFromFile(config *models.AppConfig, filename string) error {
	viper.SetConfigFile(filename)
	viper.SetConfigType(filepath.Ext(filename)[1:]) // Убираем точку

	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	// Привязываем переменные окружения
	viper.AutomaticEnv()
	viper.SetEnvPrefix("USBRIDGE")

	// Десериализуем в структуру
	if err := viper.Unmarshal(config); err != nil {
		return err
	}

	return nil
}
