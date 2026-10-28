package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DefaultMinStock = 10

var (
	ErrNotFound         = errors.New("not found")
	ErrDuplicateSKU     = errors.New("sku already exists")
	ErrHasHistory       = errors.New("product has history")
	ErrAlreadyCancelled = errors.New("order already cancelled")
	ErrKeyReused        = errors.New("idempotency key reused with a different request")
)

type InsufficientStockError struct {
	SKU       string
	Available int
}

func (e *InsufficientStockError) Error() string {
	return fmt.Sprintf("insufficient stock for %s: %d available", e.SKU, e.Available)
}

type ValidationError struct{ Details []FieldError }

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string { return "validation failed" }

type Store struct {
	pool *pgxpool.Pool
	aud  *auditWriter
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, aud: newAuditWriter(pool)} }

func (s *Store) FlushAudit(ctx context.Context) error { return s.aud.flush(ctx) }

func (s *Store) Close(ctx context.Context) error { return s.aud.close(ctx) }

type Product struct {
	ID             int        `json:"id"`
	SKU            string     `json:"sku"`
	Name           string     `json:"name"`
	Price          int        `json:"price"`
	Stock          int        `json:"stock"`
	IsActive       bool       `json:"is_active"`
	MinStock       int        `json:"min_stock"`
	CreatedAt      time.Time  `json:"created_at"`
	LastMovementAt *time.Time `json:"last_movement_at"`
}

const productSelect = `
SELECT p.id, p.sku, p.name, p.price, p.stock, p.is_active, p.min_stock, p.created_at,
       (SELECT MAX(m.created_at) FROM stock_movements m WHERE m.product_id = p.id)
FROM products p`

func scanProduct(row pgx.Row) (Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.SKU, &p.Name, &p.Price, &p.Stock, &p.IsActive, &p.MinStock, &p.CreatedAt, &p.LastMovementAt)
	return p, err
}

type ProductList struct {
	Data       []Product `json:"data"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	Total      int       `json:"total"`
	NextCursor *int      `json:"next_cursor,omitempty"`
}

type ListParams struct {
	Search string
	Status string
	Page   int
	Limit  int

	Keyset bool
	After  int
}

func (s *Store) ListProducts(ctx context.Context, lp ListParams) (ProductList, error) {
	where := `WHERE ($1 = '' OR p.sku ILIKE '%' || $1 || '%' OR p.name ILIKE '%' || $1 || '%')
	  AND CASE $2 WHEN 'active' THEN p.is_active
	              WHEN 'inactive' THEN NOT p.is_active
	              WHEN 'low' THEN p.stock <= p.min_stock
	              ELSE TRUE END`
	search := escapeLike(lp.Search)
	out := ProductList{Page: lp.Page, Limit: lp.Limit, Data: []Product{}}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM products p `+where, search, lp.Status).Scan(&out.Total); err != nil {
		return out, err
	}
	var rows pgx.Rows
	var err error
	if lp.Keyset {

		rows, err = s.pool.Query(ctx, productSelect+" "+where+` AND p.id > $3 ORDER BY p.id LIMIT $4`,
			search, lp.Status, lp.After, lp.Limit+1)
	} else {
		rows, err = s.pool.Query(ctx, productSelect+" "+where+` ORDER BY p.id LIMIT $3 OFFSET $4`,
			search, lp.Status, lp.Limit, (lp.Page-1)*lp.Limit)
	}
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return out, err
		}
		out.Data = append(out.Data, p)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if lp.Keyset && len(out.Data) > lp.Limit {
		out.Data = out.Data[:lp.Limit]
		last := out.Data[lp.Limit-1].ID
		out.NextCursor = &last
	}
	return out, nil
}

func escapeLike(s string) string {
	r := make([]rune, 0, len(s))
	for _, c := range s {
		if c == '\\' || c == '%' || c == '_' {
			r = append(r, '\\')
		}
		r = append(r, c)
	}
	return string(r)
}

func (s *Store) GetProduct(ctx context.Context, id int) (Product, error) {
	p, err := scanProduct(s.pool.QueryRow(ctx, productSelect+" WHERE p.id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

type ProductInput struct {
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	Price    int    `json:"price"`
	IsActive *bool  `json:"is_active"`

	MinStock *int `json:"min_stock"`
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func (s *Store) CreateProduct(ctx context.Context, in ProductInput) (Product, error) {
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Product{}, err
	}
	defer tx.Rollback(ctx)
	minStock := DefaultMinStock
	if in.MinStock != nil {
		minStock = *in.MinStock
	}
	var id int
	err = tx.QueryRow(ctx, `INSERT INTO products (sku, name, price, is_active, min_stock) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		in.SKU, in.Name, in.Price, active, minStock).Scan(&id)
	if isUnique(err) {
		return Product{}, ErrDuplicateSKU
	}
	if err != nil {
		return Product{}, err
	}
	ev := newAuditEvent("product", id, in.SKU, "product.created",
		map[string]any{"sku": in.SKU, "name": in.Name, "price": in.Price, "is_active": active, "min_stock": minStock})
	if err := tx.Commit(ctx); err != nil {
		return Product{}, err
	}
	s.aud.record(ev)
	return s.GetProduct(ctx, id)
}

func (s *Store) UpdateProduct(ctx context.Context, id int, in ProductInput) (Product, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Product{}, err
	}
	defer tx.Rollback(ctx)
	var oldSKU, oldName string
	var oldPrice, oldMin int
	var oldActive bool
	err = tx.QueryRow(ctx, `SELECT sku, name, price, is_active, min_stock FROM products WHERE id=$1 FOR UPDATE`, id).
		Scan(&oldSKU, &oldName, &oldPrice, &oldActive, &oldMin)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrNotFound
	}
	if err != nil {
		return Product{}, err
	}
	newActive := oldActive
	if in.IsActive != nil {
		newActive = *in.IsActive
	}
	newMin := oldMin
	if in.MinStock != nil {
		newMin = *in.MinStock
	}
	_, err = tx.Exec(ctx, `UPDATE products SET sku=$2, name=$3, price=$4, is_active=$5, min_stock=$6 WHERE id=$1`,
		id, in.SKU, in.Name, in.Price, newActive, newMin)
	if isUnique(err) {
		return Product{}, ErrDuplicateSKU
	}
	if err != nil {
		return Product{}, err
	}
	changes := map[string]map[string]any{}
	diff := func(field string, from, to any) {
		if from != to {
			changes[field] = map[string]any{"from": from, "to": to}
		}
	}
	diff("sku", oldSKU, in.SKU)
	diff("name", oldName, in.Name)
	diff("price", oldPrice, in.Price)
	diff("is_active", oldActive, newActive)
	diff("min_stock", oldMin, newMin)
	if err := tx.Commit(ctx); err != nil {
		return Product{}, err
	}
	if len(changes) > 0 {
		s.aud.record(newAuditEvent("product", id, in.SKU, "product.updated", map[string]any{"changes": changes}))
	}
	return s.GetProduct(ctx, id)
}

func (s *Store) DeleteProduct(ctx context.Context, id int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var sku, name string
	var price, stock int
	if err := tx.QueryRow(ctx, `SELECT sku, name, price, stock FROM products WHERE id=$1 FOR UPDATE`, id).
		Scan(&sku, &name, &price, &stock); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var has bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stock_movements WHERE product_id=$1)
	  OR EXISTS (SELECT 1 FROM sales_order_items WHERE sku=$2)`, id, sku).Scan(&has); err != nil {
		return err
	}
	if has {
		return ErrHasHistory
	}
	if _, err := tx.Exec(ctx, `DELETE FROM products WHERE id=$1`, id); err != nil {
		return err
	}
	ev := newAuditEvent("product", id, sku, "product.deleted",
		map[string]any{"sku": sku, "name": name, "price": price, "stock": stock})
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	s.aud.record(ev)
	return nil
}

type Movement struct {
	ID        int       `json:"id"`
	ProductID int       `json:"product_id"`
	Type      string    `json:"type"`
	Qty       int       `json:"qty"`
	Note      *string   `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) AdjustStock(ctx context.Context, id int, typ string, qty int, note string) (Product, Movement, error) {
	var mv Movement
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Product{}, mv, err
	}
	defer tx.Rollback(ctx)

	delta := qty
	if typ == "out" {
		delta = -qty
	}
	var sku string
	var after int
	err = tx.QueryRow(ctx, `UPDATE products SET stock = stock + $2 WHERE id = $1 AND stock + $2 >= 0
	  RETURNING sku, stock`, id, delta).Scan(&sku, &after)
	if errors.Is(err, pgx.ErrNoRows) {
		var avail int
		if err := tx.QueryRow(ctx, `SELECT sku, stock FROM products WHERE id=$1`, id).Scan(&sku, &avail); errors.Is(err, pgx.ErrNoRows) {
			return Product{}, mv, ErrNotFound
		} else if err != nil {
			return Product{}, mv, err
		}
		return Product{}, mv, &InsufficientStockError{SKU: sku, Available: avail}
	}
	if err != nil {
		return Product{}, mv, err
	}
	var n *string
	if note != "" {
		n = &note
	}
	err = tx.QueryRow(ctx, `INSERT INTO stock_movements (product_id, type, qty, note) VALUES ($1,$2,$3,$4)
	  RETURNING id, product_id, type, qty, note, created_at`, id, typ, qty, n).
		Scan(&mv.ID, &mv.ProductID, &mv.Type, &mv.Qty, &mv.Note, &mv.CreatedAt)
	if err != nil {
		return Product{}, mv, err
	}
	ev := newAuditEvent("product", id, sku, "stock."+typ,
		map[string]any{"qty": qty, "note": note, "stock_before": after - delta, "stock_after": after})
	if err := tx.Commit(ctx); err != nil {
		return Product{}, mv, err
	}
	s.aud.record(ev)
	p, err := s.GetProduct(ctx, id)
	return p, mv, err
}

func (s *Store) ListMovements(ctx context.Context, productID int) ([]Movement, error) {
	if _, err := s.GetProduct(ctx, productID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id, product_id, type, qty, note, created_at FROM stock_movements
	  WHERE product_id=$1 ORDER BY created_at DESC, id DESC`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Movement{}
	for rows.Next() {
		var m Movement
		if err := rows.Scan(&m.ID, &m.ProductID, &m.Type, &m.Qty, &m.Note, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type Summary struct {
	TotalProducts   int   `json:"total_products"`
	ActiveProducts  int   `json:"active_products"`
	TotalStockValue int64 `json:"total_stock_value"`
	LowStockCount   int   `json:"low_stock_count"`
}

func (s *Store) Summary(ctx context.Context) (Summary, error) {
	var sm Summary
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE is_active),
	  COALESCE(SUM(price::bigint * stock), 0), COUNT(*) FILTER (WHERE stock <= min_stock) FROM products`).
		Scan(&sm.TotalProducts, &sm.ActiveProducts, &sm.TotalStockValue, &sm.LowStockCount)
	return sm, err
}

type OrderItem struct {
	SKU       string `json:"sku"`
	Name      string `json:"name,omitempty"`
	Qty       int    `json:"qty"`
	UnitPrice int    `json:"unit_price"`
}

type Order struct {
	ID          int         `json:"id"`
	Customer    string      `json:"customer"`
	Status      string      `json:"status"`
	Total       int         `json:"total"`
	CreatedAt   time.Time   `json:"created_at"`
	CancelledAt *time.Time  `json:"cancelled_at"`
	Items       []OrderItem `json:"items"`
}

type OrderItemInput struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

type OrderInput struct {
	Customer string           `json:"customer"`
	Items    []OrderItemInput `json:"items"`
}

func validateOrder(in OrderInput) []FieldError {
	var errs []FieldError
	if trim(in.Customer) == "" {
		errs = append(errs, FieldError{"customer", "customer is required"})
	}
	if len(in.Items) == 0 {
		errs = append(errs, FieldError{"items", "at least one item is required"})
	}
	seen := map[string]int{}
	for i, it := range in.Items {
		f := fmt.Sprintf("items[%d]", i)
		if trim(it.SKU) == "" {
			errs = append(errs, FieldError{f + ".sku", "sku is required"})
		} else if first, dup := seen[it.SKU]; dup {
			errs = append(errs, FieldError{f + ".sku", fmt.Sprintf("duplicate sku %q (also at items[%d])", it.SKU, first)})
		} else {
			seen[it.SKU] = i
		}
		if it.Qty <= 0 {
			errs = append(errs, FieldError{f + ".qty", "qty must be greater than 0"})
		}
	}
	return errs
}

func trim(s string) string { return strings.TrimSpace(s) }

func (s *Store) CreateOrder(ctx context.Context, in OrderInput, idemKey string) (order Order, replayed bool, err error) {
	if errs := validateOrder(in); len(errs) > 0 {
		return Order{}, false, &ValidationError{errs}
	}
	hash := orderHash(in)
	if idemKey != "" {
		if o, err := s.orderByKey(ctx, idemKey, hash); err == nil {
			return o, true, nil
		} else if !errors.Is(err, ErrNotFound) {
			return Order{}, false, err
		}
	}

	items := append([]OrderItemInput(nil), in.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].SKU < items[j].SKU })

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, false, err
	}
	defer tx.Rollback(ctx)

	skus := make([]string, len(items))
	for i, it := range items {
		skus[i] = it.SKU
	}
	rows, err := tx.Query(ctx, `SELECT sku, is_active FROM products WHERE sku = ANY($1)`, skus)
	if err != nil {
		return Order{}, false, err
	}
	known := map[string]bool{}
	for rows.Next() {
		var sku string
		var active bool
		if err := rows.Scan(&sku, &active); err != nil {
			rows.Close()
			return Order{}, false, err
		}
		known[sku] = active
	}
	rows.Close()
	if rows.Err() != nil {
		return Order{}, false, rows.Err()
	}
	var errs []FieldError
	for i, it := range in.Items {
		active, ok := known[it.SKU]
		switch {
		case !ok:
			errs = append(errs, FieldError{fmt.Sprintf("items[%d].sku", i), fmt.Sprintf("unknown sku %q", it.SKU)})
		case !active:
			errs = append(errs, FieldError{fmt.Sprintf("items[%d].sku", i), fmt.Sprintf("sku %q is inactive", it.SKU)})
		}
	}
	if len(errs) > 0 {
		return Order{}, false, &ValidationError{errs}
	}

	var key *string
	if idemKey != "" {
		key = &idemKey
	}
	var o Order
	err = tx.QueryRow(ctx, `INSERT INTO sales_orders (customer, total, idempotency_key, idempotency_hash) VALUES ($1, 0, $2, $3)
	  RETURNING id, customer, status, created_at`, trim(in.Customer), key, hash).
		Scan(&o.ID, &o.Customer, &o.Status, &o.CreatedAt)
	if isUnique(err) && idemKey != "" {
		tx.Rollback(ctx)
		prev, gerr := s.orderByKey(ctx, idemKey, hash)
		return prev, gerr == nil, gerr
	}
	if err != nil {
		return Order{}, false, err
	}

	o.Items = make([]OrderItem, 0, len(items))
	total := 0
	for _, it := range items {
		var pid, price int
		var name string

		err := tx.QueryRow(ctx, `UPDATE products SET stock = stock - $2
		  WHERE sku = $1 AND stock >= $2 RETURNING id, price, name`, it.SKU, it.Qty).Scan(&pid, &price, &name)
		if errors.Is(err, pgx.ErrNoRows) {
			var avail int
			if err := tx.QueryRow(ctx, `SELECT stock FROM products WHERE sku=$1`, it.SKU).Scan(&avail); err != nil {
				return Order{}, false, err
			}
			return Order{}, false, &InsufficientStockError{SKU: it.SKU, Available: avail}
		}
		if err != nil {
			return Order{}, false, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO stock_movements (product_id, type, qty, note) VALUES ($1,'out',$2,$3)`,
			pid, it.Qty, fmt.Sprintf("Sales order #%d", o.ID)); err != nil {
			return Order{}, false, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sales_order_items (order_id, sku, qty, unit_price) VALUES ($1,$2,$3,$4)`,
			o.ID, it.SKU, it.Qty, price); err != nil {
			return Order{}, false, err
		}
		total += price * it.Qty
		o.Items = append(o.Items, OrderItem{SKU: it.SKU, Name: name, Qty: it.Qty, UnitPrice: price})
	}
	if _, err := tx.Exec(ctx, `UPDATE sales_orders SET total=$2 WHERE id=$1`, o.ID, total); err != nil {
		return Order{}, false, err
	}
	o.Total = total
	ev := newAuditEvent("order", o.ID, fmt.Sprintf("Order #%d", o.ID), "order.created",
		map[string]any{"customer": o.Customer, "total": total, "items": o.Items})
	if err := tx.Commit(ctx); err != nil {
		return Order{}, false, err
	}
	s.aud.record(ev)
	return o, false, nil
}

func orderHash(in OrderInput) string {
	items := append([]OrderItemInput(nil), in.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].SKU < items[j].SKU })
	b, _ := json.Marshal(struct {
		C string           `json:"c"`
		I []OrderItemInput `json:"i"`
	}{trim(in.Customer), items})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s *Store) orderByKey(ctx context.Context, key, hash string) (Order, error) {
	var id int
	var stored *string
	err := s.pool.QueryRow(ctx, `SELECT id, idempotency_hash FROM sales_orders WHERE idempotency_key=$1`, key).Scan(&id, &stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	if stored != nil && *stored != hash {
		return Order{}, ErrKeyReused
	}
	return s.GetOrder(ctx, id)
}

func (s *Store) GetOrder(ctx context.Context, id int) (Order, error) {
	var o Order
	err := s.pool.QueryRow(ctx, `SELECT id, customer, status, total, created_at, cancelled_at FROM sales_orders WHERE id=$1`, id).
		Scan(&o.ID, &o.Customer, &o.Status, &o.Total, &o.CreatedAt, &o.CancelledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	rows, err := s.pool.Query(ctx, `SELECT i.sku, p.name, i.qty, i.unit_price FROM sales_order_items i
	  JOIN products p ON p.sku = i.sku WHERE i.order_id=$1 ORDER BY i.id`, id)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	o.Items = []OrderItem{}
	for rows.Next() {
		var it OrderItem
		if err := rows.Scan(&it.SKU, &it.Name, &it.Qty, &it.UnitPrice); err != nil {
			return o, err
		}
		o.Items = append(o.Items, it)
	}
	return o, rows.Err()
}

func (s *Store) ListOrders(ctx context.Context, limit int) ([]Order, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM sales_orders ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := make([]Order, 0, len(ids))
	for _, id := range ids {
		o, err := s.GetOrder(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, nil
}

func (s *Store) CancelOrder(ctx context.Context, id int) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM sales_orders WHERE id=$1 FOR UPDATE`, id).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	} else if err != nil {
		return Order{}, err
	}
	if status == "CANCELLED" {
		return Order{}, ErrAlreadyCancelled
	}
	rows, err := tx.Query(ctx, `SELECT sku, qty FROM sales_order_items WHERE order_id=$1 ORDER BY sku`, id)
	if err != nil {
		return Order{}, err
	}
	var items []OrderItemInput
	for rows.Next() {
		var it OrderItemInput
		if err := rows.Scan(&it.SKU, &it.Qty); err != nil {
			rows.Close()
			return Order{}, err
		}
		items = append(items, it)
	}
	rows.Close()
	for _, it := range items {
		var pid int
		if err := tx.QueryRow(ctx, `UPDATE products SET stock = stock + $2 WHERE sku=$1 RETURNING id`, it.SKU, it.Qty).Scan(&pid); err != nil {
			return Order{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO stock_movements (product_id, type, qty, note) VALUES ($1,'in',$2,$3)`,
			pid, it.Qty, fmt.Sprintf("Sales order #%d cancelled", id)); err != nil {
			return Order{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE sales_orders SET status='CANCELLED', cancelled_at=now() WHERE id=$1`, id); err != nil {
		return Order{}, err
	}
	ev := newAuditEvent("order", id, fmt.Sprintf("Order #%d", id), "order.cancelled", map[string]any{"items": items})
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	s.aud.record(ev)
	return s.GetOrder(ctx, id)
}

type DailySales struct {
	Date           string `json:"date"`
	OrderCount     int    `json:"order_count"`
	CancelledCount int    `json:"cancelled_count"`
	UnitsSold      int    `json:"units_sold"`
	Revenue        int64  `json:"revenue"`
}

func (s *Store) DailySales(ctx context.Context, date string) (DailySales, error) {
	d := DailySales{Date: date}
	err := s.pool.QueryRow(ctx, `
	  SELECT COUNT(*) FILTER (WHERE status='CREATED'),
	         COUNT(*) FILTER (WHERE status='CANCELLED'),
	         COALESCE(SUM(total) FILTER (WHERE status='CREATED'), 0),
	         COALESCE(SUM((SELECT SUM(qty) FROM sales_order_items i WHERE i.order_id = o.id)) FILTER (WHERE status='CREATED'), 0)
	  FROM sales_orders o
	  WHERE (o.created_at AT TIME ZONE 'Asia/Jakarta')::date = $1::date`, date).
		Scan(&d.OrderCount, &d.CancelledCount, &d.Revenue, &d.UnitsSold)
	return d, err
}

type AuditEntry struct {
	ID          int             `json:"id"`
	EntityType  string          `json:"entity_type"`
	EntityID    int             `json:"entity_id"`
	EntityLabel string          `json:"entity_label"`
	Action      string          `json:"action"`
	Details     json.RawMessage `json:"details"`
	CreatedAt   time.Time       `json:"created_at"`
}

type AuditPage struct {
	Data  []AuditEntry `json:"data"`
	Page  int          `json:"page"`
	Limit int          `json:"limit"`
	Total int          `json:"total"`
}

type AuditParams struct {
	EntityType string
	EntityID   int
	Page       int
	Limit      int
}

func (s *Store) ListAudit(ctx context.Context, ap AuditParams) (AuditPage, error) {
	where := `WHERE ($1 = '' OR entity_type = $1) AND ($2 = 0 OR entity_id = $2)`
	out := AuditPage{Page: ap.Page, Limit: ap.Limit, Data: []AuditEntry{}}
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log `+where, ap.EntityType, ap.EntityID).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id, entity_type, entity_id, entity_label, action, details, created_at
	  FROM audit_log `+where+` ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`,
		ap.EntityType, ap.EntityID, ap.Limit, (ap.Page-1)*ap.Limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.EntityLabel, &e.Action, &e.Details, &e.CreatedAt); err != nil {
			return out, err
		}
		out.Data = append(out.Data, e)
	}
	return out, rows.Err()
}

type BulkDuplicateError struct {
	Index int
	SKU   string
}

func (e *BulkDuplicateError) Error() string { return "duplicate sku " + e.SKU }

const MaxBulkProducts = 500

func (s *Store) BulkCreateProducts(ctx context.Context, in []ProductInput) ([]Product, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	ids := make([]int, 0, len(in))
	events := make([]auditEvent, 0, len(in))
	for i, p := range in {
		active := true
		if p.IsActive != nil {
			active = *p.IsActive
		}
		minStock := DefaultMinStock
		if p.MinStock != nil {
			minStock = *p.MinStock
		}
		var id int
		err := tx.QueryRow(ctx, `INSERT INTO products (sku, name, price, is_active, min_stock) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
			p.SKU, p.Name, p.Price, active, minStock).Scan(&id)
		if isUnique(err) {
			return nil, &BulkDuplicateError{Index: i, SKU: p.SKU}
		}
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
		events = append(events, newAuditEvent("product", id, p.SKU, "product.created",
			map[string]any{"sku": p.SKU, "name": p.Name, "price": p.Price, "is_active": active, "min_stock": minStock, "bulk": true}))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	for _, ev := range events {
		s.aud.record(ev)
	}
	out := make([]Product, 0, len(ids))
	for _, id := range ids {
		p, err := s.GetProduct(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
