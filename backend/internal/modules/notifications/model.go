package notifications

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid notification")
var ErrNotFound = errors.New("notification not found")
var ErrRecipient = errors.New("recipient not active")
var ErrForbidden = errors.New("permission revoked")
var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Input struct {
	RecipientID string `json:"recipient_id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
}

func (i *Input) validate() error {
	i.Title = strings.TrimSpace(i.Title)
	i.Body = strings.TrimSpace(i.Body)
	if !uuid.MatchString(i.RecipientID) || utf8.RuneCountInString(i.Title) < 1 || utf8.RuneCountInString(i.Title) > 160 || utf8.RuneCountInString(i.Body) < 1 || utf8.RuneCountInString(i.Body) > 4000 {
		return ErrInvalid
	}
	return nil
}

type Item struct {
	ID          string     `json:"id"`
	RecipientID string     `json:"recipient_id"`
	SenderID    string     `json:"sender_id"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	ReadAt      *time.Time `json:"read_at"`
	CreatedAt   time.Time  `json:"created_at"`
}
type Page struct {
	Items    []Item `json:"items"`
	Total    int    `json:"total"`
	Unread   int    `json:"unread"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
