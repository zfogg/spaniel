package storage

import (
	"errors"
	"fmt"
	"gorm.io/gorm"
)

// DefinitionUpdated is shared across API, MCP and browser clients. Background
// reads never acknowledge a revision. The seen revision survives restarts.
func (d *DB) DefinitionUpdated(kind, id, hash string, revision int64) (bool, error) {
	if hash == "" {
		return false, nil
	}
	key := "definition_seen:" + kind + ":" + id
	seen, err := d.query.Meta.Where(d.query.Meta.Key.Eq(key)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return seen.Value != fmt.Sprintf("%s:%d", hash, revision), nil
}

// MarkDefinitionSeen records exactly the revision returned to the caller, so
// an edit racing a read remains unread. Call only for explicit detail access.
func (d *DB) MarkDefinitionSeen(kind, id, hash string, revision int64) error {
	if hash == "" {
		return nil
	}
	return d.namedQuery("MarkDefinitionSeen").Meta.UpsertValue("definition_seen:"+kind+":"+id, fmt.Sprintf("%s:%d", hash, revision))
}
