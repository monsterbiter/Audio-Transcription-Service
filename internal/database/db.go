package database

import (
	"audio-transcription-service/internal/config"
	"fmt"
	"log"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// Initialize initializes database connection
func Initialize(cfg *config.DatabaseConfig) error {
	dsn := cfg.GetDSN()

	var err error
	DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})

	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	log.Println("Database connection established")

	// Auto migrate models (optional, since we use init.sql)
	// err = DB.AutoMigrate(&models.Recording{}, &models.Task{})
	// if err != nil {
	// 	return fmt.Errorf("failed to migrate database: %w", err)
	// }

	return nil
}

// GetDB returns database instance
func GetDB() *gorm.DB {
	return DB
}
