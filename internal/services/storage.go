package services

import (
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// StorageService handles file storage operations
type StorageService struct {
	uploadDir string
}

// NewStorageService creates a new storage service
func NewStorageService(uploadDir string) *StorageService {
	return &StorageService{
		uploadDir: uploadDir,
	}
}

// ValidateFile validates uploaded file
func (s *StorageService) ValidateFile(fileHeader *multipart.FileHeader) error {
	// Check file size (max 50MB)
	maxSize := int64(50 * 1024 * 1024) // 50MB
	if fileHeader.Size > maxSize {
		return fmt.Errorf("file size exceeds 50MB limit")
	}

	// Check file extension
	allowedFormats := []string{".wav", ".mp3", ".m4a", ".aac"}
	ext := strings.ToLower(filepath.Ext(fileHeader.Filename))

	isValidFormat := false
	for _, format := range allowedFormats {
		if ext == format {
			isValidFormat = true
			break
		}
	}

	if !isValidFormat {
		return fmt.Errorf("invalid file format: only wav, mp3, m4a, aac are allowed")
	}

	return nil
}

// SaveFile saves uploaded file to disk
func (s *StorageService) SaveFile(file multipart.File, fileHeader *multipart.FileHeader) (string, error) {
	// Generate unique filename
	ext := filepath.Ext(fileHeader.Filename)
	uniqueID := uuid.New().String()
	filename := uniqueID + ext
	filePath := filepath.Join(s.uploadDir, filename)

	// Create destination file
	dst, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer dst.Close()

	// Copy uploaded file to destination
	_, err = io.Copy(dst, file)
	if err != nil {
		return "", fmt.Errorf("failed to save file: %w", err)
	}

	return filePath, nil
}

// DeleteFile deletes a file from disk
func (s *StorageService) DeleteFile(filePath string) error {
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}
