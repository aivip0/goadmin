package jobs

import (
	"errors"
	"regexp"
	"time"
)

var ErrInvalid = errors.New("invalid job request")
var ErrNotFound = errors.New("job not found")
var ErrConflict = errors.New("job cannot be retried")
var ErrForbidden = errors.New("permission revoked")
var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const SessionCleanup = "session_cleanup"

type Item struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Status       string     `json:"status"`
	RequestedBy  string     `json:"requested_by"`
	RetryOf      *string    `json:"retry_of"`
	Attempts     int        `json:"attempts"`
	RunAfter     time.Time  `json:"run_after"`
	LeaseUntil   *time.Time `json:"lease_until"`
	LastFailure  *string    `json:"last_failure"`
	AffectedRows int        `json:"affected_rows"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}
type Run struct {
	ID           string     `json:"id"`
	JobID        string     `json:"job_id"`
	Attempt      int        `json:"attempt"`
	Status       string     `json:"status"`
	FailureCode  *string    `json:"failure_code"`
	AffectedRows int        `json:"affected_rows"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}
type Detail struct {
	Item
	Runs []Run `json:"runs"`
}
type Page struct {
	Items    []Item `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
