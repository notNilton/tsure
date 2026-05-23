package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"tsure/apps/web/internal/auth"
	"tsure/apps/web/internal/budgets"
	"tsure/apps/web/internal/clientes"
	"tsure/apps/web/internal/handlers"
	"tsure/apps/web/internal/inventory"
	"tsure/apps/web/internal/middleware"
	"tsure/apps/web/internal/orders"
	"tsure/apps/web/internal/render"
)

//go:embed templates/*.html templates/partials/*.html public/*
var embeddedAssets embed.FS

type app struct {
	store     *orders.Store
	templates render.Executor
}

type pageData struct {
	Title     string
	Today     string
	User      auth.User
	CSRFField template.HTML
}

func main() {
	loadDotEnv()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg := configFromEnv()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping postgres: %v", err)
	}

	// Stores existentes (legado da v0; coexistem com o novo schema).
	ordersStore := orders.NewStore(pool)
	if err := ordersStore.Init(ctx); err != nil {
		log.Fatalf("init orders store: %v", err)
	}
	budgetsStore := budgets.NewStore(pool)
	if err := budgetsStore.Init(ctx); err != nil {
		log.Fatalf("init budgets store: %v", err)
	}
	inventoryStore := inventory.NewStore(pool)
	if err := inventoryStore.Init(ctx); err != nil {
		log.Fatalf("init inventory store: %v", err)
	}

	// Auth: usuarios + sessoes web + JWT api.
	usersStore := auth.NewStore(pool)
	if err := usersStore.BootstrapAdmin(ctx); err != nil {
		log.Fatalf("bootstrap admin: %v", err)
	}
	sessionStore := auth.NewSessionStore(pool, cfg.SessionTTL)
	jwtSigner := auth.NewJWTSigner(cfg.JWTKey, cfg.JWTTTL)

	funcs := template.FuncMap{
		"formatMoney":     formatMoneyBRL,
		"formatDate":      formatDateBR,
		"statusLabel":     orders.StatusLabel,
		"nextStatusLabel": orders.NextStatusLabel,
		"rowsData": func(items []clientes.Cliente, query string) clientes.RowsData {
			return clientes.RowsData{Clientes: items, Query: query}
		},
	}

	var tmpl render.Executor
	if cfg.DevMode {
		// Dev: le do disco a cada render. Hot-reload de .html sem rebuild.
		reloader, err := render.NewReloader(
			"apps/web/templates",
			[]string{"*.html", "partials/*.html"},
			funcs,
		)
		if err != nil {
			log.Fatalf("init template reloader: %v", err)
		}
		log.Printf("hot-reload de templates ATIVO (TSURE_ENV=dev)")
		tmpl = reloader
	} else {
		t, err := render.LoadEmbedded(
			embeddedAssets,
			[]string{"templates/*.html", "templates/partials/*.html"},
			funcs,
		)
		if err != nil {
			log.Fatalf("load embedded templates: %v", err)
		}
		tmpl = t
	}

	clientesStore := clientes.NewStore(pool)
	clientesHandler := clientes.NewHandler(clientesStore, tmpl)

	a := &app{store: ordersStore, templates: tmpl}

	webAuth := middleware.WebAuth{Users: usersStore, Sessions: sessionStore}
	apiAuth := middleware.APIAuth{Users: usersStore, Signer: jwtSigner}
	authHandler := handlers.AuthHandler{
		Users:        usersStore,
		Sessions:     sessionStore,
		JWT:          jwtSigner,
		SecureCookie: cfg.SecureCookie,
		Templates:    tmpl,
	}

	mux := http.NewServeMux()

	// ---- Rotas publicas (web, sem auth, mas com CSRF)
	mux.Handle("/login", webAuth.Optional(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			authHandler.ShowLogin(w, r)
		case http.MethodPost:
			authHandler.HandleLogin(w, r)
		default:
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
	})))

	// ---- Rotas web protegidas
	mux.Handle("/logout", webAuth.Required(http.HandlerFunc(authHandler.HandleLogout)))
	mux.Handle("/", webAuth.Required(http.HandlerFunc(a.handleIndex)))
	mux.Handle("/orders", webAuth.Required(
		middleware.RequireAnyPermission("agenda.read", "agenda.write")(
			http.HandlerFunc(a.handleOrders),
		),
	))
	mux.Handle("/orders/", webAuth.Required(
		middleware.RequireAnyPermission("agenda.read", "agenda.write")(
			http.HandlerFunc(a.handleOrderByID),
		),
	))

	// ---- Clientes (SSR + HTMX)
	clientesRead := middleware.RequireAnyPermission("clientes.read", "clientes.write")
	clientesWrite := middleware.RequirePermission("clientes.write")
	mux.Handle("/clientes", webAuth.Required(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			clientesWrite(http.HandlerFunc(clientesHandler.ServeIndex)).ServeHTTP(w, r)
			return
		}
		clientesRead(http.HandlerFunc(clientesHandler.ServeIndex)).ServeHTTP(w, r)
	})))
	mux.Handle("/clientes/rows", webAuth.Required(clientesRead(http.HandlerFunc(clientesHandler.ServeRows))))
	mux.Handle("/clientes/new", webAuth.Required(clientesWrite(http.HandlerFunc(clientesHandler.ServeNew))))
	mux.Handle("/clientes/", webAuth.Required(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			clientesWrite(http.HandlerFunc(clientesHandler.ServeByID)).ServeHTTP(w, r)
			return
		}
		clientesRead(http.HandlerFunc(clientesHandler.ServeByID)).ServeHTTP(w, r)
	})))

	// ---- API JSON (mobile)  JWT bearer, sem CSRF
	mux.Handle("/api/auth/login", http.HandlerFunc(authHandler.APILogin))
	mux.Handle("/api/auth/me", apiAuth.Required(http.HandlerFunc(authHandler.APIMe)))

	// ---- Static
	staticFS, err := fs.Sub(embeddedAssets, "public")
	if err != nil {
		log.Fatalf("load static assets: %v", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Cadeia de middlewares globais: recover -> log -> csrf (skip /api) -> mux
	csrfMW := middleware.SkipAPICSRF(middleware.NewCSRF(middleware.CSRFConfig{
		Key:    cfg.CSRFKey,
		Secure: cfg.SecureCookie,
	}))
	handler := middleware.Recover(logRequests(csrfMW(mux)))

	srv := &http.Server{
		Addr:         cfg.Addr,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("listening on http://%s", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve: %v", err)
	}
}

type config struct {
	Addr         string
	DatabaseURL  string
	CSRFKey      []byte
	JWTKey       []byte
	SessionTTL   time.Duration
	JWTTTL       time.Duration
	SecureCookie bool
	DevMode      bool
}

// loadDotEnv procura um .env subindo do CWD ate 4 niveis e carrega o
// primeiro encontrado. NAO sobrescreve variaveis ja definidas no ambiente.
// Silencioso quando nao acha o arquivo.
func loadDotEnv() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	dir := cwd
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, ".env")
		if _, err := os.Stat(path); err == nil {
			if err := godotenv.Load(path); err != nil {
				log.Printf(".env encontrado mas falhou ao carregar (%s): %v", path, err)
				return
			}
			log.Printf(".env carregado: %s", path)
			return
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}

func configFromEnv() config {
	addr := strings.TrimSpace(os.Getenv("ADDR"))
	if addr == "" {
		addr = "127.0.0.1:3456"
	}
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		dbURL = "postgres://tsure:tsure@127.0.0.1:5432/tsure?sslmode=disable"
	}
	env := strings.ToLower(strings.TrimSpace(os.Getenv("TSURE_ENV")))
	if env == "" {
		env = "dev"
	}
	isProd := env == "prod" || env == "production"

	return config{
		Addr:         addr,
		DatabaseURL:  dbURL,
		CSRFKey:      deriveSecret("CSRF_SECRET", 32),
		JWTKey:       deriveSecret("JWT_SECRET", 32),
		SessionTTL:   12 * time.Hour,
		JWTTTL:       8 * time.Hour,
		SecureCookie: isProd,
		DevMode:      !isProd,
	}
}

// deriveSecret le um segredo do ambiente e o expande para o tamanho fixo
// (SHA-256). Em dev, se a variavel nao existir, gera um aleatorio com
// aviso  trocar entre reinicios invalida sessoes/JWTs.
func deriveSecret(name string, size int) []byte {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		b := make([]byte, size)
		if _, err := rand.Read(b); err != nil {
			log.Fatalf("gerar segredo %s: %v", name, err)
		}
		sum := sha256.Sum256(b)
		log.Printf("AVISO: %s nao definido; gerado em memoria (hash=%s). Defina em producao.", name, hex.EncodeToString(sum[:6]))
		return b
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:size]
}

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	user, _ := middleware.UserFromContext(r.Context())
	data := pageData{
		Title:     "tsure | ERP de locacoes",
		Today:     time.Now().Format("02 Jan 2006"),
		User:      user,
		CSRFField: middleware.CSRFTemplateTag(r).(template.HTML),
	}
	a.render(w, "dashboard", data)
}

func (a *app) handleOrders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.renderOrdersPanel(w, r)
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "form invalido", http.StatusBadRequest)
			return
		}
		crewSize, err := strconv.Atoi(strings.TrimSpace(r.FormValue("crewSize")))
		if err != nil {
			http.Error(w, "equipe invalida", http.StatusBadRequest)
			return
		}
		if _, err := a.store.Create(
			r.Context(),
			r.FormValue("customerName"),
			r.FormValue("eventName"),
			r.FormValue("eventCity"),
			r.FormValue("eventDate"),
			r.FormValue("installDate"),
			r.FormValue("returnDate"),
			r.FormValue("vehicleLabel"),
			crewSize,
		); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		a.renderOrdersPanel(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *app) handleOrderByID(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/orders/"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		if _, err := a.store.AdvanceStatus(r.Context(), id); err != nil {
			if errors.Is(err, orders.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			a.internalError(w, err)
			return
		}
		a.renderOrdersPanel(w, r)
	case http.MethodDelete:
		if err := a.store.Delete(r.Context(), id); err != nil {
			if errors.Is(err, orders.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			a.internalError(w, err)
			return
		}
		a.renderOrdersPanel(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (a *app) renderOrdersPanel(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.List(r.Context(), "")
	if err != nil {
		a.internalError(w, err)
		return
	}
	a.render(w, "orders-panel", map[string]any{
		"Orders":  items,
		"Summary": orders.BuildSummary(items),
	})
}

func (a *app) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		a.internalError(w, err)
	}
}

func (a *app) internalError(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func parseID(raw string) (int64, error) {
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return 0, fmt.Errorf("missing id")
	}
	id, err := strconv.ParseInt(path.Base(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id")
	}
	return id, nil
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func formatDateBR(value time.Time) string { return value.Format("02/01/2006") }

func formatMoneyBRL(value float64) string {
	reais := int64(math.Round(value * 100))
	inteiro := reais / 100
	centavos := reais % 100
	if centavos < 0 {
		centavos = -centavos
	}
	intPart := strconv.FormatInt(inteiro, 10)
	if len(intPart) > 3 {
		var groups []string
		for len(intPart) > 3 {
			groups = append([]string{intPart[len(intPart)-3:]}, groups...)
			intPart = intPart[:len(intPart)-3]
		}
		groups = append([]string{intPart}, groups...)
		intPart = strings.Join(groups, ".")
	}
	return fmt.Sprintf("R$ %s,%02d", intPart, centavos)
}
