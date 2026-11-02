// Package security owns token authentication and a separate, private DuckDB.
// It deliberately does not depend on the telemetry storage package.
package security

import (
	"bytes"
	"context"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alifiroozi80/duckdb"
	"github.com/zfogg/spaniel/internal/security/model"
	"github.com/zfogg/spaniel/internal/security/querygen"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Token struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Status    string     `json:"status"`
}

type CreateInput struct {
	Name      string     `json:"name" jsonschema:"required human-readable token name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	NoExpiry  bool       `json:"no_expiry,omitempty"`
}

type Created struct {
	Token  Token  `json:"token"`
	Secret string `json:"secret"`
}

type entry struct {
	row  model.Token
	hash [32]byte
}
type session struct {
	tokenID string
	expires time.Time
}

type Service struct {
	mu       sync.Mutex
	db       *gorm.DB
	pool     *sql.DB
	aead     cipher.AEAD
	key      []byte
	entries  map[string]entry
	sessions map[string]session
	enabled  atomic.Bool
	// changes closes on revocation/configuration changes. Watchers revalidate.
	changes      chan struct{}
	now          func() time.Time
	internalHash [32]byte
	hasInternal  bool
	publicURL    atomic.Pointer[url.URL]
}

func RandomSecret() string { return randomString(32) }
func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("security: random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func Open(path, password string) (*Service, error) {
	if password == "" {
		return nil, errors.New("encryption password is required")
	}
	g, err := gorm.Open(duckdb.New(duckdb.Config{DSN: path + "?threads=1&memory_limit=128MB&enable_external_access=false&autoload_known_extensions=false&autoinstall_known_extensions=false&lock_configuration=true"}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), SkipDefaultTransaction: true})
	if err != nil {
		return nil, fmt.Errorf("open private security database: %w", err)
	}
	pool, err := g.DB()
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(1)
	fail := func(err error) (*Service, error) { _ = pool.Close(); return nil, err }
	// Raw GORM is used only for private schema migration.
	if err := g.AutoMigrate(&model.Token{}, &model.Key{}); err != nil {
		return fail(err)
	}
	if path != "" && path != ":memory:" {
		if err := os.Chmod(path, 0o600); err != nil {
			return fail(err)
		}
	}
	s := &Service{db: g, pool: pool, entries: map[string]entry{}, sessions: map[string]session{}, changes: make(chan struct{}), now: time.Now}
	q := querygen.Use(g)
	keys, err := q.Key.WithContext(context.Background()).Find()
	if err != nil {
		return fail(err)
	}
	if len(keys) == 0 {
		s.key = make([]byte, 32)
		if _, err = rand.Read(s.key); err != nil {
			return fail(err)
		}
		wrapper, err := wrapKey(s.key, password)
		if err != nil {
			return fail(err)
		}
		if err = q.Key.WithContext(context.Background()).Create(wrapper); err != nil {
			return fail(err)
		}
	} else {
		for _, wrapper := range keys {
			key, e := unwrapKey(wrapper, password)
			if e == nil {
				s.key = key
				if _, err = q.Key.WithContext(context.Background()).Where(q.Key.ID.Neq(wrapper.ID)).Delete(); err != nil {
					return fail(err)
				}
				break
			}
		}
		if s.key == nil {
			return fail(errors.New("cannot unlock security.duckdb: incorrect encryption password or damaged key; authentication was not disabled"))
		}
	}
	s.aead, err = chacha20poly1305.NewX(s.key)
	if err != nil {
		return fail(err)
	}
	rows, err := q.Token.WithContext(context.Background()).Find()
	if err != nil {
		return fail(err)
	}
	for _, row := range rows {
		secret, e := s.decryptToken(*row)
		if e != nil {
			return fail(errors.New("cannot decrypt API token record"))
		}
		// Upgrade pre-release records whose metadata was not yet authenticated.
		if !bytes.HasPrefix(row.Secret, tokenMagic) {
			row.Secret = s.encryptToken(*row, secret)
			if _, err = q.Token.WithContext(context.Background()).Where(q.Token.ID.Eq(row.ID)).Updates(row); err != nil {
				return fail(err)
			}
		}
		s.entries[row.ID] = entry{row: *row, hash: sha256.Sum256(secret)}
	}
	return s, nil
}

func (s *Service) Close() error  { return s.pool.Close() }
func (s *Service) Enabled() bool { return s.enabled.Load() }

// SetEnabledOnStartup restores enforcement before listeners start. Expired or
// absent credentials must leave the server locked, with recovery UI available.
// Interactive enable operations use Configure's usable-token invariant instead.
func (s *Service) SetEnabledOnStartup(enabled bool) { s.enabled.Store(enabled) }
func seal(a cipher.AEAD, data, aad []byte) []byte {
	nonce := make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic("security: random source unavailable")
	}
	return a.Seal(nonce, nonce, data, aad)
}
func open(a cipher.AEAD, data, aad []byte) ([]byte, error) {
	if len(data) < a.NonceSize() {
		return nil, errors.New("invalid ciphertext")
	}
	return a.Open(nil, data[:a.NonceSize()], data[a.NonceSize():], aad)
}
func (s *Service) open(data, aad []byte) ([]byte, error) { return open(s.aead, data, aad) }

var tokenMagic = []byte("spaniel-token-v1\x00")

func tokenAAD(row model.Token) []byte {
	data, _ := json.Marshal([]any{"spaniel-token-v1", row.ID, row.Name, row.Prefix, row.CreatedAt, row.ExpiresAt, row.RevokedAt})
	return data
}
func (s *Service) encryptToken(row model.Token, secret []byte) []byte {
	return append(append([]byte{}, tokenMagic...), seal(s.aead, secret, tokenAAD(row))...)
}
func (s *Service) decryptToken(row model.Token) ([]byte, error) {
	if bytes.HasPrefix(row.Secret, tokenMagic) {
		return s.open(row.Secret[len(tokenMagic):], tokenAAD(row))
	}
	return s.open(row.Secret, []byte(row.ID))
}
func passwordAEAD(password string, salt []byte) (cipher.AEAD, error) {
	return chacha20poly1305.NewX(argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32))
}
func wrapKey(key []byte, password string) (*model.Key, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	a, err := passwordAEAD(password, salt)
	if err != nil {
		return nil, err
	}
	id := randomString(12)
	return &model.Key{ID: id, Salt: salt, Wrapped: seal(a, key, []byte("spaniel-key-v1:"+id))}, nil
}
func unwrapKey(k *model.Key, password string) ([]byte, error) {
	if len(k.Salt) != 16 {
		return nil, errors.New("invalid key salt")
	}
	a, err := passwordAEAD(password, k.Salt)
	if err != nil {
		return nil, err
	}
	return open(a, k.Wrapped, []byte("spaniel-key-v1:"+k.ID))
}

func (s *Service) notifyLocked() { close(s.changes); s.changes = make(chan struct{}) }

// Configure persists before publishing. The extra key wrapper remains usable if
// a crash interrupts the database/config-file transition during password rotation.
func (s *Service) Configure(enabled bool, password string, persist func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if enabled {
		found := false
		for _, e := range s.entries {
			if s.validLocked(e) == "" {
				found = true
				break
			}
		}
		if !found {
			return errors.New("create a usable API token before enabling security")
		}
	}
	var wrapper *model.Key
	var err error
	if password != "" {
		wrapper, err = wrapKey(s.key, password)
		if err != nil {
			return err
		}
		if err = querygen.Use(s.db).Key.WithContext(context.Background()).Create(wrapper); err != nil {
			return err
		}
	}
	if persist != nil {
		if err = persist(); err != nil {
			return err
		}
	}
	s.enabled.Store(enabled)
	s.notifyLocked()
	if wrapper != nil {
		q := querygen.Use(s.db).Key
		_, err = q.WithContext(context.Background()).Where(q.ID.Neq(wrapper.ID)).Delete()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) List() []Token {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Token, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, s.metadata(e))
	}
	return out
}
func (s *Service) metadata(e entry) Token {
	t := Token{ID: e.row.ID, Name: e.row.Name, Prefix: e.row.Prefix, CreatedAt: time.Unix(0, e.row.CreatedAt).UTC(), Status: "active"}
	if e.row.ExpiresAt != 0 {
		at := time.Unix(0, e.row.ExpiresAt).UTC()
		t.ExpiresAt = &at
	}
	switch s.validLocked(e) {
	case "token_revoked":
		t.Status = "revoked"
	case "token_expired":
		t.Status = "expired"
	}
	return t
}
func (s *Service) Create(ctx context.Context, in CreateInput) (Created, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createLocked(ctx, in, "")
}
func (s *Service) createLocked(ctx context.Context, in CreateInput, legacy string) (Created, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 128 {
		return Created{}, errors.New("name must contain 1 to 128 bytes")
	}
	if in.NoExpiry && in.ExpiresAt != nil {
		return Created{}, errors.New("choose expires_at or no_expiry")
	}
	now := s.now().UTC()
	expiry := int64(0)
	if !in.NoExpiry {
		at := now.AddDate(1, 0, 0)
		if in.ExpiresAt != nil {
			at = in.ExpiresAt.UTC()
		}
		if !at.After(now) || !time.Unix(0, at.UnixNano()).Equal(at) {
			return Created{}, errors.New("expiry must be in the future")
		}
		expiry = at.UnixNano()
	}
	id := randomString(12)
	secret := "spn_" + id + "_" + randomString(32)
	if legacy != "" {
		secret = legacy
		sum := sha256.Sum256([]byte(secret))
		id = "legacy-" + base64.RawURLEncoding.EncodeToString(sum[:12])
		if _, ok := s.entries[id]; ok {
			return Created{}, nil
		}
	}
	row := model.Token{ID: id, Name: name, Prefix: "spn_" + id, CreatedAt: now.UnixNano(), ExpiresAt: expiry}
	row.Secret = s.encryptToken(row, []byte(secret))
	if err := querygen.Use(s.db).Token.WithContext(ctx).Create(&row); err != nil {
		return Created{}, err
	}
	e := entry{row: row, hash: sha256.Sum256([]byte(secret))}
	s.entries[id] = e
	return Created{Token: s.metadata(e), Secret: secret}, nil
}

func (s *Service) ImportLegacy(secret string) error {
	if secret == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.createLocked(context.Background(), CreateInput{Name: "Legacy bearer token", NoExpiry: true}, secret)
	return err
}

func (s *Service) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return errors.New("token not found")
	}
	if e.row.RevokedAt != 0 {
		return nil
	}
	if s.Enabled() && s.validLocked(e) == "" {
		found := false
		for other, v := range s.entries {
			if other != id && s.validLocked(v) == "" {
				found = true
				break
			}
		}
		if !found {
			return errors.New("cannot delete the final usable token while security is enabled; disable security first")
		}
	}
	secret, err := s.decryptToken(e.row)
	if err != nil {
		return errors.New("cannot decrypt API token record")
	}
	e.row.RevokedAt = s.now().UnixNano()
	e.row.Secret = s.encryptToken(e.row, secret)
	q := querygen.Use(s.db).Token
	if _, err := q.WithContext(ctx).Where(q.ID.Eq(id)).Updates(&e.row); err != nil {
		return err
	}
	s.entries[id] = e
	s.notifyLocked()
	return nil
}

func (s *Service) validLocked(e entry) string {
	if e.row.RevokedAt != 0 {
		return "token_revoked"
	}
	if e.row.ExpiresAt != 0 && !s.now().Before(time.Unix(0, e.row.ExpiresAt)) {
		return "token_expired"
	}
	return ""
}
func (s *Service) validateLocked(secret string) (string, string) {
	if secret == "" {
		return "", "token_required"
	}
	hash := sha256.Sum256([]byte(secret))
	if s.hasInternal && subtle.ConstantTimeCompare(hash[:], s.internalHash[:]) == 1 {
		return "internal", ""
	}
	for id, e := range s.entries {
		if subtle.ConstantTimeCompare(hash[:], e.hash[:]) == 1 {
			return id, s.validLocked(e)
		}
	}
	return "", "token_invalid"
}
func (s *Service) Validate(secret string) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validateLocked(secret)
}

// SetInternalToken supplies a process-local credential for self-exporters.
// It is random per process, never persisted or exposed as a managed token.
func (s *Service) SetInternalToken(secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.internalHash = sha256.Sum256([]byte(secret))
	s.hasInternal = true
}
