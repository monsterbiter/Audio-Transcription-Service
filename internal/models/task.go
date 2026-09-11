package models

import (
	"time"
)

// TaskStatus represents the status of a task
type TaskStatus string

const (
	TaskStatusPending      TaskStatus = "pending"
	TaskStatusTranscribing TaskStatus = "transcribing"
	TaskStatusSummarizing  TaskStatus = "summarizing"
	TaskStatusDone         TaskStatus = "done"
	TaskStatusFailed       TaskStatus = "failed"
)

// Task represents a processing task
type Task struct {
	ID            string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	RecordingID   string     `gorm:"type:varchar(36);not null;index" json:"recording_id"`
	Status        TaskStatus `gorm:"type:enum('pending','transcribing','summarizing','done','failed');default:'pending';index" json:"status"`
	CurrentStage  string     `gorm:"type:varchar(50)" json:"current_stage,omitempty"`
	ErrorMessage  string     `gorm:"type:text" json:"error_message,omitempty"`
	RetryCount    int        `gorm:"default:0" json:"retry_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Recording     *Recording `gorm:"foreignKey:RecordingID" json:"recording,omitempty"`
}

// TableName specifies the table name for Task
func (Task) TableName() string {
	return "tasks"
}

// CanRetry checks if the task can be retried
func (t *Task) CanRetry() bool {
	return t.Status == TaskStatusFailed
}
