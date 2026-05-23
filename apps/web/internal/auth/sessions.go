package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrSessionNotFound e retornado quando uma sessao web nao existe, expirou
// ou foi revogada.
var ErrSessionNotFound = errors.New("session not found")

// Session representa uma sessao web ativa.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ExpiresAt time.Time
}

// SessionStore persiste tokens opacos de sessao na tabela user_sessions.
// O token bruto e devolvido ao chamador (vira cookie), enquanto so o hash
// SHA-256 e gravado no banco  comprometer o banco nao expoe os tokens.
type SessionStore struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

// NewSessionStore cria um store com pool postgres e tempo de vida das
// sessoes.
func NewSessionStore(pool *pgxpool.Pool, ttl time.Duration) *SessionStore {
	return &SessionStore{pool: pool, ttl: ttl}
}

// Create gera um novo token de sessao para o usuario informado e devolve
// o token em texto claro (para uso no cookie) junto da expiracao.
func (s *SessionStore) Create(ctx context.Context, userID uuid.UUID, userAgent, ip string) (token string, expires time.Time, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("gerar token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	hashHex := hex.EncodeToString(hash[:])

	expires = time.Now().Add(s.ttl).UTC()

	var ipArg any
	if ip != "" {
		ipArg = ip
	}

	_, err = s.pool.Exec(ctx, `
		INSERT INTO user_sessions (user_id, token_hash, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, userID, hashHex, userAgent, ipArg, expires)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("inserir sessao: %w", err)
	}
	return token, expires, nil
}

// Lookup resolve um token de sessao para a sessao correspondente, se ainda
// estiver valida. Retorna ErrSessionNotFound em qualquer outro caso.
func (s *SessionStore) Lookup(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrSessionNotFound
	}
	hash := sha256.Sum256([]byte(token))
	hashHex := hex.EncodeToString(hash[:])

	var sess Session
	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, expires_at
		FROM user_sessions
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND expires_at > NOW()
	`, hashHex).Scan(&sess.ID, &sess.UserID, &sess.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrSessionNotFound
		}
		return Session{}, fmt.Errorf("buscar sessao: %w", err)
	}
	return sess, nil
}

// LookupWithUser resolve token de sessao  Sessao + Usuario + permissoes
// em UMA UNICA query, fazendo o JOIN com usuarios / user_roles /
// role_permissions / permissions. Reduz auth de 3 round-trips para 1.
func (s *SessionStore) LookupWithUser(ctx context.Context, token string) (User, Session, error) {
	if token == "" {
		return User{}, Session{}, ErrSessionNotFound
	}
	hash := sha256.Sum256([]byte(token))
	hashHex := hex.EncodeToString(hash[:])

	var (
		u     User
		sess  Session
		perms []string
	)
	err := s.pool.QueryRow(ctx, `
		SELECT
		    u.id, u.login, u.email::text, u.nome, u.papel::text, u.ativo,
		    s.id, s.user_id, s.expires_at,
		    COALESCE(
		        array_agg(DISTINCT p.codigo) FILTER (WHERE p.codigo IS NOT NULL),
		        '{}'
		    ) AS permissions
		FROM user_sessions s
		JOIN usuarios u ON u.id = s.user_id
		LEFT JOIN user_roles ur ON ur.user_id = u.id
		LEFT JOIN role_permissions rp ON rp.role_id = ur.role_id
		LEFT JOIN permissions p ON p.id = rp.permission_id
		WHERE s.token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.expires_at > NOW()
		  AND u.deleted_at IS NULL
		GROUP BY u.id, u.login, u.email, u.nome, u.papel, u.ativo,
		         s.id, s.user_id, s.expires_at
	`, hashHex).Scan(
		&u.ID, &u.Login, &u.Email, &u.Nome, &u.Papel, &u.Ativo,
		&sess.ID, &sess.UserID, &sess.ExpiresAt,
		&perms,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, Session{}, ErrSessionNotFound
		}
		return User{}, Session{}, fmt.Errorf("lookup session+user: %w", err)
	}
	if !u.Ativo {
		return User{}, Session{}, ErrUserDisabled
	}
	u.Permissions = perms
	return u, sess, nil
}

// Revoke marca uma sessao como invalidada (logout). Idempotente.
func (s *SessionStore) Revoke(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	hash := sha256.Sum256([]byte(token))
	hashHex := hex.EncodeToString(hash[:])
	_, err := s.pool.Exec(ctx, `
		UPDATE user_sessions
		SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL
	`, hashHex)
	if err != nil {
		return fmt.Errorf("revogar sessao: %w", err)
	}
	return nil
}

// Purge remove sessoes expiradas. Pode ser chamada por uma rotina periodica
// para conter o crescimento da tabela.
func (s *SessionStore) Purge(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_sessions WHERE expires_at < NOW() - INTERVAL '7 days'`)
	return err
}
