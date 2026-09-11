package handlers

import (
	"audio-transcription-service/internal/database"
	"audio-transcription-service/internal/models"
	"audio-transcription-service/internal/services"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// TaskHandler handles task-related requests
type TaskHandler struct {
	processorService *services.ProcessorService
}

// NewTaskHandler creates a new task handler
func NewTaskHandler(processorService *services.ProcessorService) *TaskHandler {
	return &TaskHandler{
		processorService: processorService,
	}
}

// GetTask handles GET /v1/tasks/:id
func (h *TaskHandler) GetTask(c *gin.Context) {
	taskID := c.Param("id")

	var task models.Task
	if err := database.DB.Preload("Recording").First(&task, "id = ?", taskID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "task not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":            task.ID,
		"recording_id":  task.RecordingID,
		"status":        task.Status,
		"current_stage": task.CurrentStage,
		"error_message": task.ErrorMessage,
		"retry_count":   task.RetryCount,
		"created_at":    task.CreatedAt,
		"updated_at":    task.UpdatedAt,
	})
}

// RetryTask handles POST /v1/tasks/:id/retry
func (h *TaskHandler) RetryTask(c *gin.Context) {
	taskID := c.Param("id")

	var task models.Task
	if err := database.DB.First(&task, "id = ?", taskID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "task not found",
		})
		return
	}

	// Check if task can be retried
	if !task.CanRetry() {
		c.JSON(http.StatusConflict, gin.H{
			"error": "only failed tasks can be retried",
		})
		return
	}

	// Reset task to pending state
	task.Status = models.TaskStatusPending
	task.CurrentStage = "retry queued"
	task.ErrorMessage = ""
	task.RetryCount++

	if err := database.DB.Model(&task).Updates(map[string]interface{}{
		"status":        models.TaskStatusPending,
		"current_stage": "retry queued",
		"error_message": "",
		"retry_count":   task.RetryCount,
	}).Error; err != nil {
		log.Printf("Failed to update task for retry: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "failed to retry task",
		})
		return
	}

	// Re-enqueue task
	h.processorService.EnqueueTask(taskID)

	log.Printf("Task %s retried, retry_count=%d", taskID, task.RetryCount)

	c.JSON(http.StatusOK, gin.H{
		"message":     "task retry queued",
		"task_id":     task.ID,
		"status":      task.Status,
		"retry_count": task.RetryCount,
	})
}
