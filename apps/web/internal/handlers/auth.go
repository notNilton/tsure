// Package handlers contem os handlers HTTP transversais que nao pertencem
// a um dominio especifico  hoje: autenticacao (login/logout) web + API.
package handlers

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	"tsure/apps/web/internal/auth"
	"tsure/apps/web/internal/middleware"
	"tsure/apps/web/internal/render"
)

// AuthHandler agrupa as dependencias para fluxos de login (web + API).
type AuthHandler struct {
	Users        *auth.Store
	Sessions     *auth.SessionStore
	JWT          *auth.JWTSigner
	SecureCookie bool
	Templates    render.Executor
}

// LoginPageData e o ViewModel da pagina /login.
type LoginPageData struct {
	Title     string
	Error     string
	Next      string
	CSRFField template.HTML
}

// ShowLogin renderiza o formulario /login. Se ja houver sessao valida,
// redireciona para "/" ou para o parametro ?next=.
func (h AuthHandler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.UserFromContext(r.Context()); ok {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusSeeOther)
		return
	}
	h.renderLogin(w, r, "", "")
}

// HandleLogin processa POST /login (form-encoded). Em sucesso, cria a
// sessao e define o cookie HttpOnly; em falha, re-renderiza o formulario.
func (h AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "form invalido", http.StatusBadRequest)
		return
	}
	login := strings.TrimSpace(r.FormValue("login"))
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))

	user, err := h.Users.Authenticate(r.Context(), login, password)
	if err != nil {
		msg := "Login ou senha invalidos."
		if errors.Is(err, auth.ErrUserDisabled) {
			msg = "Usuario desativado. Procure um administrador."
		}
		w.WriteHeader(http.StatusUnauthorized)
		h.renderLogin(w, r, msg, next)
		return
	}

	token, expires, err := h.Sessions.Create(r.Context(), user.ID, r.UserAgent(), clientIP(r))
	if err != nil {
		http.Error(w, "nao foi possivel iniciar a sessao", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   h.SecureCookie,
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, next, http.StatusSeeOther)
}

// HandleLogout revoga a sessao atual (se houver) e limpa o cookie.
func (h AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if token := middleware.SessionTokenFromContext(r.Context()); token != "" {
		_ = h.Sessions.Revoke(r.Context(), token)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.SecureCookie,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// APILogin processa POST /api/auth/login (JSON). Devolve um JWT bearer.
func (h AuthHandler) APILogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"data": nil, "message": "payload invalido", "errors": []string{err.Error()},
		})
		return
	}

	user, err := h.Users.Authenticate(r.Context(), body.Login, body.Password)
	if err != nil {
		status := http.StatusUnauthorized
		msg := "credenciais invalidas"
		if errors.Is(err, auth.ErrUserDisabled) {
			msg = "usuario desativado"
			status = http.StatusForbidden
		}
		writeJSON(w, status, map[string]any{
			"data": nil, "message": msg, "errors": []string{msg},
		})
		return
	}

	token, exp, err := h.JWT.Issue(user.ID, user.Nome, user.Papel)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"data": nil, "message": "nao foi possivel emitir token", "errors": []string{err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"token":       token,
			"token_type":  "Bearer",
			"expires_at":  exp.UTC().Format(time.RFC3339),
			"user": map[string]any{
				"id":          user.ID,
				"nome":        user.Nome,
				"email":       user.Email,
				"papel":       user.Papel,
				"permissions": user.Permissions,
			},
		},
		"message": "ok",
		"errors":  []string{},
	})
}

// APIMe retorna o usuario autenticado a partir do JWT atual.
func (h AuthHandler) APIMe(w http.ResponseWriter, r *http.Request) {
	u, ok := middleware.UserFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"data": nil, "message": "nao autenticado", "errors": []string{"nao autenticado"},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"id":          u.ID,
			"nome":        u.Nome,
			"email":       u.Email,
			"papel":       u.Papel,
			"permissions": u.Permissions,
		},
		"message": "ok",
		"errors":  []string{},
	})
}

func (h AuthHandler) renderLogin(w http.ResponseWriter, r *http.Request, errMsg, next string) {
	data := LoginPageData{
		Title:     "Entrar  tsure",
		Error:     errMsg,
		Next:      next,
		CSRFField: middleware.CSRFTemplateTag(r).(template.HTML),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.Templates.ExecuteTemplate(w, "login", data); err != nil {
		http.Error(w, "render login: "+err.Error(), http.StatusInternalServerError)
	}
}

// safeNext aceita apenas paths relativos para evitar open-redirect.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

// clientIP extrai o IP do cliente respeitando X-Forwarded-For quando
// presente. Para producao atras de proxy reverso, valide o proxy antes.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
