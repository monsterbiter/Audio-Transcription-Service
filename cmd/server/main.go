package main

import (
	"audio-transcription-service/internal/config"
	"audio-transcription-service/internal/database"
	"audio-transcription-service/internal/handlers"
	"audio-transcription-service/internal/services"
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg := config.Load()
	log.Printf("Starting Audio Transcription Service on port %s", cfg.Server.Port)

	// Initialize database
	if err := database.Initialize(&cfg.Database); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Create upload directory if not exists
	if err := os.MkdirAll(cfg.Storage.UploadDir, 0755); err != nil {
		log.Fatalf("Failed to create upload directory: %v", err)
	}

	// Initialize services
	storageService := services.NewStorageService(cfg.Storage.UploadDir)
	transcriptionService := services.NewTranscriptionService(
		cfg.IFlytek.AppID,
		cfg.IFlytek.APIKey,
		cfg.IFlytek.APISecret,
		cfg.IFlytek.WSURL,
	)
	summarizationService := services.NewSummarizationService(
		cfg.LLM.BaseURL,
		cfg.LLM.APIKey,
		cfg.LLM.Model,
	)
	processorService := services.NewProcessorService(
		3, // 3 concurrent workers
		transcriptionService,
		summarizationService,
	)

	// Start async task processor
	processorService.Start()
	defer processorService.Stop()

	// Initialize handlers
	recordingHandler := handlers.NewRecordingHandler(storageService, processorService)
	taskHandler := handlers.NewTaskHandler(processorService)

	// Setup router
	router := gin.Default()

	// Enable CORS
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Serve static files
	router.Static("/web", "./web")
	router.Static("/uploads", "./uploads") // Serve audio files for preview
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/web/index.html")
	})

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// API routes
	v1 := router.Group("/v1")
	{
		// Recording endpoints
		v1.POST("/recordings", recordingHandler.UploadRecording)
		v1.GET("/recordings", recordingHandler.GetRecordings)
		v1.GET("/recordings/:id", recordingHandler.GetRecording)
		v1.DELETE("/recordings/:id", recordingHandler.DeleteRecording)

		// Task endpoints
		v1.GET("/tasks/:id", taskHandler.GetTask)
		v1.POST("/tasks/:id/retry", taskHandler.RetryTask)
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:    ":" + cfg.Server.Port,
		Handler: router,
	}

	// Start server in a goroutine
	go func() {
		log.Printf("Server listening on http://localhost:%s", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}
