package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"tsure/apps/web/internal/auth"
)

// SessionCookieName e o nome do cookie HttpOnly usado pelo BFF web.
const SessionCookieName = "tsure_session"

// WebAuth e o middleware que protege rotas SSR: exige cookie de sessao
// valido. Em caso de falha, redireciona para /login (GET) ou devolve 401
// para outras verbos. Usa Cache para evitar round-trips ao DB.
type WebAuth struct {
	Users    *auth.Store
	Sessions *auth.SessionStore
	Cache    *SessionCache // opcional; quando setado, evita queries em hits
}

// resolve tenta cache primeiro, depois DB (1 query consolidada).
func (w WebAuth) resolve(r *http.Request, token string) (auth.User, auth.Session, error) {
	if w.Cache != nil {
		if u, s, ok := w.Cache.Get(token); ok {
			return u, s, nil
		}
	}
	user, sess, err := w.Sessions.LookupWithUser(r.Context(), token)
	if err != nil {
		return auth.User{}, auth.Session{}, err
	}
	if w.Cache != nil {
		w.Cache.Put(token, user, sess)
	}
	return user, sess, nil
}

// Required envolve handler em validacao obrigatoria.
func (w WebAuth) Required(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			redirectToLogin(rw, r)
			return
		}
		user, _, err := w.resolve(r, cookie.Value)
		if err != nil {
			if errors.Is(err, auth.ErrSessionNotFound) ||
				errors.Is(err, auth.ErrUserDisabled) ||
				errors.Is(err, auth.ErrInvalidCredentials) {
				redirectToLogin(rw, r)
				return
			}
			http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		ctx := WithUser(r.Context(), user)
		ctx = WithSessionToken(ctx, cookie.Value)
		next.ServeHTTP(rw, r.WithContext(ctx))
	})
}

// Optional injeta o usuario quando ha sessao valida, mas nao bloqueia se
// nao houver. Util para paginas publicas que mudam de aparencia quando
// logado (ex: a propria /login).
func (w WebAuth) Optional(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
			if user, _, err := w.resolve(r, cookie.Value); err == nil {
				ctx = WithUser(ctx, user)
				ctx = WithSessionToken(ctx, cookie.Value)
			}
		}
		next.ServeHTTP(rw, r.WithContext(ctx))
	})
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || strings.HasPrefix(r.URL.Path, "/api/") {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	dst := "/login"
	if r.URL.Path != "" && r.URL.Path != "/" {
		dst += "?next=" + r.URL.Path
	}
	http.Redirect(w, r, dst, http.StatusSeeOther)
}

// APIAuth protege rotas /api/* exigindo Authorization: Bearer <jwt>.
type APIAuth struct {
	Users  *auth.Store
	Signer *auth.JWTSigner
}

// Required valida o JWT e injeta o usuario no contexto.
func (a APIAuth) Required(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeAPIError(rw, http.StatusUnauthorized, "token ausente")
			return
		}
		token := strings.TrimPrefix(header, "Bearer ")
		claims, err := a.Signer.Verify(token)
		if err != nil {
			writeAPIError(rw, http.StatusUnauthorized, "token invalido")
			return
		}
		id, err := uuid.Parse(claims.Sub.String())
		if err != nil {
			writeAPIError(rw, http.StatusUnauthorized, "subject invalido")
			return
		}
		user, err := a.Users.GetByID(r.Context(), id)
		if err != nil {
			writeAPIError(rw, http.StatusUnauthorized, "usuario invalido")
			return
		}
		ctx := WithUser(r.Context(), user)
		next.ServeHTTP(rw, r.WithContext(ctx))
	})
}

// writeAPIError serializa um envelope JSON de erro no padrao da API.
func writeAPIError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data":    nil,
		"message": msg,
		"errors":  []string{msg},
	})
}
