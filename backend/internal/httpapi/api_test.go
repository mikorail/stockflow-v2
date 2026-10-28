package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"stockflow/internal/httpapi"
	"stockflow/internal/httpapi/mocks"
	"stockflow/internal/store"
)

func do(t *testing.T, st httpapi.Store, method, path, body string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	httpapi.New(st).ServeHTTP(rec, req)
	return rec
}

type errEnvelope struct {
	Error struct {
		Code      string             `json:"code"`
		SKU       string             `json:"sku"`
		Available *int               `json:"available"`
		Details   []store.FieldError `json:"details"`
	} `json:"error"`
}

func decodeErr(t *testing.T, rec *httptest.ResponseRecorder) errEnvelope {
	t.Helper()
	var e errEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &e))
	return e
}

func TestInsufficientStockOnOrderIs409WithSKUAndAvailable(t *testing.T) {
	m := mocks.NewMockStore(t)
	m.EXPECT().CreateOrder(mock.Anything, mock.Anything, "").
		Return(store.Order{}, false, &store.InsufficientStockError{SKU: "HS-006", Available: 5})
	rec := do(t, m, "POST", "/api/orders", `{"customer":"c","items":[{"sku":"HS-006","qty":9}]}`)
	assert.Equal(t, 409, rec.Code)
	e := decodeErr(t, rec)
	assert.Equal(t, "INSUFFICIENT_STOCK", e.Error.Code)
	assert.Equal(t, "HS-006", e.Error.SKU)
	require.NotNil(t, e.Error.Available)
	assert.Equal(t, 5, *e.Error.Available)
}

func TestInsufficientStockOnAdjustIs409(t *testing.T) {
	m := mocks.NewMockStore(t)
	m.EXPECT().AdjustStock(mock.Anything, 1, "out", 9, "").
		Return(store.Product{}, store.Movement{}, &store.InsufficientStockError{SKU: "A", Available: 2})
	rec := do(t, m, "POST", "/api/products/1/stock", `{"type":"out","qty":9}`)
	assert.Equal(t, 409, rec.Code)
	assert.Equal(t, "INSUFFICIENT_STOCK", decodeErr(t, rec).Error.Code)
}

func TestStockBodyValidationNeverReachesStore(t *testing.T) {
	m := mocks.NewMockStore(t)
	rec := do(t, m, "POST", "/api/products/1/stock", `{"type":"sideways","qty":0}`)
	assert.Equal(t, 400, rec.Code)
	e := decodeErr(t, rec)
	assert.Equal(t, "VALIDATION_ERROR", e.Error.Code)
	assert.Len(t, e.Error.Details, 2)
}

func TestCreateOrderPassesIdempotencyKeyAndReplayHeader(t *testing.T) {
	m := mocks.NewMockStore(t)
	m.EXPECT().CreateOrder(mock.Anything, mock.Anything, "k-1").Return(store.Order{ID: 7, Status: "CREATED"}, true, nil)
	rec := do(t, m, "POST", "/api/orders", `{"customer":"c","items":[{"sku":"A","qty":1}]}`, "Idempotency-Key", "k-1")
	assert.Equal(t, 201, rec.Code)
	assert.Equal(t, "true", rec.Header().Get("Idempotent-Replay"))
}

func TestStoreValidationErrorBecomes400WithDetails(t *testing.T) {
	m := mocks.NewMockStore(t)
	m.EXPECT().CreateOrder(mock.Anything, mock.Anything, "").
		Return(store.Order{}, false, &store.ValidationError{Details: []store.FieldError{{Field: "items[0].sku", Message: `unknown sku "X"`}}})
	rec := do(t, m, "POST", "/api/orders", `{"customer":"c","items":[{"sku":"X","qty":1}]}`)
	assert.Equal(t, 400, rec.Code)
	assert.Equal(t, "items[0].sku", decodeErr(t, rec).Error.Details[0].Field)
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		call   func(m *mocks.MockStore) *httptest.ResponseRecorder
		status int
		code   string
	}{
		{"update missing product", func(m *mocks.MockStore) *httptest.ResponseRecorder {
			m.EXPECT().UpdateProduct(mock.Anything, 9, mock.Anything).Return(store.Product{}, store.ErrNotFound)
			return do(t, m, "PUT", "/api/products/9", `{"sku":"A","name":"n","price":1}`)
		}, 404, "NOT_FOUND"},
		{"delete product with history", func(m *mocks.MockStore) *httptest.ResponseRecorder {
			m.EXPECT().DeleteProduct(mock.Anything, 1).Return(store.ErrHasHistory)
			return do(t, m, "DELETE", "/api/products/1", "")
		}, 409, "PRODUCT_HAS_HISTORY"},
		{"duplicate sku", func(m *mocks.MockStore) *httptest.ResponseRecorder {
			m.EXPECT().CreateProduct(mock.Anything, mock.Anything).Return(store.Product{}, store.ErrDuplicateSKU)
			return do(t, m, "POST", "/api/products", `{"sku":"A","name":"n","price":1}`)
		}, 409, "DUPLICATE_SKU"},
		{"cancel twice", func(m *mocks.MockStore) *httptest.ResponseRecorder {
			m.EXPECT().CancelOrder(mock.Anything, 3).Return(store.Order{}, store.ErrAlreadyCancelled)
			return do(t, m, "POST", "/api/orders/3/cancel", "")
		}, 409, "ORDER_ALREADY_CANCELLED"},
		{"unexpected error is a 500 without leaking details", func(m *mocks.MockStore) *httptest.ResponseRecorder {
			m.EXPECT().Summary(mock.Anything).Return(store.Summary{}, assert.AnError)
			return do(t, m, "GET", "/api/summary", "")
		}, 500, "INTERNAL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := c.call(mocks.NewMockStore(t))
			assert.Equal(t, c.status, rec.Code)
			assert.Equal(t, c.code, decodeErr(t, rec).Error.Code)
			assert.NotContains(t, rec.Body.String(), assert.AnError.Error())
		})
	}
}

func TestCreateProductValidationAndStatusCodes(t *testing.T) {
	m := mocks.NewMockStore(t)
	assert.Equal(t, 400, do(t, m, "POST", "/api/products", `{"sku":"","name":"","price":-1}`).Code)
	assert.Equal(t, 400, do(t, m, "POST", "/api/products", `{"sku":"A","name":"n","price":10.5}`).Code, "float price rejected")
	assert.Equal(t, 400, do(t, m, "POST", "/api/products", `{"sku":"A","name":"n","price":1,"bogus":1}`).Code, "unknown field rejected")

	m.EXPECT().CreateProduct(mock.Anything, mock.Anything).Return(store.Product{ID: 1, SKU: "A"}, nil)
	assert.Equal(t, 201, do(t, m, "POST", "/api/products", `{"sku":"A","name":"n","price":1}`).Code)
}

func TestListProductsParsesQueryAndRejectsBadStatus(t *testing.T) {
	m := mocks.NewMockStore(t)
	m.EXPECT().ListProducts(mock.Anything, store.ListParams{Search: "mouse", Status: "low", Page: 2, Limit: 100}).
		Return(store.ProductList{Data: []store.Product{}, Page: 2, Limit: 100}, nil)
	assert.Equal(t, 200, do(t, m, "GET", "/api/products?search=mouse&status=low&page=2&limit=5000", "").Code, "limit is capped at 100")
	assert.Equal(t, 400, do(t, m, "GET", "/api/products?status=weird", "").Code)
}

func TestAuditEndpointFiltersAndValidates(t *testing.T) {
	m := mocks.NewMockStore(t)
	m.EXPECT().ListAudit(mock.Anything, store.AuditParams{EntityType: "order", Page: 1, Limit: 20}).
		Return(store.AuditPage{Data: []store.AuditEntry{}, Page: 1, Limit: 20}, nil)
	assert.Equal(t, 200, do(t, m, "GET", "/api/audit?entity=order", "").Code)
	assert.Equal(t, 400, do(t, m, "GET", "/api/audit?entity=users", "").Code)
}

func TestBadIDIs404AndDailySalesDateValidated(t *testing.T) {
	m := mocks.NewMockStore(t)
	assert.Equal(t, http.StatusNotFound, do(t, m, "GET", "/api/orders/abc", "").Code)
	assert.Equal(t, 400, do(t, m, "GET", "/api/reports/daily-sales?date=nope", "").Code)
}

func TestReusedIdempotencyKeyIs422(t *testing.T) {
	m := mocks.NewMockStore(t)
	m.EXPECT().CreateOrder(mock.Anything, mock.Anything, "k").Return(store.Order{}, false, store.ErrKeyReused)
	rec := do(t, m, "POST", "/api/orders", `{"customer":"c","items":[{"sku":"A","qty":1}]}`, "Idempotency-Key", "k")
	assert.Equal(t, 422, rec.Code)
	assert.Equal(t, "IDEMPOTENCY_KEY_REUSED", decodeErr(t, rec).Error.Code)
}

func TestBulkCreateValidatesBeforeTouchingStore(t *testing.T) {
	m := mocks.NewMockStore(t)
	rec := do(t, m, "POST", "/api/products/bulk", `{"products":[{"sku":"A","name":"n","price":1},{"sku":"A","name":"","price":-1,"min_stock":-2}]}`)
	assert.Equal(t, 400, rec.Code)
	fields := []string{}
	for _, d := range decodeErr(t, rec).Error.Details {
		fields = append(fields, d.Field)
	}
	assert.ElementsMatch(t, []string{"products[1].name", "products[1].price", "products[1].min_stock", "products[1].sku"}, fields)
	assert.Equal(t, 400, do(t, m, "POST", "/api/products/bulk", `{"products":[]}`).Code)
}

func TestBulkDuplicateIs409WithRowIndex(t *testing.T) {
	m := mocks.NewMockStore(t)
	m.EXPECT().BulkCreateProducts(mock.Anything, mock.Anything).Return(nil, &store.BulkDuplicateError{Index: 1, SKU: "EXIST"})
	rec := do(t, m, "POST", "/api/products/bulk", `{"products":[{"sku":"N","name":"n","price":1},{"sku":"EXIST","name":"n","price":1}]}`)
	assert.Equal(t, 409, rec.Code)
	e := decodeErr(t, rec)
	assert.Equal(t, "DUPLICATE_SKU", e.Error.Code)
	assert.Equal(t, "products[1].sku", e.Error.Details[0].Field)
}

func TestKeysetParamSwitchesPaginationMode(t *testing.T) {
	m := mocks.NewMockStore(t)
	m.EXPECT().ListProducts(mock.Anything, store.ListParams{Page: 1, Limit: 2, Keyset: true, After: 7}).
		Return(store.ProductList{Data: []store.Product{}, Limit: 2}, nil)
	assert.Equal(t, 200, do(t, m, "GET", "/api/products?after=7&limit=2", "").Code)
}
