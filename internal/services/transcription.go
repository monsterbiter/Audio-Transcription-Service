package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// TranscriptionService handles audio transcription using iFlytek ASR (Voice Dictation API)
type TranscriptionService struct {
	appID     string
	apiKey    string
	apiSecret string
	wsURL     string
}

const (
	// maxAudioSize is the threshold for chunked processing (10MB)
	maxAudioSize = 10 * 1024 * 1024
)

// NewTranscriptionService creates a new transcription service
func NewTranscriptionService(appID, apiKey, apiSecret, wsURL string) *TranscriptionService {
	return &TranscriptionService{
		appID:     appID,
		apiKey:    apiKey,
		apiSecret: apiSecret,
		wsURL:     wsURL,
	}
}

// IFlytekRequest represents the request structure for iFlytek Voice Dictation API
type IFlytekRequest struct {
	Common   CommonParams   `json:"common"`
	Business BusinessParams `json:"business"`
	Data     DataParams     `json:"data"`
}

type CommonParams struct {
	AppID string `json:"app_id"`
}

type BusinessParams struct {
	Language string `json:"language"`
	Domain   string `json:"domain"`
	Accent   string `json:"accent"`
	Vad_eos  int    `json:"vad_eos,omitempty"`
	Dwa      string `json:"dwa,omitempty"`
}

type DataParams struct {
	Status   int    `json:"status"`
	Format   string `json:"format"`
	Encoding string `json:"encoding"`
	Audio    string `json:"audio"`
}

// IFlytekResponse represents the response structure
type IFlytekResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Sid     string          `json:"sid"`
	Data    ResponseData    `json:"data"`
}

type ResponseData struct {
	Result ResultInfo `json:"result"`
	Status int        `json:"status"`
}

type ResultInfo struct {
	Sn  int          `json:"sn"`
	Ls  bool         `json:"ls"`
	Bg  int          `json:"bg"`
	Ed  int          `json:"ed"`
	Ws  []WordResult `json:"ws"`
}

type WordResult struct {
	Bg int        `json:"bg"`
	Cw []CharWord `json:"cw"`
}

type CharWord struct {
	W  string `json:"w"`
	Wp string `json:"wp"`
}

// Transcribe performs audio transcription, automatically chunking large files
func (s *TranscriptionService) Transcribe(filePath string) (string, error) {
	log.Printf("[Transcribe] Starting transcription for file: %s\n", filePath)

	// Convert audio to required format (16kHz, mono, 16bit PCM)
	convertedPath, err := s.convertAudioFormat(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to convert audio: %w", err)
	}
	defer func() {
		if convertedPath != filePath {
			os.Remove(convertedPath)
		}
	}()

	log.Printf("[Transcribe] Audio converted to: %s\n", convertedPath)

	// Check file size
	fileInfo, err := os.Stat(convertedPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat converted file: %w", err)
	}

	// Small files: direct transcription
	if fileInfo.Size() <= maxAudioSize {
		log.Printf("[Transcribe] File size %d bytes <= threshold, using direct transcription\n", fileInfo.Size())
		return s.transcribeSingleFile(convertedPath)
	}

	// Large files: chunked transcription
	log.Printf("[Transcribe] File size %d bytes > threshold, using chunked transcription\n", fileInfo.Size())
	return s.transcribeWithChunks(convertedPath)
}

// transcribeSingleFile performs transcription on a single audio file via WebSocket
func (s *TranscriptionService) transcribeSingleFile(filePath string) (string, error) {
	// Build WebSocket URL with authentication
	authURL, err := s.buildAuthURL()
	if err != nil {
		return "", fmt.Errorf("failed to build auth URL: %w", err)
	}

	log.Printf("[Transcribe] Auth URL built successfully\n")

	// Connect to WebSocket
	dialer := websocket.Dialer{
		HandshakeTimeout: 30 * time.Second,
	}

	conn, resp, err := dialer.Dial(authURL, nil)
	if err != nil {
		if resp != nil {
			return "", fmt.Errorf("failed to connect to WebSocket: %w (status: %d)", err, resp.StatusCode)
		}
		return "", fmt.Errorf("failed to connect to WebSocket: %w", err)
	}
	defer conn.Close()

	log.Printf("[Transcribe] WebSocket connected successfully\n")

	// Read audio file
	audioData, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio file: %w", err)
	}

	log.Printf("[Transcribe] Audio file read: %d bytes\n", len(audioData))

	// Channel to collect results
	resultChan := make(chan string, 1)
	errorChan := make(chan error, 1)
	doneChan := make(chan bool, 1)

	// Start goroutine to receive messages
	go func() {
		defer func() {
			doneChan <- true
		}()

		var transcript strings.Builder
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					return
				}
				errorChan <- fmt.Errorf("failed to read message: %w", err)
				return
			}

			var resp IFlytekResponse
			if err := json.Unmarshal(message, &resp); err != nil {
				continue
			}

			if resp.Code != 0 {
				errorChan <- fmt.Errorf("iFlytek error: code=%d, message=%s, sid=%s", resp.Code, resp.Message, resp.Sid)
				return
			}

			// Extract text from word results
			if resp.Data.Result.Ws != nil {
				for _, ws := range resp.Data.Result.Ws {
					for _, cw := range ws.Cw {
						transcript.WriteString(cw.W)
					}
				}
			}

			// Check if this is the last frame (status=2)
			if resp.Data.Status == 2 {
				resultChan <- transcript.String()
				return
			}
		}
	}()

	// Send audio data in chunks
	frameSize := 1280
	interval := 40 * time.Millisecond

	for offset := 0; offset <= len(audioData); offset += frameSize {
		var status int
		var audioChunk []byte

		if offset == 0 {
			status = 0
		} else if offset >= len(audioData) {
			status = 2
			audioChunk = []byte{}
		} else {
			status = 1
			end := offset + frameSize
			if end > len(audioData) {
				end = len(audioData)
			}
			audioChunk = audioData[offset:end]
		}

		req := IFlytekRequest{
			Common: CommonParams{
				AppID: s.appID,
			},
			Business: BusinessParams{
				Language: "zh_cn",
				Domain:   "iat",
				Accent:   "mandarin",
				Vad_eos:  5000,
				Dwa:      "wpgs",
			},
			Data: DataParams{
				Status:   status,
				Format:   "audio/L16;rate=16000",
				Encoding: "raw",
				Audio:    base64.StdEncoding.EncodeToString(audioChunk),
			},
		}

		if err := conn.WriteJSON(req); err != nil {
			log.Printf("[Transcribe] Failed to send frame %d (status=%d): %v\n", offset/frameSize, status, err)
			return "", fmt.Errorf("failed to send frame: %w", err)
		}

		if offset%10240 == 0 {
			log.Printf("[Transcribe] Sent %d/%d bytes\n", offset, len(audioData))
		}

		if status == 2 {
			log.Printf("[Transcribe] All frames sent, waiting for final response\n")
			break
		}

		time.Sleep(interval)
	}

	log.Printf("[Transcribe] Waiting for transcription result...\n")

	select {
	case result := <-resultChan:
		<-doneChan
		if result == "" {
			return "", fmt.Errorf("empty transcription result from iFlytek")
		}
		return result, nil
	case err := <-errorChan:
		<-doneChan
		return "", err
	case <-time.After(90 * time.Second):
		return "", fmt.Errorf("transcription timeout")
	}
}

// transcribeWithChunks splits audio into chunks, transcribes in parallel, concatenates results
func (s *TranscriptionService) transcribeWithChunks(filePath string) (string, error) {
	// Create temp directory for chunks
	tempDir, err := os.MkdirTemp("", "audio-chunks-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	log.Printf("[Chunked] Created temp directory: %s\n", tempDir)

	// Split audio into chunks
	chunkPaths, err := s.splitAudioIntoChunks(filePath, tempDir)
	if err != nil {
		return "", fmt.Errorf("failed to split audio: %w", err)
	}

	log.Printf("[Chunked] Starting parallel transcription of %d chunks\n", len(chunkPaths))

	// Transcribe chunks in parallel
	type chunkResult struct {
		index int
		text  string
		err   error
	}

	resultsChan := make(chan chunkResult, len(chunkPaths))
	var wg sync.WaitGroup

	for i, chunkPath := range chunkPaths {
		wg.Add(1)
		go func(index int, path string) {
			defer wg.Done()

			log.Printf("[Chunked] Transcribing chunk %d/%d: %s\n", index+1, len(chunkPaths), filepath.Base(path))
			text, err := s.transcribeSingleFile(path)

			resultsChan <- chunkResult{
				index: index,
				text:  text,
				err:   err,
			}
		}(i, chunkPath)
	}

	// Wait for all goroutines
	wg.Wait()
	close(resultsChan)

	// Collect and sort results
	results := make([]chunkResult, 0, len(chunkPaths))
	for result := range resultsChan {
		results = append(results, result)
	}

	// Check for errors
	var errors []string
	for _, result := range results {
		if result.err != nil {
			errors = append(errors, fmt.Sprintf("chunk %d: %v", result.index, result.err))
		}
	}

	if len(errors) > 0 {
		return "", fmt.Errorf("chunk transcription errors: %s", strings.Join(errors, "; "))
	}

	// Sort by index to maintain order
	sort.Slice(results, func(i, j int) bool {
		return results[i].index < results[j].index
	})

	// Concatenate transcripts
	var finalTranscript strings.Builder
	for _, result := range results {
		finalTranscript.WriteString(result.text)
	}

	log.Printf("[Chunked] All chunks transcribed successfully, total length: %d characters\n", finalTranscript.Len())
	return finalTranscript.String(), nil
}

// buildAuthURL builds the authenticated WebSocket URL for Voice Dictation API
func (s *TranscriptionService) buildAuthURL() (string, error) {
	// Parse base URL
	u, err := url.Parse(s.wsURL)
	if err != nil {
		return "", err
	}

	// Generate RFC1123 date
	now := time.Now().UTC()
	date := now.Format(http.TimeFormat)

	// Build signature origin string
	signatureOrigin := fmt.Sprintf("host: %s\ndate: %s\nGET %s HTTP/1.1",
		u.Host, date, u.Path)

	// Calculate HMAC-SHA256 signature
	h := hmac.New(sha256.New, []byte(s.apiSecret))
	h.Write([]byte(signatureOrigin))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// Build authorization origin string
	authorizationOrigin := fmt.Sprintf(
		`api_key="%s", algorithm="hmac-sha256", headers="host date request-line", signature="%s"`,
		s.apiKey, signature)

	// Base64 encode authorization
	authorization := base64.StdEncoding.EncodeToString([]byte(authorizationOrigin))

	// Build query parameters
	query := url.Values{}
	query.Set("authorization", authorization)
	query.Set("date", date)
	query.Set("host", u.Host)

	// Build final URL
	u.RawQuery = query.Encode()

	return u.String(), nil
}

// findFFmpeg locates the ffmpeg executable.
// It checks project-relative locations first, then falls back to PATH.
func (s *TranscriptionService) findFFmpeg() (string, error) {
	candidates := []string{
		"./ffmpeg.exe",
		"ffmpeg.exe",
		"../ffmpeg.exe",
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			log.Printf("[FFmpeg] Found ffmpeg at: %s\n", candidate)
			return candidate, nil
		}
	}

	// Fall back to system PATH
	if path, err := exec.LookPath("ffmpeg"); err == nil {
		log.Printf("[FFmpeg] Using ffmpeg from system PATH: %s\n", path)
		return path, nil
	}

	return "", fmt.Errorf("ffmpeg not found: checked ./ffmpeg.exe, ffmpeg.exe, ../ffmpeg.exe, and system PATH")
}

// convertAudioFormat converts audio to required format using ffmpeg
// Required: 16kHz, mono, 16bit PCM WAV
func (s *TranscriptionService) convertAudioFormat(inputPath string) (string, error) {
	log.Printf("[ConvertAudio] Starting conversion for: %s\n", inputPath)

	// Output path for converted file
	outputPath := inputPath + ".converted.wav"

	// Build ffmpeg command
	// ffmpeg -i input.wav -ar 16000 -ac 1 -sample_fmt s16 output.wav
	args := []string{
		"-i", inputPath,
		"-ar", "16000",       // Sample rate: 16kHz
		"-ac", "1",           // Channels: mono
		"-sample_fmt", "s16", // Sample format: 16-bit signed integer
		"-y",                 // Overwrite output file
		outputPath,
	}

	ffmpegPath, err := s.findFFmpeg()
	if err != nil {
		return "", fmt.Errorf("cannot convert audio: %w", err)
	}

	// Execute ffmpeg
	cmd := exec.Command(ffmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ffmpeg conversion failed: %w, output: %s", err, string(output))
	}

	log.Printf("[ConvertAudio] Conversion successful: %s\n", outputPath)
	return outputPath, nil
}

// splitAudioIntoChunks splits an audio file into 60-second chunks using FFmpeg.
// Returns chunk paths sorted in playback order.
func (s *TranscriptionService) splitAudioIntoChunks(inputPath, tempDir string) ([]string, error) {
	log.Printf("[Split] Splitting audio: %s into %s\n", inputPath, tempDir)

	// Validate input file exists
	if _, err := os.Stat(inputPath); err != nil {
		return nil, fmt.Errorf("input file does not exist: %w", err)
	}

	ffmpegPath, err := s.findFFmpeg()
	if err != nil {
		return nil, fmt.Errorf("cannot split audio: %w", err)
	}

	// FFmpeg command: split into 60-second segments
	// -f segment: use segment muxer
	// -segment_time 60: 60 seconds per segment
	// Encode to WAV format (16kHz, mono, 16bit) to match convertAudioFormat settings
	outputPattern := filepath.Join(tempDir, "chunk_%03d.wav")

	cmd := exec.Command(ffmpegPath,
		"-i", inputPath,
		"-f", "segment",
		"-segment_time", "60",
		"-ar", "16000",       // Sample rate: 16kHz
		"-ac", "1",           // Channels: mono
		"-sample_fmt", "s16", // Sample format: 16-bit signed integer
		outputPattern,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg split failed: %w, output: %s", err, string(output))
	}

	// Find all generated chunk files
	pattern := filepath.Join(tempDir, "chunk_*.wav")
	chunks, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to find chunks: %w", err)
	}

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no chunks generated")
	}

	// Sort chunks by name to guarantee playback order
	sort.Strings(chunks)

	log.Printf("[Split] Generated %d chunks\n", len(chunks))
	return chunks, nil
}

