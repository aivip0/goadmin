package apikeys

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid API key request")
var ErrNotFound = errors.New("API key not found")
var ErrUnauthorized = errors.New("invalid API key")
var ErrForbidden = errors.New("permission revoked")
var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type Input struct {
	Name          string `json:"name"`
	ExpiresInDays int    `json:"expires_in_days"`
}

func (i *Input) validate() error {
	i.Name = strings.TrimSpace(i.Name)
	if len(i.Name) < 1 || len(i.Name) > 100 || i.ExpiresInDays < 1 || i.ExpiresInDays > 90 {
		return ErrInvalid
	}
	return nil
}

type Item struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}
type Created struct {
	Item
	Token string `json:"token"`
}
type Page struct {
	Items    []Item `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}
