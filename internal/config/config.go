package config

import (
	"os"
	"time"
)

// Config holds all configuration
type Config struct {
	Server   ServerConfig
	DB       DBConfig
	JWT      JWTConfig
	Worker   WorkerConfig
	Upload   UploadConfig
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Port         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// DBConfig holds database connection settings
type DBConfig struct {
	DSN string
}

// JWTConfig holds JWT settings
type JWTConfig struct {
	Secret       string
	Expiry       time.Duration
	RefreshExp   time.Duration
}

// WorkerConfig holds worker-related settings
type WorkerConfig struct {
	Secret      string
	OfflineThresh time.Duration // default 30s
}

// UploadConfig holds file upload settings
type UploadConfig struct {
	Dir string
}

// Load reads configuration from environment variables
func Load() *Config {
	return &Config{
		Server: ServerConfig{
			Port:         getEnv("PORT", "8099"),
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
		DB: DBConfig{
			// DSN: getEnv("DB", "root:password@tcp(localhost:3306)/auto_task?parseTime=true&charset=utf8mb4"),
			DSN: getEnv("DB", "root:root@tcp(localhost:3306)/auto_task?parseTime=true&charset=utf8mb4"),
		},
		JWT: JWTConfig{
			Secret:     getEnv("JWT_SECRET", "change-me-in-production"),
			Expiry:     24 * time.Hour,
			RefreshExp: 7 * 24 * time.Hour,
		},
		Worker: WorkerConfig{
			Secret:       getEnv("WORKER_SECRET", ""),
			OfflineThresh: 30 * time.Second,
		},
		Upload: UploadConfig{
			Dir: getEnv("UPLOAD_DIR", "./uploads"),
		},
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
