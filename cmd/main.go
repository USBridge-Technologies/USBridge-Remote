package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"usbridge-client/internal/gui"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

const (
	appName = "usbridge-client"
	version = "1.0.0"
)

func main() {
	writeStartupTrace("main: entered")
	defer func() {
		if recovered := recover(); recovered != nil {
			writeStartupPanicTrace(recovered)
			panic(recovered)
		}
	}()

	if len(os.Args) > 1 && os.Args[1] == "wg-helper" {
		if err := service.RunWindowsWireGuardHelper(os.Args[2:]); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "wg-helper failed: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	var (
		configFile  = flag.String("config", "", "Path to the configuration file")
		logLevel    = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		showVersion = flag.Bool("version", false, "Show version")
	)
	flag.Parse()
	writeStartupTrace("main: flags parsed config=%q logLevel=%q version=%v", *configFile, *logLevel, *showVersion)

	if *showVersion {
		writeStartupTrace("main: version requested")
		fmt.Printf("%s version %s\n", appName, version)
		os.Exit(0)
	}

	writeStartupTrace("main: setupLogging start")
	setupLogging(*logLevel)
	writeStartupTrace("main: setupLogging done")

	logrus.Infof("Starting %s version %s", appName, version)

	writeStartupTrace("main: loadConfig start")
	config, err := loadConfig(*configFile)
	if err != nil {
		writeStartupTrace("main: loadConfig failed: %v", err)
		logrus.Fatalf("Failed to load configuration: %v", err)
	}
	writeStartupTrace("main: loadConfig done nbd_port=%d window=%dx%d", config.NBDPort, config.WindowWidth, config.WindowHeight)

	logrus.Infof("Configuration loaded")
	logrus.Infof("NBD port: %d", config.NBDPort)

	writeStartupTrace("main: NewMainWindow start")
	gui.SetAppVersion(version)
	mainWindow := gui.NewMainWindow(config)
	writeStartupTrace("main: NewMainWindow done")

	logrus.Info("Starting GUI")
	writeStartupTrace("main: Show start")
	mainWindow.Show()
}

func setupLogging(level string) {
	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		logrus.Warnf("Invalid log level %s, using info", level)
		logLevel = logrus.InfoLevel
	}
	logrus.SetLevel(logLevel)

	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	logFilePath := appLogPath()
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		logrus.SetOutput(os.Stdout)
		logrus.Warnf("Failed to open log file: %v", err)
		return
	}

	output, err := setupPlatformLogOutput(logFile)
	if err != nil {
		logrus.SetOutput(logFile)
		logrus.Warnf("Failed to redirect standard output to log file: %v", err)
		return
	}

	logrus.SetOutput(output)
	logrus.Infof("Logging initialized: %s", logFilePath)
}

func loadConfig(configFile string) (*models.AppConfig, error) {
	config := models.DefaultConfig()

	if configFile != "" {
		if err := loadConfigFromFile(config, configFile); err != nil {
			return nil, fmt.Errorf("failed to load configuration file: %v", err)
		}
		logrus.Infof("Configuration loaded from %s", configFile)
	} else {
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
					logrus.Infof("Configuration loaded from %s", path)
					break
				}
			}
		}
	}

	return config, nil
}

func loadConfigFromFile(config *models.AppConfig, filename string) error {
	viper.SetConfigFile(filename)
	viper.SetConfigType(filepath.Ext(filename)[1:])

	if err := viper.ReadInConfig(); err != nil {
		return err
	}

	viper.AutomaticEnv()
	viper.SetEnvPrefix("USBRIDGE")

	if err := viper.Unmarshal(config); err != nil {
		return err
	}

	return nil
}
