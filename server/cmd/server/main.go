package main

import (
	"log"

	"relay-api/server/internal/app"
)

func main() {
	cfg := app.LoadConfigFromEnv()
	api, err := app.New(cfg)
	if err != nil {
		log.Fatalf("init server: %v", err)
	}
	if err := api.Run(); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
