// Package model contains only the private security database schema.
package model

// Token is never exposed through the telemetry database or public SQL.
type Token struct {
	ID        string `gorm:"primaryKey"`
	Name      string
	Prefix    string
	CreatedAt int64
	ExpiresAt int64
	RevokedAt int64
	Secret    []byte
}

func (Token) TableName() string { return "api_tokens" }

// Key wraps a random data encryption key with a password-derived key.
// Multiple wrappers make password changes recoverable across config writes.
type Key struct {
	ID      string `gorm:"primaryKey"`
	Salt    []byte
	Wrapped []byte
}

func (Key) TableName() string { return "security_keys" }
