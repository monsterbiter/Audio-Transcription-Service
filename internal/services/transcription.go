package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// TranscriptionService handles audio transcription using iFlytek ASR
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

// IFlytekRequest represents the request structure for iFlytek ASR
type IFlytekRequest struct {
	Header    IFlytekHeader    `json:"header"`
	Parameter IFlytekParameter `json:"parameter,omitempty"`
	Payload   IFlytekPayload   `json:"payload"`
}

type IFlytekHeader struct {
	AppID  string `json:"app_id"`
	Status int    `json:"status"` // 0:首帧 1:中间帧 2:最后一帧
}

type IFlytekParameter struct {
	IAT IATParameter `json:"iat"`
}

type IATParameter struct {
	Domain   string       `json:"domain"`
	Language string       `json:"language"`
	Accent   string       `json:"accent"`
	EOS      int          `json:"eos,omitempty"`
	DWA      string       `json:"dwa,omitempty"`
	Result   ResultConfig `json:"result"`
}

type ResultConfig struct {
	Encoding string `json:"encoding"`
	Compress string `json:"compress"`
	Format   string `json:"format"`
}

type IFlytekPayload struct {
	Audio AudioData `json:"audio"`
}

type AudioData struct {
	Encoding   string `json:"encoding"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	BitDepth   int    `json:"bit_depth"`
	Seq        int    `json:"seq"`
	Status     int    `json:"status"`
	Audio      string `json:"audio"`
}

// IFlytekResponse represents the response structure
type IFlytekResponse struct {
	Header  ResponseHeader  `json:"header"`
	Payload ResponsePayload `json:"payload"`
}

type ResponseHeader struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	SID     string `json:"sid"`
	Status  int    `json:"status"`
}

type ResponsePayload struct {
	Result ResultData `json:"result"`
}

type ResultData struct {
	Text string `json:"text"`
}

// TextResult represents the decoded text result
type TextResult struct {
	SN int          `json:"sn"`
	LS bool         `json:"ls"`
	WS []WordResult `json:"ws"`
}

type WordResult struct {
	CW []struct {
		W string `json:"w"`
	} `json:"cw"`
}

// Transcribe performs real transcription using iFlytek ASR API
func (s *TranscriptionService) Transcribe(filePath string) (string, error) {
	// Build WebSocket URL with authentication
	authURL, err := s.buildAuthURL()
	if err != nil {
		return "", fmt.Errorf("failed to build auth URL: %w", err)
	}

	// Connect to WebSocket
	dialer := websocket.Dialer{
		HandshakeTimeout: 30 * time.Second,
	}

	conn, _, err := dialer.Dial(authURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to connect to WebSocket: %w", err)
	}
	defer conn.Close()

	// Read audio file
	audioData, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio file: %w", err)
	}

	// Channel to collect results
	resultChan := make(chan string, 100)
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

			if resp.Header.Code != 0 {
				errorChan <- fmt.Errorf("iFlytek error: code=%d, message=%s", resp.Header.Code, resp.Header.Message)
				return
			}

			// Decode text result
			if resp.Payload.Result.Text != "" {
				textData, err := base64.StdEncoding.DecodeString(resp.Payload.Result.Text)
				if err != nil {
					continue
				}

				var textResult TextResult
				if err := json.Unmarshal(textData, &textResult); err != nil {
					continue
				}

				// Extract text from word results
				for _, ws := range textResult.WS {
					for _, cw := range ws.CW {
						transcript.WriteString(cw.W)
					}
				}
			}

			// Check if this is the last frame
			if resp.Header.Status == 2 {
				resultChan <- transcript.String()
				return
			}
		}
	}()

	// Send audio data in chunks
	frameSize := 1280 // bytes per frame
	interval := 40 * time.Millisecond
	seq := 0

	for offset := 0; offset < len(audioData) || offset == 0; offset += frameSize {
		var status int
		var audioChunk []byte

		if offset == 0 {
			status = 0 // First frame
		} else if offset+frameSize >= len(audioData) {
			status = 2 // Last frame
			if offset < len(audioData) {
				audioChunk = audioData[offset:]
			}
		} else {
			status = 1 // Middle frame
			audioChunk = audioData[offset : offset+frameSize]
		}

		// Build request
		req := IFlytekRequest{
			Header: IFlytekHeader{
				AppID:  s.appID,
				Status: status,
			},
			Payload: IFlytekPayload{
				Audio: AudioData{
					Encoding:   "raw",
					SampleRate: 16000,
					Channels:   1,
					BitDepth:   16,
					Seq:        seq,
					Status:     status,
					Audio:      base64.StdEncoding.EncodeToString(audioChunk),
				},
			},
		}

		// Add parameter only in first frame
		if status == 0 {
			req.Parameter = IFlytekParameter{
				IAT: IATParameter{
					Domain:   "slm",
					Language: "zh_cn",
					Accent:   "mandarin",
					EOS:      6000,
					DWA:      "wpgs",
					Result: ResultConfig{
						Encoding: "utf8",
						Compress: "raw",
						Format:   "json",
					},
				},
			}
		}

		// Send frame
		if err := conn.WriteJSON(req); err != nil {
			return "", fmt.Errorf("failed to send frame: %w", err)
		}

		seq++

		// Last frame, break
		if status == 2 {
			break
		}

		// Wait before sending next frame
		time.Sleep(interval)
	}

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

// buildAuthURL builds the authenticated WebSocket URL
func (s *TranscriptionService) buildAuthURL() (string, error) {
	// Parse base URL
	u, err := url.Parse(s.wsURL)
	if err != nil {
		return "", err
	}

	// Generate RFC1123 date
	date := time.Now().UTC().Format(http.TimeFormat)

	// Build signature origin
	signatureOrigin := fmt.Sprintf("host: %s\ndate: %s\nGET %s HTTP/1.1",
		u.Host, date, u.Path)

	// Calculate HMAC-SHA256
	h := hmac.New(sha256.New, []byte(s.apiSecret))
	h.Write([]byte(signatureOrigin))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// Build authorization origin
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

// ConvertToWAV converts audio file to PCM WAV format (if needed)
// For now, we assume the uploaded file is already in compatible format
func (s *TranscriptionService) ConvertToWAV(inputPath string) (string, error) {
	// TODO: Implement audio conversion if needed using ffmpeg
	// For now, return the original path
	return inputPath, nil
}
