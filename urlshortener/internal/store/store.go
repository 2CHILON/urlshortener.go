package store

import (
	"errors"
	"time"
)

var(
	ErrNotFound = errors.New("link not found")
	ErrExists = errors.New("code already exists")
)

//Link is a single shortened URL plus its usage stats.
type Link struct{
	Code string `json:"code"`
	URL string `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt *time.Time `json:expires_at,omitempy"`
	Clicks int64 `json:"clicks"`
	LastAccess *time.Time `json:"last_access,omitempy"`
}

//Expired reports wether the link is past its expiry at the given instant
func(l Link) Expired(now time.Time) bool {
	return l.ExpiresAt != nil && !now.Before(*l.ExpiresAt)
}

// Store is the persistence interface used by the service layer.
// All methods must be safe for concurrent use.

type Store interface{
	//Create inserts a link; returns ErrExists if the code is taken.
	Create(l Link) error
	//Get returns a copy of the link or ErrNotFound
	Get(code string) (Link, error)
	//RecordClick increaments the click counter and sets LastAccess.
	RecordClick(code string, at time.Time) error
	//Delete removes a link or returns ErrNotFound.
	Delete(code string) error
	//DeleteExpiredBefore removes links whose expiry is before cutoff and
	//return how many were removed.
	DeleteExpiredBefore(cutoff time.Time) int
	// Close flushes pending state and releases resources
	Close() error
}