package auth

import (
	"errors"
	"time"
)

var (
	ErrCredentials  = errors.New("invalid username or password")
	ErrUnauthorized = errors.New("authentication required")
	ErrConflict     = errors.New("administrator initialization already completed or users already exist")
	ErrInvalid      = errors.New("invalid request")
)

type Limited struct{ Retry time.Duration }

func (e *Limited) Error() string { return "too many requests" }

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type account struct {
	User
	PasswordHash string
	Status       string
	AuthVersion  int64
}

type InitialAdmin struct{ Username, Email, Password, DisplayName string }

// LoginOutcome contains only classified metadata; input identifiers and credentials
// never enter the durable login stream.
type LoginOutcome struct {
	UserID, IP, UserAgent, Outcome string
}

type session struct {
	User User
	ID   string
}
