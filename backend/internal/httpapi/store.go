package httpapi

import (
	"context"

	"stockflow/internal/store"
)

type Store interface {
	ListProducts(ctx context.Context, lp store.ListParams) (store.ProductList, error)
	GetProduct(ctx context.Context, id int) (store.Product, error)
	CreateProduct(ctx context.Context, in store.ProductInput) (store.Product, error)
	UpdateProduct(ctx context.Context, id int, in store.ProductInput) (store.Product, error)
	DeleteProduct(ctx context.Context, id int) error
	BulkCreateProducts(ctx context.Context, in []store.ProductInput) ([]store.Product, error)
	AdjustStock(ctx context.Context, id int, typ string, qty int, note string) (store.Product, store.Movement, error)
	ListMovements(ctx context.Context, productID int) ([]store.Movement, error)
	Summary(ctx context.Context) (store.Summary, error)
	CreateOrder(ctx context.Context, in store.OrderInput, idemKey string) (store.Order, bool, error)
	GetOrder(ctx context.Context, id int) (store.Order, error)
	ListOrders(ctx context.Context, limit int) ([]store.Order, error)
	CancelOrder(ctx context.Context, id int) (store.Order, error)
	DailySales(ctx context.Context, date string) (store.DailySales, error)
	ListAudit(ctx context.Context, ap store.AuditParams) (store.AuditPage, error)
}
