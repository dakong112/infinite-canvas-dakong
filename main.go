package main

import (
	"encoding/base64"
	"log"
	"strings"

	"github.com/tigerowo/infinite-canvas/config"
	"github.com/tigerowo/infinite-canvas/handler"
	"github.com/tigerowo/infinite-canvas/model"
	"github.com/tigerowo/infinite-canvas/router"
	"github.com/tigerowo/infinite-canvas/service"
)

func main() {
	if err := config.Load(); err != nil {
		log.Fatal(err)
	}
	if key := strings.TrimSpace(config.Cfg.DataEncryptionKey); key != "" {
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			log.Fatalf("invalid DATA_ENCRYPTION_KEY: %v", err)
		}
		if err := model.SetSecretKey(decoded); err != nil {
			log.Fatal(err)
		}
	}
	if err := service.EnsureDefaultAdmin(); err != nil {
		log.Fatal(err)
	}
	if err := service.EnsureDefaultAgentSkills(); err != nil {
		log.Fatal(err)
	}
	service.StartPromptSyncScheduler()
	service.StartCanvasProjectCleanupScheduler()
	service.StartStorageCapacityScheduler()
	service.StartAILogCleanupScheduler()
	handler.StartVideoTaskPoller()
	log.Fatal(router.New().Run(":" + config.Cfg.Port))
}
