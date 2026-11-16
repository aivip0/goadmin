package logs

import (
	"errors"
	"regexp"
	"time"
)

var ErrInvalid = errors.New("invalid log filter")
var ErrNotFound = errors.New("log entry not found")
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var actionPattern = regexp.MustCompile(`^[a-z_]{1,64}$`)

type Filter struct {
	Page, PageSize          int
	From, To                time.Time
	UserID, Action, Outcome string
}

type Entry struct {
	ID            string    `json:"id"`
	UserID        *string   `json:"user_id"`
	Actor         string    `json:"actor"`
	Action        string    `json:"action,omitempty"`
	TargetType    string    `json:"target_type,omitempty"`
	TargetID      string    `json:"target_id,omitempty"`
	Outcome       string    `json:"outcome"`
	AccountKind   string    `json:"account_kind,omitempty"`
	RequestMethod string    `json:"request_method,omitempty"`
	RequestPath   string    `json:"request_path,omitempty"`
	ClientIP      string    `json:"client_ip,omitempty"`
	UserAgent     string    `json:"user_agent,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type Page struct {
	Items    []Entry `json:"items"`
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}
