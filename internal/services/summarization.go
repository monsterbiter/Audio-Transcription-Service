package services

import (
	"audio-transcription-service/internal/models"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SummarizationService handles LLM-based summarization
type SummarizationService struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewSummarizationService creates a new summarization service
func NewSummarizationService(baseURL, apiKey, model string) *SummarizationService {
	return &SummarizationService{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Summarize generates structured summary from transcript using LLM
func (s *SummarizationService) Summarize(transcript string) (*models.Summary, error) {
	prompt := fmt.Sprintf(`请对以下录音转写文本生成结构化摘要,严格按照以下JSON格式返回,不要包含其他内容:
{
  "summary": "一句话总结录音内容(20字以内)",
  "key_points": ["要点1", "要点2", "要点3"],
  "todos": ["待办事项1", "待办事项2"]
}

转写文本:
%s

请直接返回JSON格式,不要添加markdown代码块标记或其他说明文字。`, transcript)

	// Prepare request
	reqBody := map[string]interface{}{
		"model": s.model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": 0.7,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", s.baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	// Send request
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse LLM response
	var llmResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.Unmarshal(body, &llmResp); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	if len(llmResp.Choices) == 0 {
		return nil, fmt.Errorf("LLM returned empty response")
	}

	content := llmResp.Choices[0].Message.Content

	// Parse summary from LLM output
	var summary models.Summary
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		// Try to extract JSON from markdown code block
		content = extractJSON(content)
		if err := json.Unmarshal([]byte(content), &summary); err != nil {
			return nil, fmt.Errorf("failed to parse summary JSON: %w, content: %s", err, content)
		}
	}

	// Validate summary
	if summary.Summary == "" {
		return nil, fmt.Errorf("LLM returned empty summary")
	}

	return &summary, nil
}

// extractJSON attempts to extract JSON from markdown code blocks
func extractJSON(content string) string {
	// Remove markdown code blocks if present
	start := bytes.Index([]byte(content), []byte("```json"))
	if start != -1 {
		content = content[start+7:]
	} else {
		start = bytes.Index([]byte(content), []byte("```"))
		if start != -1 {
			content = content[start+3:]
		}
	}

	end := bytes.Index([]byte(content), []byte("```"))
	if end != -1 {
		content = content[:end]
	}

	return string(bytes.TrimSpace([]byte(content)))
}
