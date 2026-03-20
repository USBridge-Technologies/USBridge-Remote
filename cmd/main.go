package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"usbridge-client/internal/gui"
	"usbridge-client/internal/models"

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
		configFile  = flag.String("config", "", "Path to the configuration file")
		logLevel    = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		showVersion = flag.Bool("version", false, "Show version")
	)
	flag.Parse()

	// Показываем версию если запрошено
	if *showVersion {
		fmt.Printf("%s version %s\n", appName, version)
		os.Exit(0)
	}

	// Настраиваем логирование
	setupLogging(*logLevel)

	logrus.Infof("🚀 Starting %s version %s", appName, version)

	// Загружаем конфигурацию
	config, err := loadConfig(*configFile)
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}

	logrus.Infof("📋 Configuration loaded:")
	logrus.Infof("  NBD port: %d", config.NBDPort)

	// Создаем главное окно (локализация будет загружена внутри)
	mainWindow := gui.NewMainWindow(config)

	// Запускаем приложение
	logrus.Info("🎨 Starting GUI")
	mainWindow.Show()
}

// setupLogging настраивает логирование
func setupLogging(level string) {
	// Устанавливаем уровень логирования
	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		logrus.Warnf("Invalid log level %s, using info", level)
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
		logrus.Warnf("Failed to open log file: %v", err)
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
			return nil, fmt.Errorf("failed to load configuration file: %v", err)
		}
		logrus.Infof("📁 Configuration loaded from %s", configFile)
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
					logrus.Infof("📁 Configuration loaded from %s", path)
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
