package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	Port    string
	BaseURL string
	DBPath  string
}

func Load() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:" + port
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	return Config{
		Port:    port,
		BaseURL: baseURL,
		DBPath:  filepath.Join(dataDir, "links.db"),
	}
}
