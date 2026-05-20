package orders

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("service order not found")

var statusFlow = []string{
	"orcamento",
	"agendado",
	"em_execucao",
	"aguardando_retorno",
	"finalizado",
}

type ServiceOrder struct {
	ID               int64
	Code             string
	CustomerName     string
	EventName        string
	EventCity        string
	EventDate        time.Time
	InstallDate      *time.Time
	ReturnDate       *time.Time
	Status           string
	CrewSize         int
	VehicleLabel     string
	InventorySummary string
	TotalAmount      float64
	BalanceDue       float64
}

type ServiceOrderItem struct {
	ID           int64
	EquipmentID  int64
	EquipmentCode string
	EquipmentName string
	Quantity     int
}

type ChecklistItem struct {
	ID             int64
	ServiceOrderID int64
	EquipmentID    int64
	EquipmentCode  string
	EquipmentName  string
	Status         string
	Notes          string
	CreatedAt      time.Time
}

type DashboardSummary struct {
	TotalOrders      int
	OrdersInField    int
	VehiclesReserved int
	OpenReceivables  float64
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Init(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, bootstrapSchema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	var count int64
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM service_orders`).Scan(&count); err != nil {
		return fmt.Errorf("count service orders: %w", err)
	}

	if count == 0 {
		defaults := []ServiceOrder{
			{
				Code:             "OS-2026-001",
				CustomerName:     "Casa Aurora Eventos",
				EventName:        "Feira de Noivas Primavera",
				EventCity:        "Cuiaba",
				EventDate:        time.Now().Add(48 * time.Hour),
				Status:           "agendado",
				CrewSize:         6,
				VehicleLabel:     "VUC 3/4 - Placa TSR-2041",
				InventorySummary: "Palco modular, 120 cadeiras Tiffany, kit iluminacao cenario",
				TotalAmount:      18500,
				BalanceDue:       9250,
			},
			{
				Code:             "OS-2026-002",
				CustomerName:     "Grupo Pantanal Experience",
				EventName:        "Convencao de Franqueados",
				EventCity:        "Varzea Grande",
				EventDate:        time.Now(),
				Status:           "em_execucao",
				CrewSize:         10,
				VehicleLabel:     "Truck BaU - Placa TSR-8830",
				InventorySummary: "Painel LED P3, praticaveis, sonorizacao completa",
				TotalAmount:      42750,
				BalanceDue:       0,
			},
			{
				Code:             "OS-2026-003",
				CustomerName:     "Instituto Terra Viva",
				EventName:        "Mutirao de Saude Corporativa",
				EventCity:        "Rondonopolis",
				EventDate:        time.Now().Add(120 * time.Hour),
				Status:           "orcamento",
				CrewSize:         4,
				VehicleLabel:     "Van Operacional - Placa TSR-1108",
				InventorySummary: "Tendas 10x10, climatizadores, mobiliario lounge",
				TotalAmount:      13200,
				BalanceDue:       13200,
			},
		}

		for _, item := range defaults {
			if _, err := s.pool.Exec(ctx, `
INSERT INTO service_orders (
	code,
	customer_name,
	event_name,
	event_city,
	event_date,
	install_date,
	return_date,
	status,
	crew_size,
	vehicle_label,
	inventory_summary,
	total_amount,
	balance_due
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
`,
				item.Code,
				item.CustomerName,
				item.EventName,
				item.EventCity,
				item.EventDate,
				item.InstallDate,
				item.ReturnDate,
				item.Status,
				item.CrewSize,
				item.VehicleLabel,
				item.InventorySummary,
				item.TotalAmount,
				item.BalanceDue,
			); err != nil {
				return fmt.Errorf("seed service order: %w", err)
			}
		}
	}

	return nil
}

func (s *Store) List(ctx context.Context, status string) ([]ServiceOrder, error) {
	query := `
SELECT
	id,
	code,
	customer_name,
	event_name,
	event_city,
	event_date,
	install_date,
	return_date,
	status,
	crew_size,
	vehicle_label,
	inventory_summary,
	total_amount,
	balance_due
FROM service_orders
`
	args := []any{}
	if status != "" {
		query += `WHERE status = $1 `
		args = append(args, status)
	}
	query += `ORDER BY event_date ASC, created_at ASC, id ASC`

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list service orders: %w", err)
	}
	defer rows.Close()

	items := make([]ServiceOrder, 0)
	for rows.Next() {
		var item ServiceOrder
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.CustomerName,
			&item.EventName,
			&item.EventCity,
			&item.EventDate,
			&item.InstallDate,
			&item.ReturnDate,
			&item.Status,
			&item.CrewSize,
			&item.VehicleLabel,
			&item.InventorySummary,
			&item.TotalAmount,
			&item.BalanceDue,
		); err != nil {
			return nil, fmt.Errorf("scan service order: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate service orders: %w", err)
	}

	return items, nil
}

func (s *Store) Create(ctx context.Context, customerName, eventName, eventCity, eventDateRaw, installDateRaw, returnDateRaw, vehicleLabel string, crewSize int) (int64, error) {
	customerName = strings.TrimSpace(customerName)
	eventName = strings.TrimSpace(eventName)
	eventCity = strings.TrimSpace(eventCity)
	eventDateRaw = strings.TrimSpace(eventDateRaw)
	installDateRaw = strings.TrimSpace(installDateRaw)
	returnDateRaw = strings.TrimSpace(returnDateRaw)
	vehicleLabel = strings.TrimSpace(vehicleLabel)

	if customerName == "" {
		return 0, fmt.Errorf("cliente e obrigatorio")
	}
	if eventName == "" {
		return 0, fmt.Errorf("evento e obrigatorio")
	}
	if eventCity == "" {
		return 0, fmt.Errorf("cidade e obrigatoria")
	}
	if vehicleLabel == "" {
		return 0, fmt.Errorf("veiculo e obrigatorio")
	}
	if crewSize <= 0 {
		return 0, fmt.Errorf("equipe deve ser maior que zero")
	}

	eventDate, err := time.Parse("2006-01-02", eventDateRaw)
	if err != nil {
		return 0, fmt.Errorf("data do evento invalida")
	}

	var installDate, returnDate *time.Time
	if installDateRaw != "" {
		if d, err := time.Parse("2006-01-02", installDateRaw); err == nil {
			installDate = &d
		}
	}
	if returnDateRaw != "" {
		if d, err := time.Parse("2006-01-02", returnDateRaw); err == nil {
			returnDate = &d
		}
	}

	code, err := s.nextCode(ctx, eventDate.Year())
	if err != nil {
		return 0, fmt.Errorf("generate code: %w", err)
	}

	totalAmount := float64(crewSize) * 2750
	balanceDue := totalAmount * 0.5

	var id int64
	if err := s.pool.QueryRow(ctx, `
INSERT INTO service_orders (
	code,
	customer_name,
	event_name,
	event_city,
	event_date,
	install_date,
	return_date,
	status,
	crew_size,
	vehicle_label,
	inventory_summary,
	total_amount,
	balance_due
)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'orcamento', $8, $9, $10, $11, $12)
RETURNING id
`,
		code,
		customerName,
		eventName,
		eventCity,
		eventDate,
		installDate,
		returnDate,
		crewSize,
		vehicleLabel,
		"A definir no planejamento operacional",
		totalAmount,
		balanceDue,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("create service order: %w", err)
	}

	return id, nil
}

func (s *Store) GetOrderItems(ctx context.Context, soID int64) ([]ServiceOrderItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT soi.id, soi.equipment_id, e.code, e.name, soi.quantity
		FROM service_order_items soi
		JOIN equipment e ON e.id = soi.equipment_id
		WHERE soi.service_order_id = $1
		ORDER BY e.name
	`, soID)
	if err != nil {
		return nil, fmt.Errorf("list order items: %w", err)
	}
	defer rows.Close()

	var items []ServiceOrderItem
	for rows.Next() {
		var i ServiceOrderItem
		if err := rows.Scan(&i.ID, &i.EquipmentID, &i.EquipmentCode, &i.EquipmentName, &i.Quantity); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order items: %w", err)
	}
	return items, nil
}

func (s *Store) AddOrderItem(ctx context.Context, soID, equipmentID int64, qty int) error {
	if qty <= 0 {
		return fmt.Errorf("quantidade deve ser maior que zero")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO service_order_items (service_order_id, equipment_id, quantity)
		VALUES ($1, $2, $3)
	`, soID, equipmentID, qty)
	if err != nil {
		return fmt.Errorf("add order item: %w", err)
	}
	return nil
}

func (s *Store) GetChecklist(ctx context.Context, soID int64) ([]ChecklistItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.service_order_id, c.equipment_id, e.code, e.name, c.status, c.notes, c.created_at
		FROM service_order_checklist c
		JOIN equipment e ON e.id = c.equipment_id
		WHERE c.service_order_id = $1
		ORDER BY e.name
	`, soID)
	if err != nil {
		return nil, fmt.Errorf("list checklist: %w", err)
	}
	defer rows.Close()

	var items []ChecklistItem
	for rows.Next() {
		var c ChecklistItem
		if err := rows.Scan(&c.ID, &c.ServiceOrderID, &c.EquipmentID, &c.EquipmentCode, &c.EquipmentName, &c.Status, &c.Notes, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan checklist: %w", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checklist: %w", err)
	}
	return items, nil
}

type Charge struct {
	ID             int64
	ServiceOrderID int64
	EquipmentID    *int64
	ChargeType     string
	Amount         float64
	Description    string
	CreatedAt      time.Time
}

func (s *Store) AddCharge(ctx context.Context, soID int64, equipmentID *int64, chargeType, description string, amount float64) error {
	if amount < 0 {
		return fmt.Errorf("valor nao pode ser negativo")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO service_order_charges (service_order_id, equipment_id, charge_type, amount, description)
		VALUES ($1, $2, $3, $4, $5)
	`, soID, equipmentID, chargeType, amount, description)
	if err != nil {
		return fmt.Errorf("add charge: %w", err)
	}
	return nil
}

func (s *Store) GetCharges(ctx context.Context, soID int64) ([]Charge, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, service_order_id, equipment_id, charge_type, amount, description, created_at
		FROM service_order_charges
		WHERE service_order_id = $1
		ORDER BY created_at DESC
	`, soID)
	if err != nil {
		return nil, fmt.Errorf("get charges: %w", err)
	}
	defer rows.Close()

	var items []Charge
	for rows.Next() {
		var c Charge
		if err := rows.Scan(&c.ID, &c.ServiceOrderID, &c.EquipmentID, &c.ChargeType, &c.Amount, &c.Description, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan charge: %w", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate charges: %w", err)
	}
	return items, nil
}

func (s *Store) GetTotalCharges(ctx context.Context, soID int64) (float64, error) {
	var total float64
	if err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount), 0) FROM service_order_charges WHERE service_order_id = $1
	`, soID).Scan(&total); err != nil {
		return 0, fmt.Errorf("get total charges: %w", err)
	}
	return total, nil
}

type DamageReport struct {
	EquipmentID   int64
	EquipmentCode string
	EquipmentName string
	TotalAvaria   float64
	TotalPerda    float64
	TotalGeral    float64
	CountIssues   int
}

type ChargeSummary struct {
	Type  string
	Count int
	Total float64
}

func (s *Store) GetDamageReport(ctx context.Context) ([]DamageReport, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT 
			e.id,
			COALESCE(e.code, 'DESCONHECIDO'),
			COALESCE(e.name, 'DESCONHECIDO'),
			COALESCE(SUM(CASE WHEN c.charge_type = 'avaria' THEN c.amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN c.charge_type = 'perda' THEN c.amount ELSE 0 END), 0),
			COALESCE(SUM(c.amount), 0),
			COUNT(c.id)
		FROM service_order_charges c
		JOIN equipment e ON e.id = c.equipment_id
		GROUP BY e.id, e.code, e.name
		ORDER BY SUM(c.amount) DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("get damage report: %w", err)
	}
	defer rows.Close()

	var items []DamageReport
	for rows.Next() {
		var d DamageReport
		if err := rows.Scan(&d.EquipmentID, &d.EquipmentCode, &d.EquipmentName, &d.TotalAvaria, &d.TotalPerda, &d.TotalGeral, &d.CountIssues); err != nil {
			return nil, fmt.Errorf("scan damage report: %w", err)
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate damage report: %w", err)
	}
	return items, nil
}

func (s *Store) GetChargeSummary(ctx context.Context) ([]ChargeSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT charge_type, COUNT(*), COALESCE(SUM(amount), 0)
		FROM service_order_charges
		GROUP BY charge_type
		ORDER BY SUM(amount) DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("get charge summary: %w", err)
	}
	defer rows.Close()

	var items []ChargeSummary
	for rows.Next() {
		var c ChargeSummary
		if err := rows.Scan(&c.Type, &c.Count, &c.Total); err != nil {
			return nil, fmt.Errorf("scan charge summary: %w", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate charge summary: %w", err)
	}
	return items, nil
}

func (s *Store) GetRecentCharges(ctx context.Context, limit int) ([]Charge, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.service_order_id, c.equipment_id, c.charge_type, c.amount, c.description, c.created_at
		FROM service_order_charges c
		ORDER BY c.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent charges: %w", err)
	}
	defer rows.Close()

	var items []Charge
	for rows.Next() {
		var c Charge
		if err := rows.Scan(&c.ID, &c.ServiceOrderID, &c.EquipmentID, &c.ChargeType, &c.Amount, &c.Description, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan recent charge: %w", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent charges: %w", err)
	}
	return items, nil
}

func (s *Store) UpdateBalance(ctx context.Context, soID int64) error {
	var totalAmount, totalCharges float64
	if err := s.pool.QueryRow(ctx, `SELECT total_amount FROM service_orders WHERE id = $1`, soID).Scan(&totalAmount); err != nil {
		return fmt.Errorf("get total amount: %w", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM service_order_charges WHERE service_order_id = $1`, soID).Scan(&totalCharges); err != nil {
		return fmt.Errorf("get total charges: %w", err)
	}
	newBalance := totalAmount + totalCharges
	_, err := s.pool.Exec(ctx, `UPDATE service_orders SET balance_due = $2 WHERE id = $1`, soID, newBalance)
	if err != nil {
		return fmt.Errorf("update balance: %w", err)
	}
	return nil
}

func (s *Store) ListOverdueOrders(ctx context.Context) ([]ServiceOrder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, customer_name, event_name, event_city, event_date, install_date, return_date, status, crew_size, vehicle_label, inventory_summary, total_amount, balance_due
		FROM service_orders
		WHERE event_date < CURRENT_DATE AND status NOT IN ('finalizado', 'orcamento')
		ORDER BY event_date ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list overdue orders: %w", err)
	}
	defer rows.Close()

	var items []ServiceOrder
	for rows.Next() {
		var item ServiceOrder
		if err := rows.Scan(&item.ID, &item.Code, &item.CustomerName, &item.EventName, &item.EventCity, &item.EventDate, &item.InstallDate, &item.ReturnDate, &item.Status, &item.CrewSize, &item.VehicleLabel, &item.InventorySummary, &item.TotalAmount, &item.BalanceDue); err != nil {
			return nil, fmt.Errorf("scan overdue order: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate overdue orders: %w", err)
	}
	return items, nil
}

func (s *Store) ListChecklistIssues(ctx context.Context) ([]ChecklistItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.service_order_id, c.equipment_id, e.code, e.name, c.status, c.notes, c.created_at
		FROM service_order_checklist c
		JOIN equipment e ON e.id = c.equipment_id
		WHERE c.status IN ('avariado', 'perdido', 'nao_retornado')
		ORDER BY c.created_at DESC
		LIMIT 20
	`)
	if err != nil {
		return nil, fmt.Errorf("list checklist issues: %w", err)
	}
	defer rows.Close()

	var items []ChecklistItem
	for rows.Next() {
		var c ChecklistItem
		if err := rows.Scan(&c.ID, &c.ServiceOrderID, &c.EquipmentID, &c.EquipmentCode, &c.EquipmentName, &c.Status, &c.Notes, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan checklist issue: %w", err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checklist issues: %w", err)
	}
	return items, nil
}

func (s *Store) SaveChecklistItem(ctx context.Context, soID, equipmentID int64, status, notes string) error {
	if status == "" {
		return fmt.Errorf("status e obrigatorio")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO service_order_checklist (service_order_id, equipment_id, status, notes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (service_order_id, equipment_id) DO UPDATE SET status = EXCLUDED.status, notes = EXCLUDED.notes, created_at = NOW()
	`, soID, equipmentID, status, notes)
	if err != nil {
		return fmt.Errorf("save checklist: %w", err)
	}
	return nil
}

func (s *Store) AdvanceStatus(ctx context.Context, id int64) (string, error) {
	var currentStatus string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM service_orders WHERE id = $1`, id).Scan(&currentStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("load service order status: %w", err)
	}

	nextStatus := currentStatus
	for index, status := range statusFlow {
		if status == currentStatus {
			nextStatus = statusFlow[(index+1)%len(statusFlow)]
			break
		}
	}

	tag, err := s.pool.Exec(ctx, `
UPDATE service_orders
SET status = $2, updated_at = NOW()
WHERE id = $1
`, id, nextStatus)
	if err != nil {
		return "", fmt.Errorf("advance service order status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}

	return nextStatus, nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM service_orders WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete service order: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func BuildSummary(items []ServiceOrder) DashboardSummary {
	summary := DashboardSummary{
		TotalOrders: len(items),
	}

	vehicles := make(map[string]struct{})
	for _, item := range items {
		if item.Status == "em_execucao" || item.Status == "aguardando_retorno" {
			summary.OrdersInField++
		}
		if item.Status == "agendado" || item.Status == "em_execucao" || item.Status == "aguardando_retorno" {
			vehicles[item.VehicleLabel] = struct{}{}
		}
		summary.OpenReceivables += item.BalanceDue
	}

	summary.VehiclesReserved = len(vehicles)
	summary.OpenReceivables = math.Round(summary.OpenReceivables*100) / 100
	return summary
}

func StatusLabel(status string) string {
	switch status {
	case "orcamento":
		return "Orcamento"
	case "agendado":
		return "Agendado"
	case "em_execucao":
		return "Em execucao"
	case "aguardando_retorno":
		return "Aguardando retorno"
	case "finalizado":
		return "Finalizado"
	default:
		return status
	}
}

func NextStatusLabel(status string) string {
	for index, current := range statusFlow {
		if current == status {
			return StatusLabel(statusFlow[(index+1)%len(statusFlow)])
		}
	}
	return "Atualizar status"
}

func (s *Store) nextCode(ctx context.Context, year int) (string, error) {
	var count int
	if err := s.pool.QueryRow(ctx, `
SELECT COUNT(*)
FROM service_orders
WHERE code LIKE $1
`, fmt.Sprintf("OS-%d-%%", year)).Scan(&count); err != nil {
		return "", err
	}

	return fmt.Sprintf("OS-%d-%03d", year, count+1), nil
}
