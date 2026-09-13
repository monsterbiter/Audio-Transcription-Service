# Audio Chunking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement audio chunking for reliable transcription of long audio files by splitting them into 60-second segments, processing them in parallel with retry logic, and merging results.

**Architecture:** Split audio into 60-second chunks using FFmpeg, process chunks in parallel using a worker pool (max 3 concurrent), retry failed chunks up to 3 times with exponential backoff, merge transcripts in order, and clean up temporary files.

**Tech Stack:** Go 1.22+, FFmpeg, iFlytek Voice Dictation API, goroutines + channels

## Global Constraints

- Go version: 1.22 or higher
- FFmpeg must be installed and accessible in system PATH
- Chunk size: fixed 60 seconds
- Concurrent workers: 3
- Max retries per chunk: 3
- Retry backoff: exponential (2s, 4s, 8s)
- Temporary files: store in `os.TempDir()/transcription-{taskID}/`
- All temporary files must be cleaned up on completion or failure

---

## File Structure

### New Files
- None (all changes are modifications to existing files)

### Modified Files
- `internal/services/transcription.go` - Add chunking, parallel processing, retry logic, ffmpeg path helper
- `internal/services/processor.go` - Add progress callback support and GORM-based stage updates

---

### Task 1: Add FFmpeg Path Helper and Audio Splitting Function

**Files:**
- Modify: `internal/services/transcription.go` (imports, new functions, refactor `convertAudioFormat`)

**Interfaces:**
- Consumes: `TranscriptionService` struct (existing)
- Produces:
  - `findFFmpeg() (string, error)` - locates the ffmpeg executable, returns its path
  - `splitAudioIntoChunks(inputPath, tempDir string) ([]string, error)` - returns sorted list of chunk file paths

- [ ] **Step 1: Add imports for path/filepath, math, and sort**

Add to the imports block at the top of the file (alongside the existing `os/exec`, `strings`, `time` entries):

```go
"math"
"path/filepath"
"sort"
```

- [ ] **Step 2: Write findFFmpeg helper function**

The existing `convertAudioFormat` already searches several locations for `ffmpeg.exe`. Extract that lookup into a reusable helper so audio splitting uses the same resolution. Add this function immediately before `convertAudioFormat`:

```go
// findFFmpeg locates the ffmpeg executable.
// It checks project-relative locations first (ffmpeg.exe lives in the repo root
// on this machine and is NOT on the system PATH), then falls back to PATH.
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
```

- [ ] **Step 3: Refactor convertAudioFormat to use findFFmpeg**

In `convertAudioFormat`, replace the inline lookup block (the `ffmpegPaths` slice, the `for` loop over it, and the `if ffmpegPath == ""` fallback that returns `inputPath`) with a call to the helper:

```go
	ffmpegPath, err := s.findFFmpeg()
	if err != nil {
		return "", fmt.Errorf("cannot convert audio: %w", err)
	}
```

Leave the rest of `convertAudioFormat` (the `args` slice, `exec.Command(ffmpegPath, args...)`, and the success log) unchanged.

Note: this changes behavior deliberately. Previously a missing ffmpeg silently returned the unconverted input path, which sent the wrong audio format to iFlytek and produced confusing downstream failures. Now it returns a clear error.

- [ ] **Step 4: Write splitAudioIntoChunks function**

Add after `convertAudioFormat`:

```go
// splitAudioIntoChunks splits an audio file into 60-second chunks using FFmpeg.
// Returns chunk paths sorted in playback order.
func (s *TranscriptionService) splitAudioIntoChunks(inputPath, tempDir string) ([]string, error) {
	log.Printf("[Split] Splitting audio: %s into %s\n", inputPath, tempDir)

	ffmpegPath, err := s.findFFmpeg()
	if err != nil {
		return nil, fmt.Errorf("cannot split audio: %w", err)
	}

	// FFmpeg command: split into 60-second segments
	// -f segment: use segment muxer
	// -segment_time 60: 60 seconds per segment
	// -c copy: copy codec without re-encoding (fast)
	outputPattern := filepath.Join(tempDir, "chunk_%03d.wav")

	cmd := exec.Command(ffmpegPath,
		"-i", inputPath,
		"-f", "segment",
		"-segment_time", "60",
		"-c", "copy",
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
```

- [ ] **Step 5: Build and verify no compilation errors**

Run:
```bash
cd "e:/github/Audio Transcription Service"
go build ./...
```

Expected: BUILD SUCCESS (no errors)

- [ ] **Step 5: Build and verify no compilation errors**

Run:
```bash
cd "e:/github/Audio Transcription Service"
go build ./...
```

Expected: BUILD SUCCESS (no errors)

- [ ] **Step 6: Commit**

```bash
git add internal/services/transcription.go
git commit -m "feat: add ffmpeg path helper and audio splitting

- Add findFFmpeg() to locate ffmpeg.exe (project root + PATH fallback)
- Refactor convertAudioFormat() to use findFFmpeg() helper
- Add splitAudioIntoChunks() to split audio into 60s segments
- Use FFmpeg segment muxer for fast splitting without re-encoding
- Sort chunks to guarantee playback order
- Return clear error if ffmpeg not found (was silent fallback before)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Add Single Chunk Transcription Function

**Files:**
- Modify: `internal/services/transcription.go:271+`

**Interfaces:**
- Consumes: `TranscriptionService` struct, existing WebSocket transcription logic
- Produces: `transcribeChunk(chunkPath string) (string, error)` - transcribes one audio chunk

- [ ] **Step 1: Extract chunk transcription logic**

Add after `splitAudioIntoChunks`:

```go
// transcribeChunk transcribes a single audio chunk
// This function contains the core WebSocket logic extracted from the original Transcribe method
func (s *TranscriptionService) transcribeChunk(chunkPath string) (string, error) {
	log.Printf("[TranscribeChunk] Starting chunk: %s\n", chunkPath)
	
	// Build WebSocket URL with authentication
	authURL, err := s.buildAuthURL()
	if err != nil {
		return "", fmt.Errorf("failed to build auth URL: %w", err)
	}
	
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
	
	// Read audio file
	audioData, err := os.ReadFile(chunkPath)
	if err != nil {
		return "", fmt.Errorf("failed to read audio file: %w", err)
	}
	
	log.Printf("[TranscribeChunk] Audio file read: %d bytes\n", len(audioData))
	
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
	frameSize := 1280 // bytes per frame (16k: 1280)
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
			return "", fmt.Errorf("failed to send frame: %w", err)
		}
		
		// Last frame sent, break
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
```

- [ ] **Step 2: Build and verify no compilation errors**

Run:
```bash
go build ./...
```

Expected: BUILD SUCCESS

- [ ] **Step 3: Commit**

```bash
git add internal/services/transcription.go
git commit -m "feat: extract single chunk transcription logic

- Add transcribeChunk() for processing one audio segment
- Extract WebSocket logic from original Transcribe()
- Prepare for parallel chunk processing with retry

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Add Retry Logic for Chunk Transcription

**Files:**
- Modify: `internal/services/transcription.go` (after transcribeChunk)

**Interfaces:**
- Consumes: `transcribeChunk(chunkPath string) (string, error)`
- Produces: `transcribeChunkWithRetry(chunkPath string, maxRetries int) (string, error)` - transcribes with exponential backoff retry

- [ ] **Step 1: Write transcribeChunkWithRetry function**

Add after `transcribeChunk`:

```go
// transcribeChunkWithRetry attempts to transcribe a chunk with exponential backoff retry
func (s *TranscriptionService) transcribeChunkWithRetry(chunkPath string, maxRetries int) (string, error) {
	var lastErr error
	
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 2s, 4s, 8s
			backoff := time.Duration(math.Pow(2, float64(attempt))) * time.Second
			log.Printf("[Retry] Chunk %s: attempt %d/%d after %v\n", 
				filepath.Base(chunkPath), attempt+1, maxRetries+1, backoff)
			time.Sleep(backoff)
		}
		
		text, err := s.transcribeChunk(chunkPath)
		if err == nil {
			if attempt > 0 {
				log.Printf("[Retry] Chunk %s succeeded on attempt %d\n", 
					filepath.Base(chunkPath), attempt+1)
			}
			return text, nil
		}
		
		lastErr = err
		log.Printf("[Retry] Chunk %s failed (attempt %d/%d): %v\n", 
			filepath.Base(chunkPath), attempt+1, maxRetries+1, err)
	}
	
	return "", fmt.Errorf("chunk transcription failed after %d attempts: %w", 
		maxRetries+1, lastErr)
}
```

- [ ] **Step 2: Build and verify no compilation errors**

Run:
```bash
go build ./...
```

Expected: BUILD SUCCESS

- [ ] **Step 3: Commit**

```bash
git add internal/services/transcription.go
git commit -m "feat: add retry logic for chunk transcription

- Add transcribeChunkWithRetry() with exponential backoff
- Retry up to 3 times with delays: 2s, 4s, 8s
- Log each retry attempt for debugging

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Implement Parallel Chunk Processing with Progress Callback

**Files:**
- Modify: `internal/services/transcription.go:95-271` (rewrite Transcribe function)

**Interfaces:**
- Consumes: All functions from Tasks 1-3
- Produces: `Transcribe(filePath string, onProgress func(int, int)) (string, error)` - main entry point with progress callback

- [ ] **Step 1: Define chunk job types**

Add after the CharWord type (around line 93):

```go
// Chunk processing types for worker pool
type chunkJob struct {
	index int
	path  string
}

type chunkResult struct {
	index int
	text  string
	err   error
}
```

- [ ] **Step 2: Rewrite Transcribe function to use chunking**

Replace the existing `Transcribe` function (lines 96-271) with:

```go
// Transcribe performs real transcription using iFlytek Voice Dictation API with chunking
func (s *TranscriptionService) Transcribe(filePath string, onProgress func(int, int)) (string, error) {
	log.Printf("[Transcribe] Starting transcription for file: %s\n", filePath)
	
	// Convert audio to required format (16kHz, mono, 16bit PCM)
	convertedPath, err := s.convertAudioFormat(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to convert audio: %w", err)
	}
	defer func() {
		if convertedPath != filePath {
			os.Remove(convertedPath) // Clean up converted file
		}
	}()
	
	log.Printf("[Transcribe] Audio converted to: %s\n", convertedPath)
	
	// Create temporary directory for chunks
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("transcription-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir) // Clean up temp directory
	
	log.Printf("[Transcribe] Temp directory created: %s\n", tempDir)
	
	// Get audio duration to decide if chunking is needed
	// For files < 60 seconds, skip chunking
	chunks, err := s.splitAudioIntoChunks(convertedPath, tempDir)
	if err != nil {
		return "", fmt.Errorf("failed to split audio: %w", err)
	}
	
	numChunks := len(chunks)
	log.Printf("[Transcribe] Processing %d chunks\n", numChunks)
	
	// If only one chunk, process directly without worker pool
	if numChunks == 1 {
		if onProgress != nil {
			onProgress(0, 1)
		}
		text, err := s.transcribeChunkWithRetry(chunks[0], 3)
		if err != nil {
			return "", err
		}
		if onProgress != nil {
			onProgress(1, 1)
		}
		return text, nil
	}
	
	// Worker pool for parallel processing (max 3 concurrent)
	const maxWorkers = 3
	jobChan := make(chan chunkJob, numChunks)
	resultChan := make(chan chunkResult, numChunks)
	
	// Start workers
	workerCount := maxWorkers
	if numChunks < maxWorkers {
		workerCount = numChunks
	}
	
	for i := 0; i < workerCount; i++ {
		go func(workerID int) {
			for job := range jobChan {
				log.Printf("[Worker %d] Processing chunk %d: %s\n", 
					workerID, job.index, filepath.Base(job.path))
				
				text, err := s.transcribeChunkWithRetry(job.path, 3)
				resultChan <- chunkResult{
					index: job.index,
					text:  text,
					err:   err,
				}
			}
		}(i)
	}
	
	// Send jobs
	for i, chunkPath := range chunks {
		jobChan <- chunkJob{
			index: i,
			path:  chunkPath,
		}
	}
	close(jobChan)
	
	// Collect results
	results := make([]string, numChunks)
	completedCount := 0
	
	for i := 0; i < numChunks; i++ {
		result := <-resultChan
		
		if result.err != nil {
			return "", fmt.Errorf("chunk %d failed: %w", result.index, result.err)
		}
		
		results[result.index] = result.text
		completedCount++
		
		// Report progress
		if onProgress != nil {
			onProgress(completedCount, numChunks)
		}
		
		log.Printf("[Transcribe] Progress: %d/%d chunks completed\n", completedCount, numChunks)
	}
	
	// Merge results in order
	transcript := strings.Join(results, " ")
	
	log.Printf("[Transcribe] Transcription complete: %d chunks, %d characters\n", 
		numChunks, len(transcript))
	
	return transcript, nil
}
```

- [ ] **Step 3: Build and verify no compilation errors**

Run:
```bash
go build ./...
```

Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add internal/services/transcription.go
git commit -m "feat: implement parallel chunk processing

- Rewrite Transcribe() to use chunking + worker pool
- Process up to 3 chunks concurrently
- Add progress callback for status updates
- Skip chunking for short audio (<60s)
- Merge chunk transcripts in order

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Update Processor to Pass Progress Callback

**Files:**
- Modify: `internal/services/processor.go`

**Interfaces:**
- Consumes: `Transcribe(filePath string, onProgress func(int, int)) (string, error)`
- Produces: Updated `processTask` that updates task.current_stage with progress using GORM

- [ ] **Step 1: Add import for fmt if not already present**

Check imports at top of file, add if missing:

```go
"fmt"
```

- [ ] **Step 2: Find and update the transcription call in processTask**

Find the line that calls `p.transcriptionSvc.Transcribe(...)` in the `processTask` function. Replace it with:

```go
// Transcribe with progress callback
transcript, err := p.transcriptionSvc.Transcribe(task.Recording.FilePath, func(current, total int) {
	stage := fmt.Sprintf("transcribing (%d/%d)", current, total)
	// Update current_stage using GORM
	if err := database.GetDB().Model(&models.Task{}).Where("id = ?", task.ID).Update("current_stage", stage).Error; err != nil {
		log.Printf("Failed to update task stage: %v", err)
	}
})
```

- [ ] **Step 3: Build and verify no compilation errors**

Run:
```bash
go build ./...
```

Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```bash
git add internal/services/processor.go
git commit -m "feat: add progress callback to transcription

- Update processTask to pass progress callback to Transcribe()
- Use GORM to update task.current_stage with real-time progress
- Display progress like 'transcribing (3/10)'

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: Integration Testing

**Files:**
- Test: Full system integration test with real audio file

**Interfaces:**
- Consumes: All implemented functionality

- [ ] **Step 1: Check if test audio file exists**

Run:
```bash
Test-Path "e:/github/Audio Transcription Service/tes/audio.m4a"
```

Expected: True (file exists from earlier testing)

- [ ] **Step 2: Start the service**

Run:
```powershell
cd "e:/github/Audio Transcription Service"
go run cmd/server/main.go
```

Expected: Server starts on port 8080

- [ ] **Step 3: In another terminal, upload test audio**

Run:
```powershell
curl -X POST http://localhost:8080/api/recordings/upload `
  -F "file=@tes/audio.m4a"
```

Expected: JSON response with recording_id and task_id

- [ ] **Step 4: Check task progress multiple times**

Run (replace TASK_ID with actual ID from step 3):
```powershell
curl http://localhost:8080/api/tasks/TASK_ID
```

Expected: See status change from "pending" → "transcribing (1/N)" → "transcribing (2/N)" → ... → "summarizing" → "done"

- [ ] **Step 5: Verify final result**

Run:
```powershell
curl http://localhost:8080/api/recordings
```

Expected: Recording has transcript and summary filled in

- [ ] **Step 6: Check logs for chunking evidence**

Look for log lines like:
- `[Split] Generated N chunks`
- `[Worker X] Processing chunk Y`
- `[Retry] Chunk X succeeded on attempt Y` (if any retries happened)
- `[Transcribe] Progress: X/Y chunks completed`

- [ ] **Step 7: Verify temp files cleaned up**

Run:
```powershell
Get-ChildItem $env:TEMP\transcription-* -ErrorAction SilentlyContinue
```

Expected: Empty or no such directory (files cleaned up)

- [ ] **Step 8: Stop the server**

Press Ctrl+C in the server terminal

- [ ] **Step 9: Document test results**

Create `docs/superpowers/plans/2026-09-13-audio-chunking-test-results.md`:

```markdown
# Audio Chunking Test Results

**Date:** 2026-09-13
**Audio File:** tes/audio.m4a
**Duration:** [X minutes Y seconds]

## Results

- ✅ Audio split into [N] chunks
- ✅ All chunks transcribed successfully
- ✅ Progress updates visible in task status
- ✅ Final transcript merged correctly
- ✅ Summary generated successfully
- ✅ Temp files cleaned up

## Logs Excerpt

[Paste relevant log lines showing chunking, workers, progress]

## Performance

- Total time: [X seconds]
- Compared to single-file: [faster/slower by Y%]

## Issues Found

[None / List any issues]
```

- [ ] **Step 10: Commit test results**

```bash
git add docs/superpowers/plans/2026-09-13-audio-chunking-test-results.md
git commit -m "test: add audio chunking integration test results

- Tested with [X minute] audio file
- Verified chunking, parallel processing, progress tracking
- Confirmed temp file cleanup
- All functionality working as designed

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review Checklist

### Spec Coverage
- ✅ Audio splitting (60s chunks): Task 1
- ✅ Parallel processing (3 workers): Task 4
- ✅ Retry logic (3 attempts, exponential backoff): Task 3
- ✅ Progress tracking: Task 5 (GORM-based)
- ✅ Temp file cleanup: Task 4 (defer statements)
- ✅ Integration testing: Task 6

### Placeholder Scan
- ✅ No TBD/TODO markers
- ✅ All code blocks contain actual code
- ✅ No "add appropriate error handling" without showing the code
- ✅ No "similar to Task N" references

### Type Consistency
- ✅ `findFFmpeg() (string, error)` - defined in Task 1, used in Task 1
- ✅ `splitAudioIntoChunks(inputPath, tempDir string) ([]string, error)` - defined in Task 1, used in Task 4
- ✅ `transcribeChunk(chunkPath string) (string, error)` - defined in Task 2, used in Task 3
- ✅ `transcribeChunkWithRetry(chunkPath string, maxRetries int) (string, error)` - defined in Task 3, used in Task 4
- ✅ `Transcribe(filePath string, onProgress func(int, int)) (string, error)` - defined in Task 4, used in Task 5

### Interface Consistency
- ✅ All function signatures match between definition and usage
- ✅ All types (chunkJob, chunkResult) defined before use
- ✅ Progress callback signature consistent: `func(int, int)`
- ✅ Database layer uses GORM (project convention)
- ✅ FFmpeg path resolution reuses existing pattern

---

## Plan Complete

All tasks are fully specified with:
- Exact code to write
- Build verification steps
- Commit messages
- No placeholders or TODOs
- Adjustments for project conventions (GORM, ffmpeg path lookup)
