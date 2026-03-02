package main

import (
	"flag"
	"log"
	"os"

	"github.com/marcoantonios1/costguard/internal/app"
	"github.com/marcoantonios1/costguard/internal/config"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "config.json", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	a, err := app.New(cfg)
	if err != nil {
		log.Fatalf("init app: %v", err)
	}

	if err := a.Run(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}
