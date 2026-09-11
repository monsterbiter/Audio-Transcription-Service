package services

import (
	"audio-transcription-service/internal/database"
	"audio-transcription-service/internal/models"
	"log"
	"sync"
)

// ProcessorService handles async task processing
type ProcessorService struct {
	taskQueue          chan string
	transcriptionSvc   *TranscriptionService
	summarizationSvc   *SummarizationService
	workerCount        int
	wg                 sync.WaitGroup
	stopChan           chan struct{}
}

// NewProcessorService creates a new processor service
func NewProcessorService(workerCount int, transcriptionSvc *TranscriptionService, summarizationSvc *SummarizationService) *ProcessorService {
	return &ProcessorService{
		taskQueue:        make(chan string, 100),
		transcriptionSvc: transcriptionSvc,
		summarizationSvc: summarizationSvc,
		workerCount:      workerCount,
		stopChan:         make(chan struct{}),
	}
}

// Start starts the worker pool
func (p *ProcessorService) Start() {
	log.Printf("Starting %d workers for task processing", p.workerCount)

	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// Stop stops the processor service
func (p *ProcessorService) Stop() {
	close(p.stopChan)
	close(p.taskQueue)
	p.wg.Wait()
	log.Println("Processor service stopped")
}

// EnqueueTask adds a task to the processing queue
func (p *ProcessorService) EnqueueTask(taskID string) {
	p.taskQueue <- taskID
	log.Printf("Task enqueued: %s", taskID)
}

// worker processes tasks from the queue
func (p *ProcessorService) worker(id int) {
	defer p.wg.Done()

	log.Printf("Worker %d started", id)

	for {
		select {
		case <-p.stopChan:
			log.Printf("Worker %d stopped", id)
			return
		case taskID, ok := <-p.taskQueue:
			if !ok {
				log.Printf("Worker %d: task queue closed", id)
				return
			}

			log.Printf("Worker %d processing task: %s", id, taskID)
			p.processTask(taskID)
		}
	}
}

// processTask processes a single task through the state machine
func (p *ProcessorService) processTask(taskID string) {
	// Load task
	var task models.Task
	if err := database.DB.Preload("Recording").First(&task, "id = ?", taskID).Error; err != nil {
		log.Printf("Failed to load task %s: %v", taskID, err)
		return
	}

	log.Printf("Task %s state machine started: %s -> transcribing", taskID, task.Status)

	// Stage 1: Transcription
	if err := p.updateTaskStatus(&task, models.TaskStatusTranscribing, "starting transcription"); err != nil {
		log.Printf("Failed to update task status: %v", err)
		return
	}

	transcript, err := p.transcriptionSvc.Transcribe(task.Recording.FilePath)
	if err != nil {
		log.Printf("Task %s transcription failed: %v", taskID, err)
		p.failTask(&task, "transcription failed: "+err.Error())
		return
	}

	// Save transcript
	if err := database.DB.Model(&models.Recording{}).Where("id = ?", task.RecordingID).Update("transcript", transcript).Error; err != nil {
		log.Printf("Failed to save transcript: %v", err)
		p.failTask(&task, "failed to save transcript")
		return
	}

	log.Printf("Task %s transcription completed, transcript length: %d", taskID, len(transcript))

	// Stage 2: Summarization
	if err := p.updateTaskStatus(&task, models.TaskStatusSummarizing, "starting summarization"); err != nil {
		log.Printf("Failed to update task status: %v", err)
		return
	}

	summary, err := p.summarizationSvc.Summarize(transcript)
	if err != nil {
		log.Printf("Task %s summarization failed: %v", taskID, err)
		p.failTask(&task, "summarization failed: "+err.Error())
		return
	}

	// Save summary
	if err := database.DB.Model(&models.Recording{}).Where("id = ?", task.RecordingID).Update("summary", summary).Error; err != nil {
		log.Printf("Failed to save summary: %v", err)
		p.failTask(&task, "failed to save summary")
		return
	}

	log.Printf("Task %s summarization completed", taskID)

	// Stage 3: Done
	if err := p.updateTaskStatus(&task, models.TaskStatusDone, "completed"); err != nil {
		log.Printf("Failed to update task status: %v", err)
		return
	}

	log.Printf("Task %s completed successfully", taskID)
}

// updateTaskStatus updates task status and stage
func (p *ProcessorService) updateTaskStatus(task *models.Task, status models.TaskStatus, stage string) error {
	task.Status = status
	task.CurrentStage = stage
	return database.DB.Model(task).Updates(map[string]interface{}{
		"status":        status,
		"current_stage": stage,
	}).Error
}

// failTask marks task as failed
func (p *ProcessorService) failTask(task *models.Task, errorMsg string) {
	task.Status = models.TaskStatusFailed
	task.ErrorMessage = errorMsg
	task.CurrentStage = "failed"

	if err := database.DB.Model(task).Updates(map[string]interface{}{
		"status":        models.TaskStatusFailed,
		"error_message": errorMsg,
		"current_stage": "failed",
	}).Error; err != nil {
		log.Printf("Failed to update task failure status: %v", err)
	}

	log.Printf("Task %s marked as failed: %s", task.ID, errorMsg)
}
