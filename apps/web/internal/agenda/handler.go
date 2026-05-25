package agenda

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"tsure/apps/web/internal/auth"
	"tsure/apps/web/internal/maps"
	"tsure/apps/web/internal/middleware"
	"tsure/apps/web/internal/render"
)

// loadRefs roda as 3 queries auxiliares em paralelo via errgroup.
// Reduz cold-path de ~3xRTT (serial) para ~1xRTT (paralelo).
func (h *Handler) loadRefs(r *http.Request, query string, withList bool) (items []Agendamento, clientes []ClienteRef, funcs []FuncionarioRef, err error) {
	g, ctx := errgroup.WithContext(r.Context())
	if withList {
		g.Go(func() error {
			var e error
			items, e = h.Store.List(ctx, query)
			return e
		})
	}
	g.Go(func() error {
		var e error
		clientes, e = h.Store.ListClientesRef(ctx)
		return e
	})
	g.Go(func() error {
		var e error
		funcs, e = h.Store.ListFuncionariosRef(ctx)
		return e
	})
	err = g.Wait()
	return
}

// Handler concentra dependencias HTTP para /agenda.
type Handler struct {
	Store     *Store
	Templates render.Executor
}

// NewHandler cria um Handler.
func NewHandler(store *Store, tmpl render.Executor) *Handler {
	return &Handler{Store: store, Templates: tmpl}
}

// PageData e o ViewModel da pagina /agenda.
type PageData struct {
	Title         string
	User          auth.User
	CSRFField     template.HTML
	Agendamento   Agendamento
	IsNew         bool
	Itens         []Agendamento
	Query         string
	Error         string
	Clientes      []ClienteRef
	ClienteAtual  ClienteRef // pre-carga via ?cliente=
	Funcionarios  []FuncionarioRef
	UFs           []maps.UF
	Cidades       []maps.Cidade
}

// FormData wrappa o formulario.
type FormData struct {
	CSRFField    template.HTML
	Agendamento  Agendamento
	IsNew        bool
	Error        string
	Clientes     []ClienteRef
	ClienteAtual ClienteRef
	Funcionarios []FuncionarioRef
	UFs          []maps.UF
	Cidades      []maps.Cidade
}

// RowsData wrappa a tabela.
type RowsData struct {
	Itens []Agendamento
	Query string
}

// ServeIndex: GET /agenda (pagina) e POST /agenda (criar).
func (h *Handler) ServeIndex(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.renderPage(w, r, "", emptyAgendamento(), true, "", parseClienteParam(r))
	case http.MethodPost:
		h.handleCreate(w, r)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// ServeByID despacha /agenda/{id}, /agenda/{id}/edit, /agenda/{id}/delete.
func (h *Handler) ServeByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/agenda/")
	rest = strings.Trim(rest, "/")
	if rest == "" || rest == "rows" || rest == "novo" || rest == "new" {
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
		h.handleUpdate(w, r, id)
	case sub == "" && r.Method == http.MethodGet:
		a, err := h.Store.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			h.serverError(w, err)
			return
		}
		c, _ := h.Store.GetClienteRef(r.Context(), a.ClienteID)
		h.renderPage(w, r, "", a, false, "", c)
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// ServeRows: GET /agenda/rows?q=
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
	h.render(w, "agenda-rows", RowsData{Itens: items, Query: q})
}

// ServeNew: GET /agenda/novo  formulario vazio (aceita ?cliente=).
func (h *Handler) ServeNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	clienteRef := parseClienteParam(r)
	if clienteRef.ID != uuid.Nil {
		c, _ := h.Store.GetClienteRef(r.Context(), clienteRef.ID)
		clienteRef = c
	}
	a := emptyAgendamento()
	if clienteRef.ID != uuid.Nil {
		a.ClienteID = clienteRef.ID
		a.ClienteNome = clienteRef.Nome
	}
	if r.Header.Get("HX-Request") == "true" {
		h.renderForm(w, r, a, true, "", clienteRef)
	} else {
		h.renderPage(w, r, "", a, true, "", clienteRef)
	}
}

func (h *Handler) handleEdit(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	a, err := h.Store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.serverError(w, err)
		return
	}
	c, _ := h.Store.GetClienteRef(r.Context(), a.ClienteID)
	h.renderForm(w, r, a, false, "", c)
}

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	a, err := parseForm(r)
	if err != nil {
		h.renderForm(w, r, a, true, err.Error(), ClienteRef{})
		return
	}
	user, _ := middleware.UserFromContext(r.Context())
	id, numero, err := h.Store.Save(r.Context(), a, &user.ID)
	if err != nil {
		h.renderForm(w, r, a, true, h.userMessage(err), ClienteRef{})
		return
	}
	a.ID = id
	a.Numero = numero
	c, _ := h.Store.GetClienteRef(r.Context(), a.ClienteID)
	h.respondAfterWrite(w, r, a, false, c)
}

func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	a, err := parseForm(r)
	if err != nil {
		a.ID = id
		h.renderForm(w, r, a, false, err.Error(), ClienteRef{})
		return
	}
	a.ID = id
	user, _ := middleware.UserFromContext(r.Context())
	_, _, err = h.Store.Save(r.Context(), a, &user.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.renderForm(w, r, a, false, h.userMessage(err), ClienteRef{})
		return
	}
	updated, _ := h.Store.Get(r.Context(), id)
	c, _ := h.Store.GetClienteRef(r.Context(), updated.ClienteID)
	h.respondAfterWrite(w, r, updated, false, c)
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if err := h.Store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		h.serverError(w, err)
		return
	}
	h.respondAfterWrite(w, r, emptyAgendamento(), true, ClienteRef{})
}

func (h *Handler) respondAfterWrite(w http.ResponseWriter, r *http.Request, a Agendamento, isNew bool, c ClienteRef) {
	items, clientes, funcionarios, err := h.loadRefs(r, "", true)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data := struct {
		FormData
		Itens []Agendamento
	}{
		FormData: FormData{
			CSRFField:    middleware.CSRFTemplateTag(r).(template.HTML),
			Agendamento:  a,
			IsNew:        isNew,
			Clientes:     clientes,
			ClienteAtual: c,
			Funcionarios: funcionarios,
			UFs:          maps.UFs,
			Cidades:      maps.CidadesMT,
		},
		Itens: items,
	}
	h.render(w, "agenda-form-with-rows", data)
}

func (h *Handler) renderPage(w http.ResponseWriter, r *http.Request, query string, a Agendamento, isNew bool, errMsg string, c ClienteRef) {
	items, clientes, funcionarios, err := h.loadRefs(r, query, true)
	if err != nil {
		h.serverError(w, err)
		return
	}
	user, _ := middleware.UserFromContext(r.Context())
	data := PageData{
		Title:        "Agendamento de Servicos  tsure",
		User:         user,
		CSRFField:    middleware.CSRFTemplateTag(r).(template.HTML),
		Agendamento:  a,
		IsNew:        isNew,
		Itens:        items,
		Query:        query,
		Error:        errMsg,
		Clientes:     clientes,
		ClienteAtual: c,
		Funcionarios: funcionarios,
		UFs:          maps.UFs,
		Cidades:      maps.CidadesMT,
	}
	h.render(w, "agenda", data)
}

func (h *Handler) renderForm(w http.ResponseWriter, r *http.Request, a Agendamento, isNew bool, errMsg string, c ClienteRef) {
	_, clientes, funcionarios, err := h.loadRefs(r, "", false)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data := FormData{
		CSRFField:    middleware.CSRFTemplateTag(r).(template.HTML),
		Agendamento:  a,
		IsNew:        isNew,
		Error:        errMsg,
		Clientes:     clientes,
		ClienteAtual: c,
		Funcionarios: funcionarios,
		UFs:          maps.UFs,
		Cidades:      maps.CidadesMT,
	}
	h.render(w, "agenda-form", data)
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

func (h *Handler) userMessage(err error) string {
	if errors.Is(err, ErrInvalidInput) {
		return strings.TrimPrefix(err.Error(), "dados invalidos: ")
	}
	return "Nao foi possivel salvar: " + err.Error()
}

func emptyAgendamento() Agendamento {
	return Agendamento{
		Status:     StatusOrcamento,
		TipoEvento: TipoParticular,
		Finalizado: false,
	}
}

func parseClienteParam(r *http.Request) ClienteRef {
	raw := r.URL.Query().Get("cliente")
	if raw == "" {
		return ClienteRef{}
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return ClienteRef{}
	}
	return ClienteRef{ID: id}
}

func parseForm(r *http.Request) (Agendamento, error) {
	if err := r.ParseForm(); err != nil {
		return Agendamento{}, errors.New("formulario invalido")
	}
	get := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }

	a := Agendamento{
		Status:               get("status"),
		TipoEvento:           get("tipo_evento"),
		DescricaoEvento:      get("descricao_evento"),
		HoraEvento:           get("hora_evento"),
		HoraInstalacao:       get("hora_instalacao"),
		FormaPagamento:       get("forma_pagamento"),
		NumeroAprovacao:      get("numero_aprovacao"),
		Observacoes:          get("observacoes"),
		Finalizado:           get("finalizado") == "on" || get("finalizado") == "true" || get("finalizado") == "1",
		LocalLogradouro:      get("local_logradouro"),
		LocalNumero:          get("local_numero"),
		LocalComplemento:     get("local_complemento"),
		LocalBairro:          get("local_bairro"),
		LocalCidade:          get("local_cidade"),
		LocalUF:              get("local_uf"),
		Responsavel1Nome:     get("responsavel1_nome"),
		Responsavel1Telefone: get("responsavel1_telefone"),
		Responsavel2Nome:     get("responsavel2_nome"),
		Responsavel2Telefone: get("responsavel2_telefone"),
	}

	if id, err := uuid.Parse(get("cliente_id")); err == nil {
		a.ClienteID = id
	}
	if raw := get("quem_contratou_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			a.QuemContratouID = &id
		}
	}
	if v := get("valor_total"); v != "" {
		f, _ := strconv.ParseFloat(strings.Replace(v, ",", ".", 1), 64)
		a.ValorTotal = f
	}
	if d := parseDate(get("data_evento")); d != nil {
		a.DataEvento = d
	}
	if d := parseDate(get("data_instalacao")); d != nil {
		a.DataInstalacao = d
	}
	if d := parseDate(get("data_retorno_prevista")); d != nil {
		a.DataRetornoPrevista = d
	}
	if d := parseDate(get("data_aprovacao")); d != nil {
		a.DataAprovacao = d
	}
	return a, nil
}

func parseDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t
		}
	}
	return nil
}
