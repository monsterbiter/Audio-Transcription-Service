package models

import (
	"time"
)

// Recording represents an audio recording record
type Recording struct {
	ID         string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	Filename   string    `gorm:"type:varchar(255);not null" json:"filename"`
	FilePath   string    `gorm:"type:varchar(512);not null" json:"file_path"`
	FileSize   int64     `gorm:"not null" json:"file_size"`
	Format     string    `gorm:"type:varchar(10);not null" json:"format"`
	Transcript string    `gorm:"type:text" json:"transcript,omitempty"`
	Summary    *Summary  `gorm:"type:json" json:"summary,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Summary represents the structured summary result
type Summary struct {
	Summary   string   `json:"summary"`
	KeyPoints []string `json:"key_points"`
	Todos     []string `json:"todos"`
}

// TableName specifies the table name for Recording
func (Recording) TableName() string {
	return "recordings"
}
