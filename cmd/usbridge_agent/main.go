package main

import (
	"log"

	"usbridge_agent/internal/app"
)

func main() {
	instance, err := app.New()
	if err != nil {
		log.Fatalf("create app: %v", err)
	}
	if err := instance.Run(); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
