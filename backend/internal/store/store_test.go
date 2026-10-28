package store

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	schema, err := os.ReadFile("../../migrations/001_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS audit_log, sales_order_items, sales_orders, stock_movements, products CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatal(err)
	}
	return New(pool), pool
}

func seedProduct(t *testing.T, s *Store, sku string, price, stock int) Product {
	t.Helper()
	ctx := context.Background()
	p, err := s.CreateProduct(ctx, ProductInput{SKU: sku, Name: "Item " + sku, Price: price})
	if err != nil {
		t.Fatal(err)
	}
	if stock > 0 {
		if p, _, err = s.AdjustStock(ctx, p.ID, "in", stock, "seed"); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func count(t *testing.T, pool *pgxpool.Pool, q string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestInsufficientStockWritesNothing(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	a := seedProduct(t, s, "A", 1000, 5)
	b := seedProduct(t, s, "B", 2000, 1)
	movBefore := count(t, pool, `SELECT COUNT(*) FROM stock_movements`)

	_, _, err := s.CreateOrder(ctx, OrderInput{Customer: "c", Items: []OrderItemInput{{"A", 3}, {"B", 2}}}, "")
	var ins *InsufficientStockError
	if !errors.As(err, &ins) || ins.SKU != "B" || ins.Available != 1 {
		t.Fatalf("want InsufficientStock{B,1}, got %v", err)
	}
	for _, c := range []struct {
		p    Product
		want int
	}{{a, 5}, {b, 1}} {
		got, _ := s.GetProduct(ctx, c.p.ID)
		if got.Stock != c.want {
			t.Errorf("%s stock = %d, want %d", c.p.SKU, got.Stock, c.want)
		}
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM sales_orders`); n != 0 {
		t.Errorf("orders = %d, want 0", n)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM sales_order_items`); n != 0 {
		t.Errorf("order items = %d, want 0", n)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM stock_movements`); n != movBefore {
		t.Errorf("movements = %d, want %d", n, movBefore)
	}
}

func TestAdjustStockOutRejectedWritesNothing(t *testing.T) {
	s, pool := newTestStore(t)
	p := seedProduct(t, s, "A", 1000, 2)
	before := count(t, pool, `SELECT COUNT(*) FROM stock_movements`)
	_, _, err := s.AdjustStock(context.Background(), p.ID, "out", 3, "")
	var ins *InsufficientStockError
	if !errors.As(err, &ins) || ins.Available != 2 {
		t.Fatalf("want InsufficientStock{available 2}, got %v", err)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM stock_movements`); n != before {
		t.Errorf("movement written on rejection")
	}
}

func TestCreateOrderHappyPath(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	a := seedProduct(t, s, "A", 1000, 10)
	b := seedProduct(t, s, "B", 2500, 4)

	o, replayed, err := s.CreateOrder(ctx, OrderInput{Customer: "Budi", Items: []OrderItemInput{{"B", 2}, {"A", 3}}}, "")
	if err != nil || replayed {
		t.Fatal(err, replayed)
	}
	if o.Total != 3*1000+2*2500 || o.Status != "CREATED" || len(o.Items) != 2 {
		t.Fatalf("bad order: %+v", o)
	}
	ga, _ := s.GetProduct(ctx, a.ID)
	gb, _ := s.GetProduct(ctx, b.ID)
	if ga.Stock != 7 || gb.Stock != 2 {
		t.Errorf("stock = %d,%d want 7,2", ga.Stock, gb.Stock)
	}
	if ga.LastMovementAt == nil {
		t.Error("last_movement_at not set")
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM stock_movements WHERE type='out' AND product_id=$1 AND qty=3`, a.ID); n != 1 {
		t.Errorf("out movement for A = %d, want 1", n)
	}
}

func TestOrderValidation(t *testing.T) {
	s, _ := newTestStore(t)
	seedProduct(t, s, "A", 1000, 10)
	cases := map[string]OrderInput{
		"empty items":   {Customer: "c"},
		"no customer":   {Items: []OrderItemInput{{"A", 1}}},
		"zero qty":      {Customer: "c", Items: []OrderItemInput{{"A", 0}}},
		"duplicate sku": {Customer: "c", Items: []OrderItemInput{{"A", 1}, {"A", 2}}},
		"unknown sku":   {Customer: "c", Items: []OrderItemInput{{"NOPE", 1}}},
	}
	for name, in := range cases {
		_, _, err := s.CreateOrder(context.Background(), in, "")
		var ve *ValidationError
		if !errors.As(err, &ve) || len(ve.Details) == 0 {
			t.Errorf("%s: want ValidationError, got %v", name, err)
		}
	}
}

func TestCancelRestoresStockOnce(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	a := seedProduct(t, s, "A", 1000, 10)
	o, _, err := s.CreateOrder(ctx, OrderInput{Customer: "c", Items: []OrderItemInput{{"A", 4}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetProduct(ctx, a.ID); got.Stock != 10 {
		t.Errorf("stock = %d, want 10", got.Stock)
	}
	if _, err := s.CancelOrder(ctx, o.ID); !errors.Is(err, ErrAlreadyCancelled) {
		t.Errorf("second cancel = %v, want ErrAlreadyCancelled", err)
	}
	if got, _ := s.GetProduct(ctx, a.ID); got.Stock != 10 {
		t.Errorf("stock after second cancel = %d, want 10", got.Stock)
	}
}

func TestDeleteWithHistoryRejected(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	used := seedProduct(t, s, "A", 1000, 3)
	fresh := seedProduct(t, s, "B", 1000, 0)
	if err := s.DeleteProduct(ctx, used.ID); !errors.Is(err, ErrHasHistory) {
		t.Errorf("want ErrHasHistory, got %v", err)
	}
	if err := s.DeleteProduct(ctx, fresh.ID); err != nil {
		t.Errorf("delete fresh: %v", err)
	}
}

func TestConcurrentOrdersNeverOversell(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	const stock, workers = 20, 50
	p := seedProduct(t, s, "HOT", 1000, stock)

	var ok, rejected, other atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := s.CreateOrder(ctx, OrderInput{Customer: "c", Items: []OrderItemInput{{"HOT", 1}}}, "")
			var ins *InsufficientStockError
			switch {
			case err == nil:
				ok.Add(1)
			case errors.As(err, &ins):
				rejected.Add(1)
			default:
				other.Add(1)
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if ok.Load() != stock || rejected.Load() != workers-stock || other.Load() != 0 {
		t.Errorf("ok=%d rejected=%d other=%d", ok.Load(), rejected.Load(), other.Load())
	}
	if got, _ := s.GetProduct(ctx, p.ID); got.Stock != 0 {
		t.Errorf("final stock = %d, want 0", got.Stock)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM sales_orders`); n != stock {
		t.Errorf("orders = %d, want %d", n, stock)
	}
}

func TestConcurrentMultiItemOrders(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	a := seedProduct(t, s, "A", 100, 30)
	b := seedProduct(t, s, "B", 100, 30)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		items := []OrderItemInput{{"A", 1}, {"B", 1}}
		if i%2 == 1 {
			items = []OrderItemInput{{"B", 1}, {"A", 1}}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := s.CreateOrder(ctx, OrderInput{Customer: "c", Items: items}, "")
			var ins *InsufficientStockError
			if err != nil && !errors.As(err, &ins) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	ga, _ := s.GetProduct(ctx, a.ID)
	gb, _ := s.GetProduct(ctx, b.ID)
	if ga.Stock != 0 || gb.Stock != 0 {
		t.Errorf("stock = %d,%d want 0,0", ga.Stock, gb.Stock)
	}
}

func TestIdempotencyKeyReplaysSameOrder(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	p := seedProduct(t, s, "A", 1000, 10)
	in := OrderInput{Customer: "c", Items: []OrderItemInput{{"A", 2}}}

	var wg sync.WaitGroup
	ids := make(chan int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, _, err := s.CreateOrder(ctx, in, "key-1")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- o.ID
		}()
	}
	wg.Wait()
	close(ids)
	first := -1
	for id := range ids {
		if first == -1 {
			first = id
		} else if id != first {
			t.Errorf("got order %d and %d for one key", first, id)
		}
	}
	if got, _ := s.GetProduct(ctx, p.ID); got.Stock != 8 {
		t.Errorf("stock = %d, want 8 (ordered once)", got.Stock)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM sales_orders`); n != 1 {
		t.Errorf("orders = %d, want 1", n)
	}
}

func auditActions(t *testing.T, s *Store, pool *pgxpool.Pool) []string {
	t.Helper()
	if err := s.FlushAudit(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(context.Background(), `SELECT action FROM audit_log ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

func TestAuditRecordsEveryMutation(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	p := seedProduct(t, s, "A", 1000, 0)
	if _, _, err := s.AdjustStock(ctx, p.ID, "in", 5, "restock"); err != nil {
		t.Fatal(err)
	}
	price := 1200
	if _, err := s.UpdateProduct(ctx, p.ID, ProductInput{SKU: "A", Name: "Item A", Price: price}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProduct(ctx, p.ID, ProductInput{SKU: "A", Name: "Item A", Price: price}); err != nil {
		t.Fatal(err)
	}
	o, _, err := s.CreateOrder(ctx, OrderInput{Customer: "c", Items: []OrderItemInput{{"A", 2}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelOrder(ctx, o.ID); err != nil {
		t.Fatal(err)
	}
	empty := seedProduct(t, s, "B", 1, 0)
	if err := s.DeleteProduct(ctx, empty.ID); err != nil {
		t.Fatal(err)
	}
	want := []string{"product.created", "stock.in", "product.updated", "order.created", "order.cancelled", "product.created", "product.deleted"}
	got := auditActions(t, s, pool)
	if len(got) != len(want) {
		t.Fatalf("audit = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("audit = %v, want %v", got, want)
		}
	}
	if err := s.FlushAudit(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListAudit(ctx, AuditParams{EntityType: "order", Page: 1, Limit: 10})
	if err != nil || page.Total != 2 {
		t.Errorf("order audit total = %d, err %v, want 2", page.Total, err)
	}
}

func TestAuditNotWrittenOnRollback(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	a := seedProduct(t, s, "A", 1000, 1)
	before := len(auditActions(t, s, pool))
	if _, _, err := s.AdjustStock(ctx, a.ID, "out", 5, ""); err == nil {
		t.Fatal("expected insufficient stock")
	}
	if _, _, err := s.CreateOrder(ctx, OrderInput{Customer: "c", Items: []OrderItemInput{{"A", 5}}}, ""); err == nil {
		t.Fatal("expected insufficient stock")
	}
	if _, err := s.CreateProduct(ctx, ProductInput{SKU: "A", Name: "dup", Price: 1}); !errors.Is(err, ErrDuplicateSKU) {
		t.Fatalf("want duplicate, got %v", err)
	}
	if n := len(auditActions(t, s, pool)); n != before {
		t.Errorf("audit rows = %d, want %d", n, before)
	}
}

func TestAuditAsyncOrderingAndDrainOnClose(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	p := seedProduct(t, s, "A", 1000, 0)
	for i := 0; i < 30; i++ {
		if _, _, err := s.AdjustStock(ctx, p.ID, "in", 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	var sorted bool
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log WHERE action='stock.in'`).Scan(&n); err != nil || n != 30 {
		t.Fatalf("stock.in entries = %d (err %v), want 30", n, err)
	}
	if err := pool.QueryRow(ctx, `SELECT bool_and(c.created_at <= n.created_at) FROM audit_log c
	  JOIN audit_log n ON n.id = c.id + 1 WHERE c.action='stock.in' AND n.action='stock.in'`).Scan(&sorted); err != nil || !sorted {
		t.Errorf("created_at not monotonic with id (err %v)", err)
	}
}

func TestRenameSKUWithOrderHistory(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	p := seedProduct(t, s, "BP- 001", 1000, 5)
	o, _, err := s.CreateOrder(ctx, OrderInput{Customer: "c", Items: []OrderItemInput{{"BP- 001", 2}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateProduct(ctx, p.ID, ProductInput{SKU: "BP-001", Name: "Item", Price: 1000}); err != nil {
		t.Fatalf("rename failed: %v", err)
	}
	got, err := s.GetOrder(ctx, o.ID)
	if err != nil || len(got.Items) != 1 || got.Items[0].SKU != "BP-001" {
		t.Fatalf("order items after rename = %+v, err %v", got.Items, err)
	}
	if _, err := s.CancelOrder(ctx, o.ID); err != nil {
		t.Fatalf("cancel after rename: %v", err)
	}
}

func TestMinStockDrivesLowStockFilterAndSummary(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	three := 3
	a := seedProduct(t, s, "A", 100, 5)
	b, err := s.CreateProduct(ctx, ProductInput{SKU: "B", Name: "B", Price: 1, MinStock: &three})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AdjustStock(ctx, b.ID, "in", 5, ""); err != nil {
		t.Fatal(err)
	}
	low, err := s.ListProducts(ctx, ListParams{Status: "low", Page: 1, Limit: 10})
	if err != nil || low.Total != 1 || low.Data[0].SKU != "A" {
		t.Fatalf("low = %+v err %v, want only A", low, err)
	}
	if sm, _ := s.Summary(ctx); sm.LowStockCount != 1 {
		t.Errorf("summary low = %d, want 1", sm.LowStockCount)
	}

	nine := 9
	if _, err := s.UpdateProduct(ctx, b.ID, ProductInput{SKU: "B", Name: "B", Price: 1, MinStock: &nine}); err != nil {
		t.Fatal(err)
	}
	if sm, _ := s.Summary(ctx); sm.LowStockCount != 2 {
		t.Errorf("summary low after raise = %d, want 2", sm.LowStockCount)
	}
	_ = a
}

func TestIdempotencyKeyWithDifferentBodyIsRejected(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	seedProduct(t, s, "A", 1000, 10)
	seedProduct(t, s, "B", 1000, 10)
	first := OrderInput{Customer: "c", Items: []OrderItemInput{{"A", 1}, {"B", 2}}}
	if _, _, err := s.CreateOrder(ctx, first, "k"); err != nil {
		t.Fatal(err)
	}

	if _, replayed, err := s.CreateOrder(ctx, OrderInput{Customer: "c", Items: []OrderItemInput{{"B", 2}, {"A", 1}}}, "k"); err != nil || !replayed {
		t.Fatalf("retry: replayed=%v err=%v", replayed, err)
	}

	_, _, err := s.CreateOrder(ctx, OrderInput{Customer: "c", Items: []OrderItemInput{{"A", 5}, {"B", 2}}}, "k")
	if !errors.Is(err, ErrKeyReused) {
		t.Fatalf("want ErrKeyReused, got %v", err)
	}
}

func TestKeysetPaginationWalksAllRowsOnce(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	for _, sku := range []string{"A", "B", "C", "D", "E"} {
		seedProduct(t, s, sku, 1, 0)
	}
	var got []string
	after := 0
	for i := 0; i < 10; i++ {
		page, err := s.ListProducts(ctx, ListParams{Keyset: true, After: after, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range page.Data {
			got = append(got, p.SKU)
		}
		if page.NextCursor == nil {
			break
		}
		after = *page.NextCursor
	}
	if len(got) != 5 || got[0] != "A" || got[4] != "E" {
		t.Fatalf("walked %v, want A..E exactly once", got)
	}
}

func TestBulkCreateIsAllOrNothing(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()
	seedProduct(t, s, "EXIST", 1, 0)
	_, err := s.BulkCreateProducts(ctx, []ProductInput{{SKU: "N1", Name: "n", Price: 1}, {SKU: "EXIST", Name: "dup", Price: 1}})
	var dup *BulkDuplicateError
	if !errors.As(err, &dup) || dup.Index != 1 {
		t.Fatalf("want duplicate at row 1, got %v", err)
	}
	if n := count(t, pool, `SELECT COUNT(*) FROM products WHERE sku = 'N1'`); n != 0 {
		t.Errorf("N1 persisted despite failure")
	}
	made, err := s.BulkCreateProducts(ctx, []ProductInput{{SKU: "N1", Name: "n", Price: 1}, {SKU: "N2", Name: "n", Price: 2}})
	if err != nil || len(made) != 2 {
		t.Fatalf("bulk ok: %v %v", made, err)
	}
	if acts := auditActions(t, s, pool); len(acts) < 3 {
		t.Errorf("audit actions = %v, want bulk rows recorded", acts)
	}
}
