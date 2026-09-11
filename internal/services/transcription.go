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
	"strings"
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

// Transcribe performs real transcription using iFlytek Voice Dictation API
func (s *TranscriptionService) Transcribe(filePath string) (string, error) {
	log.Printf("[Transcribe] Starting transcription for file: %s\n", filePath)

	// Convert audio to required format (16kHz, mono, 16bit PCM)
	convertedPath, err := s.convertAudioFormat(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to convert audio: %w", err)
	}
	// Keep converted file for preview instead of deleting
	// defer func() {
	// 	if convertedPath != filePath {
	// 		os.Remove(convertedPath) // Clean up converted file
	// 	}
	// }()

	log.Printf("[Transcribe] Audio converted to: %s\n", convertedPath)

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
	audioData, err := os.ReadFile(convertedPath)
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
					log.Printf("[Transcribe] WebSocket closed normally")
					// Return accumulated transcript when connection closes normally
					if transcript.Len() > 0 {
						resultChan <- transcript.String()
					} else {
						errorChan <- fmt.Errorf("connection closed with no transcript")
					}
					return
				}
				errorChan <- fmt.Errorf("failed to read message: %w", err)
				return
			}

			log.Printf("[Transcribe] Received message: %s", string(message))

			var resp IFlytekResponse
			if err := json.Unmarshal(message, &resp); err != nil {
				log.Printf("[Transcribe] Failed to unmarshal response: %v", err)
				continue
			}

			log.Printf("[Transcribe] Response parsed: code=%d, message=%s, data.status=%d", resp.Code, resp.Message, resp.Data.Status)

			if resp.Code != 0 {
				errorChan <- fmt.Errorf("iFlytek error: code=%d, message=%s, sid=%s", resp.Code, resp.Message, resp.Sid)
				return
			}

			// Extract text from word results
			if resp.Data.Result.Ws != nil {
				log.Printf("[Transcribe] Got %d word segments", len(resp.Data.Result.Ws))
				for _, ws := range resp.Data.Result.Ws {
					for _, cw := range ws.Cw {
						transcript.WriteString(cw.W)
					}
				}
			}

			// Check if this is the last frame (status=2)
			if resp.Data.Status == 2 {
				log.Printf("[Transcribe] Received final response, transcript: %s", transcript.String())
				resultChan <- transcript.String()
				return
			}
		}
	}()

	// Send audio data in chunks
	frameSize := 1280 // bytes per frame (8k: 1280, 16k: 1280)
	interval := 40 * time.Millisecond

	for offset := 0; offset <= len(audioData); offset += frameSize {
		var status int
		var audioChunk []byte

		if offset == 0 {
			status = 0 // First frame
		} else if offset >= len(audioData) {
			status = 2 // Last frame (empty audio)
			audioChunk = []byte{}
		} else {
			status = 1 // Middle frame
			end := offset + frameSize
			if end > len(audioData) {
				end = len(audioData)
			}
			audioChunk = audioData[offset:end]
		}

		// Build request
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

		// Send frame
		if err := conn.WriteJSON(req); err != nil {
			log.Printf("[Transcribe] Failed to send frame %d (status=%d): %v\n", offset/frameSize, status, err)
			return "", fmt.Errorf("failed to send frame: %w", err)
		}

		if offset%10240 == 0 { // Log every 10KB
			log.Printf("[Transcribe] Sent %d/%d bytes\n", offset, len(audioData))
		}

		// Last frame sent, break
		if status == 2 {
			log.Printf("[Transcribe] All frames sent, waiting for final response\n")
			break
		}

		// Wait before sending next frame
		time.Sleep(interval)
	}

	log.Printf("[Transcribe] Waiting for transcription result...\n")

	// Wait for result or error
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

	// Try to find ffmpeg.exe
	ffmpegPaths := []string{
		"./ffmpeg.exe",           // Current directory
		"ffmpeg.exe",             // PATH
		"../ffmpeg.exe",          // Parent directory
	}

	var ffmpegPath string
	for _, path := range ffmpegPaths {
		if _, err := os.Stat(path); err == nil {
			ffmpegPath = path
			log.Printf("[ConvertAudio] Found ffmpeg at: %s\n", path)
			break
		}
	}

	if ffmpegPath == "" {
		// Try system PATH
		if _, err := exec.LookPath("ffmpeg"); err == nil {
			ffmpegPath = "ffmpeg"
			log.Printf("[ConvertAudio] Using ffmpeg from system PATH\n")
		} else {
			log.Printf("[ConvertAudio] WARNING: ffmpeg not found, using original file\n")
			return inputPath, nil
		}
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

