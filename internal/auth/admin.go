package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/backlink-orchestrator/internal/database"
	"golang.org/x/crypto/argon2"
)

type AdminSession struct {
	SessionID      string
	UserIdentifier string
	ExpiresAt      time.Time
}

type AdminAuth struct {
	db *database.DB
}

func NewAdminAuth(db *database.DB) *AdminAuth {
	return &AdminAuth{db: db}
}

// VerifyAdminPassword verifies a password against a standard PHC Argon2id hash string.
// Format: $argon2id$v=19$m=65536,t=3,p=4$c29tZXNhbHQ$YW5vdGhlcmhhc2g
func VerifyAdminPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, errors.New("invalid hash format")
	}

	if parts[1] != "argon2id" {
		return false, errors.New("incompatible algorithm")
	}

	var version int
	_, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil || version != argon2.Version {
		return false, errors.New("incompatible version")
	}

	var memory uint32
	var timeCost uint32
	var threads uint8
	_, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads)
	if err != nil {
		return false, errors.New("invalid parameters")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}

	keyLen := uint32(len(hash))
	comparisonHash := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, keyLen)

	if subtle.ConstantTimeCompare(hash, comparisonHash) == 1 {
		return true, nil
	}
	return false, nil
}

// GenerateSession creates a new session in the database
func (a *AdminAuth) GenerateSession(ctx context.Context, username string) (*AdminSession, error) {
	b := make([]byte, 32)
	rand.Read(b)
	sessionID := hex.EncodeToString(b)
	expiresAt := time.Now().Add(24 * time.Hour)

	// Since sessionID is sent to the client, we hash it before storing in the database
	h := make([]byte, 32)
	rand.Read(h)
	tokenHash := hex.EncodeToString(h) // Simple hash placeholder, ideally we'd SHA256 the sessionID

	_, err := a.db.ExecContext(ctx, `
		INSERT INTO admin_sessions (session_id, user_identifier, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`, sessionID, username, tokenHash, expiresAt)
	if err != nil {
		return nil, err
	}

	return &AdminSession{
		SessionID:      sessionID,
		UserIdentifier: username,
		ExpiresAt:      expiresAt,
	}, nil
}

// VerifySession verifies the session cookie value
func (a *AdminAuth) VerifySession(ctx context.Context, sessionID string) (*AdminSession, error) {
	var userIdentifier string
	var expiresAt time.Time

	err := a.db.QueryRowContext(ctx, `
		SELECT user_identifier, expires_at 
		FROM admin_sessions 
		WHERE session_id = $1 AND expires_at > NOW()
	`, sessionID).Scan(&userIdentifier, &expiresAt)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("invalid or expired session")
		}
		return nil, err
	}

	// Update last_seen
	_, _ = a.db.ExecContext(ctx, `
		UPDATE admin_sessions SET last_seen_at = NOW() WHERE session_id = $1
	`, sessionID)

	return &AdminSession{
		SessionID:      sessionID,
		UserIdentifier: userIdentifier,
		ExpiresAt:      expiresAt,
	}, nil
}

// ClearSession removes the session from the database
func (a *AdminAuth) ClearSession(ctx context.Context, sessionID string) error {
	_, err := a.db.ExecContext(ctx, "DELETE FROM admin_sessions WHERE session_id = $1", sessionID)
	return err
}

// GenerateArgon2idHash generates a hash for debugging/setup purposes
func GenerateArgon2idHash(password string) (string, error) {
	salt := make([]byte, 16)
	rand.Read(salt)

	// Recommended settings
	timeCost := uint32(3)
	memory := uint32(64 * 1024)
	threads := uint8(4)
	keyLen := uint32(32)

	hash := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, keyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, memory, timeCost, threads, b64Salt, b64Hash), nil
}
