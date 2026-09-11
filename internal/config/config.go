package config

import (
	"fmt"
	"os"
)

// Config holds application configuration
type Config struct {
	Database DatabaseConfig
	Server   ServerConfig
	LLM      LLMConfig
	Storage  StorageConfig
	IFlytek  IFlytekConfig
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port string
}

// LLMConfig holds LLM API configuration
type LLMConfig struct {
	BaseURL string
	APIKey  string
	Model   string
}

// StorageConfig holds file storage configuration
type StorageConfig struct {
	UploadDir string
}

// IFlytekConfig holds iFlytek ASR configuration
type IFlytekConfig struct {
	AppID     string
	APIKey    string
	APISecret string
	WSURL     string
}

// Load loads configuration from environment or defaults
func Load() *Config {
	return &Config{
		Database: DatabaseConfig{
			Host:     getEnv("DB_HOST", "localhost"),
			Port:     getEnv("DB_PORT", "3306"),
			User:     getEnv("DB_USER", "root"),
			Password: getEnv("DB_PASSWORD", "940588775jia"),
			DBName:   getEnv("DB_NAME", "audio_transcription"),
		},
		Server: ServerConfig{
			Port: getEnv("SERVER_PORT", "8080"),
		},
		LLM: LLMConfig{
			BaseURL: getEnv("LLM_BASE_URL", "https://api.agnes-ai.cn/v1"),
			APIKey:  getEnv("LLM_API_KEY", "sk-6zj7FGytFFv1TzeoBpOwrWHZiTru96rhMFoSnNJYBybGfI1J"),
			Model:   getEnv("LLM_MODEL", "agnes-25-flash"),
		},
		Storage: StorageConfig{
			UploadDir: getEnv("UPLOAD_DIR", "./uploads"),
		},
		IFlytek: IFlytekConfig{
			AppID:     getEnv("IFLYTEK_APP_ID", "bebd7457"),
			APIKey:    getEnv("IFLYTEK_API_KEY", "2d4238e31a345e50d120eecb8c830b9e"),
			APISecret: getEnv("IFLYTEK_API_SECRET", "OGI5OGY3YzE2ZWE3OTZhNWQxMWExOTc1"),
			WSURL:     getEnv("IFLYTEK_WS_URL", "wss://iat-api.xfyun.cn/v2/iat"),
		},
	}
}

// GetDSN returns database connection string
func (c *DatabaseConfig) GetDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		c.User, c.Password, c.Host, c.Port, c.DBName)
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
