package models

import (
	"time"
)

// User represents a system user
type User struct {
	ID        string    `json:"id" db:"id"`
	Username  string    `json:"username" db:"username"`
	Password  string    `json:"-" db:"password"` // never expose
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// LoginRequest represents login credentials
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// TokenResponse represents JWT token response
type TokenResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}
