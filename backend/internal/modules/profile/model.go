package profile

import (
	"errors"
	"time"

	"goadminkit/backend/internal/modules/auth"
)

var (
	ErrInvalid  = errors.New("invalid profile input")
	ErrPassword = errors.New("password verification failed")
	ErrConflict = errors.New("profile conflict")
	ErrNotFound = errors.New("session not found")
)

type Input struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

type PasswordInput struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type Session struct {
	ID         string    `json:"id"`
	Current    bool      `json:"current"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type SessionPage struct {
	Items    []Session `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

// credential is private to the service/repository boundary, never an HTTP DTO.
type credential struct {
	User auth.User
	Hash string
}
