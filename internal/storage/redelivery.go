package storage

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/zfogg/spaniel/internal/model"
	"gorm.io/gorm/clause"
)

// RedeliveryKey is an installation-local key for opaque, stable finding identities.
// The detector never stores the underlying message identity in its own state on disk.
func (d *DB) RedeliveryKey() ([32]byte, error) {
	var result [32]byte
	var candidate [32]byte
	if _, err := rand.Read(candidate[:]); err != nil {
		return result, err
	}
	entry := &model.Meta{Key: "redelivery_hmac_key", Value: hex.EncodeToString(candidate[:])}
	if err := d.namedQuery("RedeliveryKeyCreate").Meta.Clauses(clause.OnConflict{DoNothing: true}).Create(entry); err != nil {
		return result, err
	}
	stored, err := d.query.Meta.Where(d.query.Meta.Key.Eq(entry.Key)).First()
	if err != nil {
		return result, err
	}
	decoded, err := hex.DecodeString(stored.Value)
	if err != nil || len(decoded) != len(result) {
		return result, fmt.Errorf("invalid redelivery key")
	}
	copy(result[:], decoded)
	return result, nil
}

func (d *DB) GetTraceIssue(id string) (*TraceIssue, error) {
	return d.query.TraceIssue.Where(d.query.TraceIssue.ID.Eq(id)).First()
}
