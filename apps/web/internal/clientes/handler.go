package clientes

import (
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"tsure/apps/web/internal/auth"
	"tsure/apps/web/internal/maps"
	"tsure/apps/web/internal/middleware"
	"tsure/apps/web/internal/render"
)

// Handler aglutina dependencias HTTP para a tela de Clientes.
type Handler struct {
	Store     *Store
	Templates render.Executor
}

// NewHandler constroi um Handler.
func NewHandler(store *Store, tmpl render.Executor) *Handler {
	return &Handler{Store: store, Templates: tmpl}
}

// PageData e o ViewModel da pagina /clientes.
type PageData struct {
	Title     string
	User      auth.User
	CSRFField template.HTML
	Cliente   Cliente
	IsNew     bool
	Clientes  []Cliente
	Query     string
	Error     string
	UFs       []maps.UF
	Cidades   []maps.Cidade
}

// PageDataForm e o subset usado no parcial do formulario.
type FormData struct {
	CSRFField template.HTML
	Cliente   Cliente
	IsNew     bool
	Error     string
	UFs       []maps.UF
	Cidades   []maps.Cidade
}

// RowsData e o subset usado no parcial da tabela (tbody + linhas).
type RowsData struct {
	Clientes []Cliente
	Query    string
}

// ServeIndex: GET /clientes  pagina completa; POST /clientes  cria.
// Opcionalmente aceita ?id= para abrir um cliente especifico no form.
func (h *Handler) ServeIndex(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.renderPage(w, r, "", Cliente{}, true, "")
	case http.MethodPost:
		// POST /clientes  cria novo cliente
		h.handleCreate(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// ServeByID despacha sob /clientes/{id}, /clientes/{id}/edit e
// /clientes/{id}/delete. Decisao pelo path + verbo HTTP.
func (h *Handler) ServeByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/clientes/")
	rest = strings.Trim(rest, "/")
	if rest == "" || rest == "rows" || rest == "new" {
		http.NotFound(w, r)
		return
	}

	parts := strings.SplitN(rest, "/", 2)
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch {
	case sub == "edit" && r.Method == http.MethodGet:
		h.handleEdit(w, r, id)
	case sub == "delete" && r.Method == http.MethodPost:
		h.handleDelete(w, r, id)
	case sub == "" && r.Method == http.MethodPost:
		// POST /clientes/{id}  update
		h.handleUpdate(w, r, id)
	case sub == "" && r.Method == http.MethodGet:
		// GET /clientes/{id}  renderiza pagina ja com o cliente carregado
		c, err := h.Store.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			h.serverError(w, err)
			return
		}
		h.renderPage(w, r, "", c, false, "")
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// ServeRows: GET /clientes/rows?q=  retorna so o tbody (HTMX).
func (h *Handler) ServeRows(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query().Get("q")
	items, err := h.Store.List(r.Context(), q)
	if err != nil {
		h.serverError(w, err)
		return
	}
	h.render(w, "clientes-rows", RowsData{Clientes: items, Query: q})
}

// ServeNew: GET /clientes/new  formulario vazio (HTMX).
func (h *Handler) ServeNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	h.renderForm(w, r, Cliente{Tipo: TipoPJ}, true, "")
}

// handleEdit: GET /clientes/{id}/edit  formulario preenchido (HTMX).
func (h *Handler) handleEdit(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	c, err := h.Store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.serverError(w, err)
		return
	}
	h.renderForm(w, r, c, false, "")
}

// handleCreate processa POST /clientes (multipart/form-encoded).
func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	c, err := parseForm(r)
	if err != nil {
		h.renderForm(w, r, c, true, err.Error())
		return
	}
	id, err := h.Store.Create(r.Context(), c)
	if err != nil {
		h.renderForm(w, r, c, true, h.userMessage(err))
		return
	}
	c.ID = id
	h.respondAfterWrite(w, r, c, false)
}

// handleUpdate processa POST /clientes/{id}.
func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	c, err := parseForm(r)
	if err != nil {
		c.ID = id
		h.renderForm(w, r, c, false, err.Error())
		return
	}
	if err := h.Store.Update(r.Context(), id, c); err != nil {
		c.ID = id
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.renderForm(w, r, c, false, h.userMessage(err))
		return
	}
	c.ID = id
	h.respondAfterWrite(w, r, c, false)
}

// handleDelete processa POST /clientes/{id}/delete (soft-delete).
func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if err := h.Store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.serverError(w, err)
		return
	}
	// Retorna lista atualizada + formulario zerado (HTMX OOB)
	h.respondAfterWrite(w, r, Cliente{Tipo: TipoPJ}, true)
}

// respondAfterWrite renderiza o formulario do cliente atual + a tabela
// (out-of-band swap) para que HTMX atualize as duas areas em uma resposta.
func (h *Handler) respondAfterWrite(w http.ResponseWriter, r *http.Request, c Cliente, isNew bool) {
	items, err := h.Store.List(r.Context(), "")
	if err != nil {
		h.serverError(w, err)
		return
	}
	csrf := middleware.CSRFTemplateTag(r).(template.HTML)
	data := struct {
		FormData
		Clientes []Cliente
	}{
		FormData: FormData{
			CSRFField: csrf,
			Cliente:   c,
			IsNew:     isNew,
			UFs:       maps.UFs,
			Cidades:   maps.CidadesMT,
		},
		Clientes: items,
	}
	h.render(w, "clientes-form-with-rows", data)
}

// renderPage devolve a pagina completa /clientes.
func (h *Handler) renderPage(w http.ResponseWriter, r *http.Request, query string, c Cliente, isNew bool, errMsg string) {
	if isNew && c.Tipo == "" {
		c.Tipo = TipoPJ
	}
	items, err := h.Store.List(r.Context(), query)
	if err != nil {
		h.serverError(w, err)
		return
	}
	user, _ := middleware.UserFromContext(r.Context())
	data := PageData{
		Title:     "Clientes  tsure",
		User:      user,
		CSRFField: middleware.CSRFTemplateTag(r).(template.HTML),
		Cliente:   c,
		IsNew:     isNew,
		Clientes:  items,
		Query:     query,
		Error:     errMsg,
		UFs:       maps.UFs,
		Cidades:   maps.CidadesMT,
	}
	h.render(w, "clientes", data)
}

func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, c Cliente, isNew bool, errMsg string) {
	data := FormData{
		CSRFField: middleware.CSRFTemplateTag(r).(template.HTML),
		Cliente:   c,
		IsNew:     isNew,
		Error:     errMsg,
		UFs:       maps.UFs,
		Cidades:   maps.CidadesMT,
	}
	h.render(w, "clientes-form", data)
}

func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.Templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) serverError(w http.ResponseWriter, err error) {
	http.Error(w, "erro interno: "+err.Error(), http.StatusInternalServerError)
}

// userMessage converte um erro do store em mensagem amigavel.
func (h *Handler) userMessage(err error) string {
	switch {
	case errors.Is(err, ErrDuplicateDoc):
		return "Documento ja cadastrado para outro cliente."
	case errors.Is(err, ErrInvalidInput):
		return strings.TrimPrefix(err.Error(), "dados invalidos: ")
	default:
		return "Nao foi possivel salvar: " + err.Error()
	}
}

// parseForm extrai um Cliente do formulario POST.
func parseForm(r *http.Request) (Cliente, error) {
	if err := r.ParseForm(); err != nil {
		return Cliente{}, errors.New("formulario invalido")
	}
	get := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }

	c := Cliente{
		Tipo:            get("tipo"),
		NomeRazaoSocial: get("nome_razao_social"),
		Documento:       get("documento"),
		Email:           get("email"),
		TelefoneFixo:    get("telefone_fixo"),
		TelefoneCelular: get("telefone_celular"),
		ContatoCliente:  get("contato_cliente"),
		Logradouro:      get("logradouro"),
		Numero:          get("numero"),
		Complemento:     get("complemento"),
		Bairro:          get("bairro"),
		Cidade:          get("cidade"),
		UF:              get("uf"),
		CEP:             get("cep"),
		Bloqueado:       get("bloqueado") == "on" || get("bloqueado") == "true" || get("bloqueado") == "1",
		MotivoBloqueio:  get("motivo_bloqueio"),
		Observacoes:     get("observacoes"),
	}
	return c, nil
}
