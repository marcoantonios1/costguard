package main

import (
	"flag"
	"log"
	"os"

	"github.com/joho/godotenv"
	"github.com/marcoantonios1/costguard/internal/app"
	"github.com/marcoantonios1/costguard/internal/config"
	"github.com/marcoantonios1/costguard/internal/logging"
)

func main() {
	_ = godotenv.Load(".env")
	var configPath string
	flag.StringVar(&configPath, "config", "config.json", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	lg := logging.New(cfg.Logging)
	a, err := app.New(cfg, lg)
	if err != nil {
		lg.Error("failed_to_create_app", map[string]any{"error": err.Error()})
		os.Exit(1)
	}

	if err := a.Run(); err != nil {
		lg.Error("app_error", map[string]any{"error": err.Error()})
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
