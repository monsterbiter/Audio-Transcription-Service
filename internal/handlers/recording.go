package handlers

import (
	"audio-transcription-service/internal/database"
	"audio-transcription-service/internal/models"
	"audio-transcription-service/internal/services"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RecordingHandler handles recording-related requests
type RecordingHandler struct {
	storageService   *services.StorageService
	processorService *services.ProcessorService
}

// NewRecordingHandler creates a new recording handler
func NewRecordingHandler(storageService *services.StorageService, processorService *services.ProcessorService) *RecordingHandler {
	return &RecordingHandler{
		storageService:   storageService,
		processorService: processorService,
	}
}

// UploadRecording handles POST /v1/recordings
func (h *RecordingHandler) UploadRecording(c *gin.Context) {
	// Get uploaded file
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "file is required",
		})
		return
	}

	// Validate file
	if err := h.storageService.ValidateFile(fileHeader); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	// Open uploaded file
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to read file",
		})
		return
	}
	defer file.Close()

	// Save file to disk
	filePath, err := h.storageService.SaveFile(file, fileHeader)
	if err != nil {
		log.Printf("Failed to save file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to save file",
		})
		return
	}

	// Create recording record
	recording := &models.Recording{
		ID:       uuid.New().String(),
		Filename: fileHeader.Filename,
		FilePath: filePath,
		FileSize: fileHeader.Size,
		Format:   strings.TrimPrefix(filepath.Ext(fileHeader.Filename), "."),
	}

	if err := database.DB.Create(recording).Error; err != nil {
		// Rollback: delete file if database insert fails
		h.storageService.DeleteFile(filePath)
		log.Printf("Failed to create recording record: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to create recording record",
		})
		return
	}

	// Create task record
	task := &models.Task{
		ID:           uuid.New().String(),
		RecordingID:  recording.ID,
		Status:       models.TaskStatusPending,
		CurrentStage: "waiting",
	}

	if err := database.DB.Create(task).Error; err != nil {
		log.Printf("Failed to create task record: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to create task",
		})
		return
	}

	// Enqueue task for async processing
	h.processorService.EnqueueTask(task.ID)

	log.Printf("Recording uploaded: id=%s, task_id=%s, filename=%s", recording.ID, task.ID, recording.Filename)

	// Return response immediately
	c.JSON(http.StatusOK, gin.H{
		"recording_id": recording.ID,
		"task_id":      task.ID,
		"status":       task.Status,
	})
}

// GetRecordings handles GET /v1/recordings
func (h *RecordingHandler) GetRecordings(c *gin.Context) {
	// Parse pagination parameters
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}

	offset := (page - 1) * pageSize

	// Query recordings with latest task
	var recordings []models.Recording
	err := database.DB.
		Preload("Task").
		Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&recordings).Error

	if err != nil {
		log.Printf("Failed to query recordings: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to query recordings",
		})
		return
	}

	// Get total count
	var total int64
	database.DB.Model(&models.Recording{}).Count(&total)

	// Get task status for each recording
	type RecordingWithTask struct {
		models.Recording
		TaskStatus string `json:"task_status"`
		TaskID     string `json:"task_id"`
	}

	var result []RecordingWithTask
	for _, rec := range recordings {
		var task models.Task
		database.DB.Where("recording_id = ?", rec.ID).Order("created_at DESC").First(&task)

		result = append(result, RecordingWithTask{
			Recording:  rec,
			TaskStatus: string(task.Status),
			TaskID:     task.ID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"recordings": result,
		"total":      total,
		"page":       page,
		"page_size":  pageSize,
	})
}

// GetRecording handles GET /v1/recordings/:id
func (h *RecordingHandler) GetRecording(c *gin.Context) {
	id := c.Param("id")

	var recording models.Recording
	if err := database.DB.First(&recording, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "recording not found",
		})
		return
	}

	// Get latest task for this recording
	var task models.Task
	database.DB.Where("recording_id = ?", id).Order("created_at DESC").First(&task)

	c.JSON(http.StatusOK, gin.H{
		"recording": recording,
		"task":      task,
	})
}

// DeleteRecording handles DELETE /v1/recordings/:id
func (h *RecordingHandler) DeleteRecording(c *gin.Context) {
	id := c.Param("id")

	// Find recording
	var recording models.Recording
	if err := database.DB.First(&recording, "id = ?", id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "recording not found",
		})
		return
	}

	// Delete file from disk
	if err := h.storageService.DeleteFile(recording.FilePath); err != nil {
		log.Printf("Failed to delete file: %v", err)
	}

	// Delete recording (cascade delete tasks)
	if err := database.DB.Delete(&recording).Error; err != nil {
		log.Printf("Failed to delete recording: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to delete recording",
		})
		return
	}

	log.Printf("Recording deleted: id=%s", id)

	c.JSON(http.StatusOK, gin.H{
		"message": "recording deleted successfully",
	})
}
