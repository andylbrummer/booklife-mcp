package enrichment

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// JobStatus represents the status of an enrichment job
type JobStatus string

const (
	JobStatusPending   JobStatus = "pending"
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusCancelled JobStatus = "cancelled"
)

// JobProgress contains real-time progress information
type JobProgress struct {
	JobID             string     `json:"job_id"`
	Status            JobStatus  `json:"status"`
	TotalBooks        int        `json:"total_books"`
	ProcessedBooks    int        `json:"processed_books"`
	SuccessfulBooks   int        `json:"successful_books"`
	FailedBooks       int        `json:"failed_books"`
	CurrentBook       string     `json:"current_book,omitempty"`
	CurrentAuthor     string     `json:"current_author,omitempty"`
	StartedAt         time.Time  `json:"started_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	ElapsedSeconds    int        `json:"elapsed_seconds"`
	EstimatedSeconds  int        `json:"estimated_seconds,omitempty"`
	AvgSecondsPerBook float64    `json:"avg_seconds_per_book,omitempty"`
	RecentErrors      []string   `json:"recent_errors,omitempty"`
	Error             string     `json:"error,omitempty"`
}

// Job represents a background enrichment job
type Job struct {
	ID       string
	Status   JobStatus
	Progress *JobProgress
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.RWMutex
}

// JobManager manages background enrichment jobs
type JobManager struct {
	jobs map[string]*Job
	mu   sync.RWMutex
}

// NewJobManager creates a new job manager
func NewJobManager() *JobManager {
	return &JobManager{
		jobs: make(map[string]*Job),
	}
}

// CreateJob creates a new enrichment job
func (m *JobManager) CreateJob() *Job {
	m.mu.Lock()
	defer m.mu.Unlock()

	jobID := fmt.Sprintf("enrich-%d", time.Now().Unix())
	ctx, cancel := context.WithCancel(context.Background())

	job := &Job{
		ID:     jobID,
		Status: JobStatusPending,
		Progress: &JobProgress{
			JobID:     jobID,
			Status:    JobStatusPending,
			StartedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		ctx:    ctx,
		cancel: cancel,
	}

	m.jobs[jobID] = job
	return job
}

// GetJob retrieves a job by ID
func (m *JobManager) GetJob(jobID string) (*Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	job, exists := m.jobs[jobID]
	if !exists {
		return nil, fmt.Errorf("job %s not found", jobID)
	}

	return job, nil
}

// GetCurrentJob returns the most recent job
func (m *JobManager) GetCurrentJob() *Job {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var latestJob *Job
	var latestTime time.Time

	for _, job := range m.jobs {
		if job.Progress.StartedAt.After(latestTime) {
			latestTime = job.Progress.StartedAt
			latestJob = job
		}
	}

	return latestJob
}

// CancelJob cancels a running job
func (m *JobManager) CancelJob(jobID string) error {
	job, err := m.GetJob(jobID)
	if err != nil {
		return err
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	if job.Status != JobStatusRunning {
		return fmt.Errorf("job %s is not running (status: %s)", jobID, job.Status)
	}

	job.cancel()
	job.Status = JobStatusCancelled
	job.Progress.Status = JobStatusCancelled
	job.Progress.UpdatedAt = time.Now()

	return nil
}

// UpdateProgress updates the job progress (thread-safe)
func (j *Job) UpdateProgress(update func(*JobProgress)) {
	j.mu.Lock()
	defer j.mu.Unlock()

	update(j.Progress)
	j.Progress.UpdatedAt = time.Now()
	j.Progress.ElapsedSeconds = int(time.Since(j.Progress.StartedAt).Seconds())

	// Calculate estimated time remaining
	if j.Progress.ProcessedBooks > 0 && j.Progress.TotalBooks > 0 {
		j.Progress.AvgSecondsPerBook = float64(j.Progress.ElapsedSeconds) / float64(j.Progress.ProcessedBooks)
		remaining := j.Progress.TotalBooks - j.Progress.ProcessedBooks
		j.Progress.EstimatedSeconds = int(float64(remaining) * j.Progress.AvgSecondsPerBook)
	}
}

// GetProgress returns a copy of the current progress (thread-safe)
func (j *Job) GetProgress() JobProgress {
	j.mu.RLock()
	defer j.mu.RUnlock()

	// Return a copy
	progress := *j.Progress

	// Recalculate elapsed time
	progress.ElapsedSeconds = int(time.Since(progress.StartedAt).Seconds())

	return progress
}

// SetStatus sets the job status (thread-safe)
func (j *Job) SetStatus(status JobStatus) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.Status = status
	j.Progress.Status = status
	j.Progress.UpdatedAt = time.Now()

	if status == JobStatusCompleted || status == JobStatusFailed || status == JobStatusCancelled {
		now := time.Now()
		j.Progress.CompletedAt = &now
	}
}

// AddError adds an error to the recent errors list (keeps last 10)
func (j *Job) AddError(bookTitle, errorMsg string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	errStr := fmt.Sprintf("%s: %s", bookTitle, errorMsg)
	j.Progress.RecentErrors = append(j.Progress.RecentErrors, errStr)

	// Keep only last 10 errors
	if len(j.Progress.RecentErrors) > 10 {
		j.Progress.RecentErrors = j.Progress.RecentErrors[len(j.Progress.RecentErrors)-10:]
	}
}
