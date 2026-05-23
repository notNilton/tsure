// Package clientes implementa o dominio comercial de clientes do ERP:
// CRUD sobre a tabela clientes (PF/PJ), busca por nome ou documento e
// soft-delete via deleted_at.
package clientes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Erros canonicos do dominio.
var (
	ErrNotFound       = errors.New("cliente nao encontrado")
	ErrDuplicateDoc   = errors.New("documento ja cadastrado")
	ErrInvalidInput   = errors.New("dados invalidos")
)

// Tipos validos espelhando o enum cliente_tipo do banco.
const (
	TipoPF = "pessoa_fisica"
	TipoPJ = "pessoa_juridica"
)

// Cliente representa uma linha da tabela clientes.
type Cliente struct {
	ID              uuid.UUID
	Tipo            string
	NomeRazaoSocial string
	Documento       string
	Email           string
	TelefoneFixo    string
	TelefoneCelular string
	ContatoCliente  string
	Logradouro      string
	Numero          string
	Complemento     string
	Bairro          string
	Cidade          string
	UF              string
	CEP             string
	Bloqueado       bool
	MotivoBloqueio  string
	Observacoes     string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TipoLabel devolve o rotulo humano do tipo do cliente.
func (c Cliente) TipoLabel() string {
	switch c.Tipo {
	case TipoPF:
		return "Fisica"
	case TipoPJ:
		return "Juridica"
	default:
		return c.Tipo
	}
}

// DocumentoFormatado retorna o documento com mascara para exibicao.
func (c Cliente) DocumentoFormatado() string {
	return FormatDocumento(c.Tipo, c.Documento)
}

// Store concentra as operacoes contra a tabela clientes.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore cria um Store sobre o pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// List devolve clientes filtrados por nome ou documento (case-insensitive),
// limitados a 200 registros, ordenados por nome.
func (s *Store) List(ctx context.Context, query string) ([]Cliente, error) {
	q := strings.TrimSpace(query)
	args := []any{}
	where := "WHERE deleted_at IS NULL"
	if q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%", normalizeDigits(q))
		where += " AND (lower(nome_razao_social) LIKE $1 OR documento LIKE $2 || '%')"
	}

	sql := `
		SELECT id, tipo::text, nome_razao_social, documento,
		       COALESCE(email::text, ''), COALESCE(telefone_fixo, ''), COALESCE(telefone_celular, ''),
		       COALESCE(contato_cliente, ''), COALESCE(logradouro, ''), COALESCE(numero, ''),
		       COALESCE(complemento, ''), COALESCE(bairro, ''), COALESCE(cidade, ''),
		       COALESCE(uf, ''), COALESCE(cep, ''),
		       bloqueado, COALESCE(motivo_bloqueio, ''), COALESCE(observacoes, ''),
		       created_at, updated_at
		FROM clientes
		` + where + `
		ORDER BY nome_razao_social ASC
		LIMIT 200
	`

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("listar clientes: %w", err)
	}
	defer rows.Close()

	out := make([]Cliente, 0, 32)
	for rows.Next() {
		c, err := scanCliente(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Get busca um cliente por id; devolve ErrNotFound se nao existir.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (Cliente, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tipo::text, nome_razao_social, documento,
		       COALESCE(email::text, ''), COALESCE(telefone_fixo, ''), COALESCE(telefone_celular, ''),
		       COALESCE(contato_cliente, ''), COALESCE(logradouro, ''), COALESCE(numero, ''),
		       COALESCE(complemento, ''), COALESCE(bairro, ''), COALESCE(cidade, ''),
		       COALESCE(uf, ''), COALESCE(cep, ''),
		       bloqueado, COALESCE(motivo_bloqueio, ''), COALESCE(observacoes, ''),
		       created_at, updated_at
		FROM clientes
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	c, err := scanCliente(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Cliente{}, ErrNotFound
		}
		return Cliente{}, fmt.Errorf("buscar cliente: %w", err)
	}
	return c, nil
}

// Create insere um novo cliente e devolve o id gerado.
func (s *Store) Create(ctx context.Context, c Cliente) (uuid.UUID, error) {
	if err := c.Validate(); err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		INSERT INTO clientes (
			tipo, nome_razao_social, documento, email,
			telefone_fixo, telefone_celular, contato_cliente,
			logradouro, numero, complemento, bairro, cidade, uf, cep,
			bloqueado, motivo_bloqueio, observacoes
		) VALUES (
			$1::cliente_tipo, $2, $3, NULLIF($4, ''),
			$5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14,
			$15, $16, $17
		)
		RETURNING id
	`,
		c.Tipo, c.NomeRazaoSocial, normalizeDigits(c.Documento), c.Email,
		c.TelefoneFixo, c.TelefoneCelular, c.ContatoCliente,
		c.Logradouro, c.Numero, c.Complemento, c.Bairro, c.Cidade, normalizeUF(c.UF), c.CEP,
		c.Bloqueado, c.MotivoBloqueio, c.Observacoes,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return uuid.Nil, ErrDuplicateDoc
		}
		return uuid.Nil, fmt.Errorf("inserir cliente: %w", err)
	}
	return id, nil
}

// Update sobrescreve um cliente existente. Retorna ErrNotFound se id ausente.
func (s *Store) Update(ctx context.Context, id uuid.UUID, c Cliente) error {
	if err := c.Validate(); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE clientes SET
			tipo = $2::cliente_tipo,
			nome_razao_social = $3,
			documento = $4,
			email = NULLIF($5, ''),
			telefone_fixo = $6,
			telefone_celular = $7,
			contato_cliente = $8,
			logradouro = $9,
			numero = $10,
			complemento = $11,
			bairro = $12,
			cidade = $13,
			uf = $14,
			cep = $15,
			bloqueado = $16,
			motivo_bloqueio = $17,
			observacoes = $18
		WHERE id = $1 AND deleted_at IS NULL
	`,
		id,
		c.Tipo, c.NomeRazaoSocial, normalizeDigits(c.Documento), c.Email,
		c.TelefoneFixo, c.TelefoneCelular, c.ContatoCliente,
		c.Logradouro, c.Numero, c.Complemento, c.Bairro, c.Cidade, normalizeUF(c.UF), c.CEP,
		c.Bloqueado, c.MotivoBloqueio, c.Observacoes,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicateDoc
		}
		return fmt.Errorf("atualizar cliente: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete marca o cliente como excluido (soft-delete).
func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE clientes SET deleted_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
	if err != nil {
		return fmt.Errorf("remover cliente: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Validate confere as regras minimas antes de gravar.
func (c Cliente) Validate() error {
	if c.Tipo != TipoPF && c.Tipo != TipoPJ {
		return fmt.Errorf("%w: tipo invalido", ErrInvalidInput)
	}
	if strings.TrimSpace(c.NomeRazaoSocial) == "" {
		return fmt.Errorf("%w: razao social/nome obrigatorio", ErrInvalidInput)
	}
	doc := normalizeDigits(c.Documento)
	if c.Tipo == TipoPF && len(doc) != 11 {
		return fmt.Errorf("%w: CPF deve ter 11 digitos", ErrInvalidInput)
	}
	if c.Tipo == TipoPJ && len(doc) != 14 {
		return fmt.Errorf("%w: CNPJ deve ter 14 digitos", ErrInvalidInput)
	}
	if c.Bloqueado && strings.TrimSpace(c.MotivoBloqueio) == "" {
		return fmt.Errorf("%w: motivo do bloqueio obrigatorio", ErrInvalidInput)
	}
	return nil
}

// rowScanner aceita pgx.Row ou pgx.Rows (ambos implementam Scan).
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCliente(r rowScanner) (Cliente, error) {
	var c Cliente
	err := r.Scan(
		&c.ID, &c.Tipo, &c.NomeRazaoSocial, &c.Documento,
		&c.Email, &c.TelefoneFixo, &c.TelefoneCelular,
		&c.ContatoCliente, &c.Logradouro, &c.Numero,
		&c.Complemento, &c.Bairro, &c.Cidade,
		&c.UF, &c.CEP,
		&c.Bloqueado, &c.MotivoBloqueio, &c.Observacoes,
		&c.CreatedAt, &c.UpdatedAt,
	)
	return c, err
}

// normalizeDigits remove tudo que nao e digito.
func normalizeDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeUF(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) > 2 {
		s = s[:2]
	}
	return s
}

// FormatDocumento devolve o documento (so digitos) com mascara de exibicao.
func FormatDocumento(tipo, doc string) string {
	d := normalizeDigits(doc)
	switch tipo {
	case TipoPF:
		if len(d) != 11 {
			return d
		}
		return d[0:3] + "." + d[3:6] + "." + d[6:9] + "-" + d[9:11]
	case TipoPJ:
		if len(d) != 14 {
			return d
		}
		return d[0:2] + "." + d[2:5] + "." + d[5:8] + "/" + d[8:12] + "-" + d[12:14]
	default:
		return d
	}
}

// isUniqueViolation reconhece o codigo SQLSTATE 23505 (unique_violation)
// devolvido pelo pgx quando o documento ja existe.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type pgErr interface {
		SQLState() string
	}
	if pe, ok := err.(pgErr); ok {
		return pe.SQLState() == "23505"
	}
	return false
}
