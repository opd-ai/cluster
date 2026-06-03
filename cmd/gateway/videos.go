// videos.go adds /v1/videos/generations and /v1/videos/edits to the gateway.
//
// Video generation is long-running, so these endpoints return a job_id
// immediately.  Clients poll GET /v1/videos/jobs/{id} for status.  When
// the job completes, the output mp4 and preview GIF are available at the
// URLs returned in the job status response.
//
// Jobs are stored in-memory (restart clears queue).  Persistent storage
// is on the roadmap (MinIO outputs/<date>/<job-id>/).
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// -------------------------------------------------------------------------
// Job types
// -------------------------------------------------------------------------

// jobStatus represents the state of an async video generation job.
type jobStatus string

const (
	jobPending   jobStatus = "pending"
	jobRunning   jobStatus = "running"
	jobCompleted jobStatus = "completed"
	jobFailed    jobStatus = "failed"
)

// videoJob holds state for a single video generation job.
type videoJob struct {
	ID        string    `json:"id"`
	Status    jobStatus `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	VideoURL  string    `json:"video_url,omitempty"`
	GifURL    string    `json:"gif_url,omitempty"`
	Error     string    `json:"error,omitempty"`

	APIKey string // track for concurrent job limiting
	req    videoGenerationRequest
}

// videoJobStore is a simple in-memory job registry.
type videoJobStore struct {
	mu               sync.RWMutex
	jobs             map[string]*videoJob
	concurrentByKey  map[string]int // track in-flight jobs per API key
	maxConcurrentKey int            // max concurrent jobs per key
	maxConcurrentAll int            // max concurrent jobs globally
	concurrentAll    int            // current global count
}

var globalVideoJobs = &videoJobStore{
	jobs:             make(map[string]*videoJob),
	concurrentByKey:  make(map[string]int),
	maxConcurrentKey: 10,  // max 10 in-flight videos per API key
	maxConcurrentAll: 100, // max 100 in-flight videos globally
}

func (s *videoJobStore) canAdd(j *videoJob) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.concurrentAll >= s.maxConcurrentAll {
		return false
	}
	if s.concurrentByKey[j.APIKey] >= s.maxConcurrentKey {
		return false
	}
	return true
}

func (s *videoJobStore) add(j *videoJob) {
	s.mu.Lock()
	s.jobs[j.ID] = j
	s.concurrentAll++
	s.concurrentByKey[j.APIKey]++
	s.mu.Unlock()
}

func (s *videoJobStore) markTerminal(j *videoJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.concurrentAll > 0 {
		s.concurrentAll--
	}
	if s.concurrentByKey[j.APIKey] > 0 {
		s.concurrentByKey[j.APIKey]--
	}
}

func (s *videoJobStore) get(id string) (*videoJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

// getSnapshot returns a value copy of the job under the read lock, preventing
// data races between the JSON encoder and concurrent writes in runVideoJob.
func (s *videoJobStore) getSnapshot(id string) (videoJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return videoJob{}, false
	}
	return *j, true
}

// pruneOldJobs removes completed and failed jobs older than the given retention window.
// This prevents unbounded memory growth from long-running gateway processes.
func (s *videoJobStore) pruneOldJobs(retentionWindow time.Duration) {
	cutoff := time.Now().UTC().Add(-retentionWindow)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, job := range s.jobs {
		if (job.Status == jobCompleted || job.Status == jobFailed) && job.UpdatedAt.Before(cutoff) {
			delete(s.jobs, id)
		}
	}
}

// -------------------------------------------------------------------------
// Request / response types
// -------------------------------------------------------------------------

// videoGenerationRequest is the /v1/videos/generations body.
type videoGenerationRequest struct {
	Prompt         string `json:"prompt"`
	Model          string `json:"model"`
	Duration       int    `json:"duration_seconds"` // hint; model may override
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	ResponseFormat string `json:"response_format"` // "url" (default)
	User           string `json:"user"`
	// Image seed for img→video (base64 PNG or URL)
	Image string `json:"image,omitempty"`
	// Video for video→video edits (base64 or URL)
	Video string `json:"video,omitempty"`
}

// videoEditRequest is the /v1/videos/edits body (video→video or img→video).
type videoEditRequest struct {
	Prompt string `json:"prompt"`
	Video  string `json:"video,omitempty"` // base64 or URL
	Image  string `json:"image,omitempty"` // base64 or URL
	Model  string `json:"model"`
}

// jobResponse is the immediate response after job submission.
type jobResponse struct {
	JobID     string    `json:"job_id"`
	Status    jobStatus `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// -------------------------------------------------------------------------
// Handlers
// -------------------------------------------------------------------------

// handleVideoGenerations submits a text→video job and returns a job_id.
func (gw *Gateway) handleVideoGenerations(w http.ResponseWriter, r *http.Request) {
	if gw.swarmURL == "" {
		http.Error(w, `{"error":"video generation backend not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req videoGenerationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	key := extractBearerToken(r)
	if !gw.checkNSFW(w, req.Prompt) {
		return
	}
	if !gw.checkVideoQuota(w, key) {
		return
	}

	job := newVideoJob(req, key)
	if !globalVideoJobs.canAdd(job) {
		http.Error(w, `{"error":"too many concurrent video jobs"}`, http.StatusTooManyRequests)
		return
	}
	globalVideoJobs.add(job)

	go gw.runVideoJob(job)

	writeJSON(w, jobResponse{
		JobID:     job.ID,
		Status:    job.Status,
		CreatedAt: job.CreatedAt,
	})
}

// handleVideoEdits submits a video-edit (img→video or video→video) job.
func (gw *Gateway) handleVideoEdits(w http.ResponseWriter, r *http.Request) {
	if gw.swarmURL == "" {
		http.Error(w, `{"error":"video generation backend not configured"}`, http.StatusServiceUnavailable)
		return
	}

	var req videoEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}

	key := extractBearerToken(r)
	if !gw.checkNSFW(w, req.Prompt) {
		return
	}
	if !gw.checkVideoQuota(w, key) {
		return
	}

	genReq := videoGenerationRequest{
		Prompt: req.Prompt,
		Model:  req.Model,
		Image:  req.Image,
		Video:  req.Video,
	}
	job := newVideoJob(genReq, key)
	if !globalVideoJobs.canAdd(job) {
		http.Error(w, `{"error":"too many concurrent video jobs"}`, http.StatusTooManyRequests)
		return
	}
	globalVideoJobs.add(job)

	go gw.runVideoJob(job)

	writeJSON(w, jobResponse{
		JobID:     job.ID,
		Status:    job.Status,
		CreatedAt: job.CreatedAt,
	})
}

// handleVideoJobStatus returns the current status of a video job.
func (gw *Gateway) handleVideoJobStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	snapshot, ok := globalVideoJobs.getSnapshot(id)
	if !ok {
		http.Error(w, `{"error":"job not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, snapshot)
}

// -------------------------------------------------------------------------
// Job execution
// -------------------------------------------------------------------------

func newVideoJob(req videoGenerationRequest, apiKey string) *videoJob {
	now := time.Now().UTC()
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback to timestamp on entropy failure (extremely unlikely).
		return &videoJob{
			ID:        fmt.Sprintf("vid-%d", now.UnixNano()),
			Status:    jobPending,
			CreatedAt: now,
			UpdatedAt: now,
			APIKey:    apiKey,
			req:       req,
		}
	}
	id := "vid-" + hex.EncodeToString(buf[:])
	return &videoJob{
		ID:        id,
		Status:    jobPending,
		CreatedAt: now,
		UpdatedAt: now,
		APIKey:    apiKey,
		req:       req,
	}
}

func (gw *Gateway) runVideoJob(job *videoJob) {
	defer func() {
		// Decrement concurrent count when job completes or fails
		globalVideoJobs.markTerminal(job)
	}()

	setJobStatus(job, jobRunning, "")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	videoURL, gifURL, err := gw.generateVideo(ctx, job.req)
	if err != nil {
		log.Printf("video job %s failed: %v", job.ID, err)
		setJobStatus(job, jobFailed, err.Error())
		return
	}

	globalVideoJobs.mu.Lock()
	job.Status = jobCompleted
	job.VideoURL = videoURL
	job.GifURL = gifURL
	job.UpdatedAt = time.Now().UTC()
	globalVideoJobs.mu.Unlock()
}

func setJobStatus(job *videoJob, s jobStatus, errMsg string) {
	globalVideoJobs.mu.Lock()
	job.Status = s
	job.UpdatedAt = time.Now().UTC()
	if errMsg != "" {
		job.Error = errMsg
	}
	globalVideoJobs.mu.Unlock()
}

// generateVideo calls the SwarmUI backend and returns (videoURL, gifURL).
func (gw *Gateway) generateVideo(ctx context.Context, req videoGenerationRequest) (string, string, error) {
	model := swarmVideoModel(req.Model)

	w := req.Width
	h := req.Height
	if w <= 0 {
		w = 832
	} else if w > 2560 {
		w = 2560 // cap at max reasonable width
	}
	if h <= 0 {
		h = 480
	} else if h > 1440 {
		h = 1440 // cap at max reasonable height
	}

	swarmReq := map[string]any{
		"prompt":    req.Prompt,
		"model":     model,
		"width":     w,
		"height":    h,
		"donotsave": false,
	}
	if req.Image != "" {
		swarmReq["init_image"] = req.Image
	}
	if req.Video != "" {
		swarmReq["init_video"] = req.Video
	}

	data, err := json.Marshal(swarmReq)
	if err != nil {
		return "", "", err
	}

	httpClient := &http.Client{Timeout: 20 * time.Minute}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		gw.swarmURL+"/API/GenerateVideo", bytes.NewReader(data))
	if err != nil {
		return "", "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	var result struct {
		VideoPath string `json:"video_path"`
		GifPath   string `json:"gif_path"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("parse swarm video response: %w", err)
	}
	if result.Error != "" {
		return "", "", fmt.Errorf("swarm error: %s", result.Error)
	}

	videoURL := result.VideoPath
	gifURL := result.GifPath
	if len(videoURL) > 0 && videoURL[0] == '/' {
		videoURL = gw.swarmURL + videoURL
	}
	if len(gifURL) > 0 && gifURL[0] == '/' {
		gifURL = gw.swarmURL + gifURL
	}

	// Use path.Base so we report clean output names in logs.
	log.Printf("video job done: %s", path.Base(videoURL))
	return videoURL, gifURL, nil
}

// swarmVideoModel maps an API model name to a SwarmUI video model identifier.
func swarmVideoModel(model string) string {
	switch model {
	case "animatediff":
		return "animatediff/dreamshaper_8.safetensors"
	case "cogvideox", "cogvideox-5b":
		return "CogVideoX-5B"
	case "hunyuan", "hunyuan-video":
		return "HunyuanVideo"
	case "ltx", "ltx-video":
		return "ltxv-0.9.1.safetensors"
	case "wan", "wan2", "wan-2.1":
		return "Wan2.1-T2V-14B.safetensors"
	case "wan-i2v":
		return "Wan2.1-I2V-14B-480P.safetensors"
	case "":
		return "" // SwarmUI picks default
	default:
		return model
	}
}

// pruneVideoJobsLoop periodically removes old completed/failed video jobs
// to prevent unbounded memory growth.
func pruneVideoJobsLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			globalVideoJobs.pruneOldJobs(24 * time.Hour)
		}
	}
}
