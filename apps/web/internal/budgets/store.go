package budgets

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Budget struct {
	ID           int64
	Code         string
	CustomerName string
	EventName    string
	EventCity    string
	EventDate    time.Time
	InstallDate  *time.Time
	ReturnDate   *time.Time
	Status       string
	CrewSize     int
	VehicleLabel string
	TotalAmount  float64
	ValidUntil   *time.Time
	Notes        string
	CreatedAt    time.Time
}

type BudgetItem struct {
	ID            int64
	BudgetID      int64
	EquipmentID   *int64
	KitID         *int64
	Quantity      int
	UnitPrice     float64
	TotalPrice    float64
	EquipmentCode string
	EquipmentName string
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Init(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, schema); err != nil {
		return fmt.Errorf("create budgets schema: %w", err)
	}
	return nil
}

func (s *Store) List(ctx context.Context, status string) ([]Budget, error) {
	query := `
		SELECT id, code, customer_name, event_name, event_city, event_date, install_date, return_date, status, crew_size, vehicle_label, total_amount, valid_until, notes, created_at
		FROM budgets
	`
	args := []any{}
	if status != "" {
		query += `WHERE status = $1 `
		args = append(args, status)
	}
	query += `ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list budgets: %w", err)
	}
	defer rows.Close()

	var items []Budget
	for rows.Next() {
		var b Budget
		if err := rows.Scan(&b.ID, &b.Code, &b.CustomerName, &b.EventName, &b.EventCity, &b.EventDate, &b.InstallDate, &b.ReturnDate, &b.Status, &b.CrewSize, &b.VehicleLabel, &b.TotalAmount, &b.ValidUntil, &b.Notes, &b.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan budget: %w", err)
		}
		items = append(items, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate budgets: %w", err)
	}
	return items, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Budget, error) {
	var b Budget
	err := s.pool.QueryRow(ctx, `
		SELECT id, code, customer_name, event_name, event_city, event_date, install_date, return_date, status, crew_size, vehicle_label, total_amount, valid_until, notes, created_at
		FROM budgets WHERE id = $1
	`, id).Scan(&b.ID, &b.Code, &b.CustomerName, &b.EventName, &b.EventCity, &b.EventDate, &b.InstallDate, &b.ReturnDate, &b.Status, &b.CrewSize, &b.VehicleLabel, &b.TotalAmount, &b.ValidUntil, &b.Notes, &b.CreatedAt)
	if err != nil {
		return b, fmt.Errorf("get budget: %w", err)
	}
	return b, nil
}

func (s *Store) Create(ctx context.Context, code, customerName, eventName, eventCity, eventDateRaw, installDateRaw, returnDateRaw, vehicleLabel, notes string, crewSize int, validUntilRaw string) (int64, error) {
	customerName = trim(customerName)
	eventName = trim(eventName)
	eventCity = trim(eventCity)
	eventDateRaw = trim(eventDateRaw)
	vehicleLabel = trim(vehicleLabel)

	if customerName == "" || eventName == "" || eventCity == "" {
		return 0, fmt.Errorf("cliente, evento e cidade sao obrigatorios")
	}

	eventDate, err := time.Parse("2006-01-02", eventDateRaw)
	if err != nil {
		return 0, fmt.Errorf("data do evento invalida")
	}

	var installDate, returnDate, validUntil *time.Time
	if d, err := time.Parse("2006-01-02", trim(installDateRaw)); err == nil {
		installDate = &d
	}
	if d, err := time.Parse("2006-01-02", trim(returnDateRaw)); err == nil {
		returnDate = &d
	}
	if d, err := time.Parse("2006-01-02", trim(validUntilRaw)); err == nil {
		validUntil = &d
	}

	var id int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO budgets (code, customer_name, event_name, event_city, event_date, install_date, return_date, crew_size, vehicle_label, valid_until, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, code, customerName, eventName, eventCity, eventDate, installDate, returnDate, crewSize, vehicleLabel, validUntil, notes).Scan(&id); err != nil {
		return 0, fmt.Errorf("create budget: %w", err)
	}
	return id, nil
}

func (s *Store) UpdateStatus(ctx context.Context, id int64, status string) error {
	_, err := s.pool.Exec(ctx, `UPDATE budgets SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	if err != nil {
		return fmt.Errorf("update budget status: %w", err)
	}
	return nil
}

func (s *Store) AddItem(ctx context.Context, budgetID int64, equipmentID, kitID *int64, qty int, unitPrice float64) error {
	if qty <= 0 {
		return fmt.Errorf("quantidade deve ser maior que zero")
	}
	total := float64(qty) * unitPrice
	_, err := s.pool.Exec(ctx, `
		INSERT INTO budget_items (budget_id, equipment_id, kit_id, quantity, unit_price, total_price)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, budgetID, equipmentID, kitID, qty, unitPrice, total)
	if err != nil {
		return fmt.Errorf("add budget item: %w", err)
	}
	return nil
}

func (s *Store) GetItems(ctx context.Context, budgetID int64) ([]BudgetItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT bi.id, bi.budget_id, bi.equipment_id, bi.kit_id, bi.quantity, bi.unit_price, bi.total_price, COALESCE(e.code, ''), COALESCE(e.name, '')
		FROM budget_items bi
		LEFT JOIN equipment e ON e.id = bi.equipment_id
		WHERE bi.budget_id = $1
		ORDER BY bi.id
	`, budgetID)
	if err != nil {
		return nil, fmt.Errorf("get budget items: %w", err)
	}
	defer rows.Close()

	var items []BudgetItem
	for rows.Next() {
		var bi BudgetItem
		if err := rows.Scan(&bi.ID, &bi.BudgetID, &bi.EquipmentID, &bi.KitID, &bi.Quantity, &bi.UnitPrice, &bi.TotalPrice, &bi.EquipmentCode, &bi.EquipmentName); err != nil {
			return nil, fmt.Errorf("scan budget item: %w", err)
		}
		items = append(items, bi)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate budget items: %w", err)
	}
	return items, nil
}

func (s *Store) UpdateTotal(ctx context.Context, budgetID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE budgets SET total_amount = COALESCE((SELECT SUM(total_price) FROM budget_items WHERE budget_id = $1), 0) WHERE id = $1
	`, budgetID)
	if err != nil {
		return fmt.Errorf("update budget total: %w", err)
	}
	return nil
}

func (s *Store) NextCode(ctx context.Context, year int) (string, error) {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM budgets WHERE code LIKE $1`, fmt.Sprintf("ORC-%d-%%", year)).Scan(&count); err != nil {
		return "", err
	}
	return fmt.Sprintf("ORC-%d-%03d", year, count+1), nil
}

func trim(s string) string {
	return strings.TrimSpace(s)
}
