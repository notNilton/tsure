package middleware

import (
	"sync"
	"time"

	"tsure/apps/web/internal/auth"
)

// SessionCache mantem em memoria o resultado do lookup de sessao + usuario
// + permissoes por um TTL curto. Evita 3 queries por request quando o
// banco e remoto (WAN). Tolerancia: ate o TTL, alteracoes em papel/perms
// ou desligar usuario podem nao ser refletidas imediatamente.
type SessionCache struct {
	mu    sync.RWMutex
	items map[string]sessionEntry
	ttl   time.Duration
}

type sessionEntry struct {
	user    auth.User
	session auth.Session
	expires time.Time
}

// NewSessionCache cria um cache com TTL informado (ex: 30s).
func NewSessionCache(ttl time.Duration) *SessionCache {
	return &SessionCache{
		items: make(map[string]sessionEntry, 64),
		ttl:   ttl,
	}
}

// Get retorna a entrada se ainda valida, ou ok=false.
func (c *SessionCache) Get(token string) (auth.User, auth.Session, bool) {
	c.mu.RLock()
	e, ok := c.items[token]
	c.mu.RUnlock()
	if !ok {
		return auth.User{}, auth.Session{}, false
	}
	if time.Now().After(e.expires) {
		return auth.User{}, auth.Session{}, false
	}
	return e.user, e.session, true
}

// Put armazena uma resolucao bem-sucedida.
func (c *SessionCache) Put(token string, u auth.User, s auth.Session) {
	c.mu.Lock()
	c.items[token] = sessionEntry{user: u, session: s, expires: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// Invalidate descarta uma entrada (chamado no logout).
func (c *SessionCache) Invalidate(token string) {
	c.mu.Lock()
	delete(c.items, token)
	c.mu.Unlock()
}

// Sweep remove entradas expiradas. Pode ser chamado por uma rotina
// periodica; nao e essencial (entradas expiradas sao detectadas no Get).
func (c *SessionCache) Sweep() {
	now := time.Now()
	c.mu.Lock()
	for k, v := range c.items {
		if now.After(v.expires) {
			delete(c.items, k)
		}
	}
	c.mu.Unlock()
}
