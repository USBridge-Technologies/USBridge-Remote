package main

import (
	"log"
	"os"

	"usbridge_agent/internal/app"
)

func main() {
	setupLogging()

	instance, err := app.New()
	if err != nil {
		log.Fatalf("create app: %v", err)
	}
	if err := instance.Run(); err != nil {
		log.Fatalf("run app: %v", err)
	}
}

func setupLogging() {
	logFilePath := appLogPath()
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.SetOutput(os.Stdout)
		log.Printf("logging fallback to stdout: %v", err)
		return
	}

	output, err := setupPlatformLogOutput(logFile)
	if err != nil {
		log.SetOutput(logFile)
		log.Printf("logging fallback to file only: %v", err)
		return
	}

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.SetOutput(output)
	log.Printf("logging initialized: %s", logFilePath)
}
