package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"stockflow/internal/store"
)

type API struct{ st Store }

func New(st Store) http.Handler {
	a := &API{st: st}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/products", a.listProducts)
	mux.HandleFunc("POST /api/products", a.createProduct)
	mux.HandleFunc("POST /api/products/bulk", a.bulkCreateProducts)
	mux.HandleFunc("PUT /api/products/{id}", a.updateProduct)
	mux.HandleFunc("DELETE /api/products/{id}", a.deleteProduct)
	mux.HandleFunc("POST /api/products/{id}/stock", a.adjustStock)
	mux.HandleFunc("GET /api/products/{id}/movements", a.movements)
	mux.HandleFunc("GET /api/summary", a.summary)
	mux.HandleFunc("GET /api/orders", a.listOrders)
	mux.HandleFunc("POST /api/orders", a.createOrder)
	mux.HandleFunc("GET /api/orders/{id}", a.getOrder)
	mux.HandleFunc("POST /api/orders/{id}/cancel", a.cancelOrder)
	mux.HandleFunc("GET /api/reports/daily-sales", a.dailySales)
	mux.HandleFunc("GET /api/audit", a.listAudit)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return cors(mux)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Idempotency-Key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type errBody struct {
	Code      string             `json:"code"`
	Message   string             `json:"message"`
	SKU       string             `json:"sku,omitempty"`
	Available *int               `json:"available,omitempty"`
	Details   []store.FieldError `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func writeErr(w http.ResponseWriter, status int, e errBody) {
	writeJSON(w, status, map[string]errBody{"error": e})
}

func validation(w http.ResponseWriter, details ...store.FieldError) {
	writeErr(w, 400, errBody{Code: "VALIDATION_ERROR", Message: "validation failed", Details: details})
}

func handleErr(w http.ResponseWriter, err error) {
	var ins *store.InsufficientStockError
	var val *store.ValidationError
	var dup *store.BulkDuplicateError
	switch {
	case errors.As(err, &ins):
		av := ins.Available
		writeErr(w, 409, errBody{Code: "INSUFFICIENT_STOCK", Message: ins.Error(), SKU: ins.SKU, Available: &av})
	case errors.As(err, &val):
		validation(w, val.Details...)
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, 404, errBody{Code: "NOT_FOUND", Message: "resource not found"})
	case errors.Is(err, store.ErrDuplicateSKU):
		writeErr(w, 409, errBody{Code: "DUPLICATE_SKU", Message: "sku already exists"})
	case errors.Is(err, store.ErrHasHistory):
		writeErr(w, 409, errBody{Code: "PRODUCT_HAS_HISTORY", Message: "product has movement history and cannot be deleted"})
	case errors.Is(err, store.ErrKeyReused):
		writeErr(w, 422, errBody{Code: "IDEMPOTENCY_KEY_REUSED", Message: "this Idempotency-Key was already used with a different request body"})
	case errors.As(err, &dup):
		writeErr(w, 409, errBody{Code: "DUPLICATE_SKU", Message: "sku already exists", Details: []store.FieldError{{Field: fmt.Sprintf("products[%d].sku", dup.Index), Message: fmt.Sprintf("sku %q already exists", dup.SKU)}}})
	case errors.Is(err, store.ErrAlreadyCancelled):
		writeErr(w, 409, errBody{Code: "ORDER_ALREADY_CANCELLED", Message: "order is already cancelled"})
	default:
		log.Printf("internal error: %v", err)
		writeErr(w, 500, errBody{Code: "INTERNAL", Message: "internal server error"})
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeErr(w, 404, errBody{Code: "NOT_FOUND", Message: "resource not found"})
		return 0, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeErr(w, 400, errBody{Code: "INVALID_JSON", Message: "invalid request body: " + err.Error()})
		return false
	}
	return true
}

func intParam(r *http.Request, name string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || v < min {
		return def
	}
	if v > max {
		return max
	}
	return v
}

func (a *API) listProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	if status != "" && status != "active" && status != "inactive" && status != "low" {
		validation(w, store.FieldError{Field: "status", Message: "must be active, inactive or low"})
		return
	}
	res, err := a.st.ListProducts(r.Context(), store.ListParams{
		Search: strings.TrimSpace(q.Get("search")),
		Status: status,
		Page:   intParam(r, "page", 1, 1, 1<<30),
		Limit:  intParam(r, "limit", 20, 1, 100),

		Keyset: q.Has("after"),
		After:  intParam(r, "after", 0, 0, 1<<30),
	})
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func validateProduct(in store.ProductInput) []store.FieldError {
	var errs []store.FieldError
	if strings.TrimSpace(in.SKU) == "" {
		errs = append(errs, store.FieldError{Field: "sku", Message: "sku is required"})
	}
	if strings.TrimSpace(in.Name) == "" {
		errs = append(errs, store.FieldError{Field: "name", Message: "name is required"})
	}
	if in.Price < 0 {
		errs = append(errs, store.FieldError{Field: "price", Message: "price must be >= 0"})
	}
	if in.MinStock != nil && *in.MinStock < 0 {
		errs = append(errs, store.FieldError{Field: "min_stock", Message: "min_stock must be >= 0"})
	}
	return errs
}

func (a *API) bulkCreateProducts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Products []store.ProductInput `json:"products"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.Products) == 0 || len(body.Products) > store.MaxBulkProducts {
		validation(w, store.FieldError{Field: "products", Message: fmt.Sprintf("send between 1 and %d products", store.MaxBulkProducts)})
		return
	}
	var errs []store.FieldError
	seen := map[string]int{}
	for i := range body.Products {
		p := &body.Products[i]
		p.SKU, p.Name = strings.TrimSpace(p.SKU), strings.TrimSpace(p.Name)
		for _, fe := range validateProduct(*p) {
			errs = append(errs, store.FieldError{Field: fmt.Sprintf("products[%d].%s", i, fe.Field), Message: fe.Message})
		}
		if p.SKU != "" {
			if first, dup := seen[p.SKU]; dup {
				errs = append(errs, store.FieldError{Field: fmt.Sprintf("products[%d].sku", i), Message: fmt.Sprintf("duplicate sku %q (also at products[%d])", p.SKU, first)})
			} else {
				seen[p.SKU] = i
			}
		}
	}
	if len(errs) > 0 {
		validation(w, errs...)
		return
	}
	created, err := a.st.BulkCreateProducts(r.Context(), body.Products)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"data": created, "count": len(created)})
}

func (a *API) createProduct(w http.ResponseWriter, r *http.Request) {
	var in store.ProductInput
	if !decode(w, r, &in) {
		return
	}
	in.SKU, in.Name = strings.TrimSpace(in.SKU), strings.TrimSpace(in.Name)
	if errs := validateProduct(in); len(errs) > 0 {
		validation(w, errs...)
		return
	}
	p, err := a.st.CreateProduct(r.Context(), in)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 201, p)
}

func (a *API) updateProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in store.ProductInput
	if !decode(w, r, &in) {
		return
	}
	in.SKU, in.Name = strings.TrimSpace(in.SKU), strings.TrimSpace(in.Name)
	if errs := validateProduct(in); len(errs) > 0 {
		validation(w, errs...)
		return
	}
	p, err := a.st.UpdateProduct(r.Context(), id, in)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (a *API) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.st.DeleteProduct(r.Context(), id); err != nil {
		handleErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type stockReq struct {
	Type string `json:"type"`
	Qty  int    `json:"qty"`
	Note string `json:"note"`
}

func (a *API) adjustStock(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in stockReq
	if !decode(w, r, &in) {
		return
	}
	var errs []store.FieldError
	if in.Type != "in" && in.Type != "out" {
		errs = append(errs, store.FieldError{Field: "type", Message: `type must be "in" or "out"`})
	}
	if in.Qty <= 0 {
		errs = append(errs, store.FieldError{Field: "qty", Message: "qty must be an integer greater than 0"})
	}
	if len(errs) > 0 {
		validation(w, errs...)
		return
	}
	p, mv, err := a.st.AdjustStock(r.Context(), id, in.Type, in.Qty, strings.TrimSpace(in.Note))
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"product": p, "movement": mv})
}

func (a *API) movements(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ms, err := a.st.ListMovements(r.Context(), id)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": ms})
}

func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	s, err := a.st.Summary(r.Context())
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, s)
}

func (a *API) listOrders(w http.ResponseWriter, r *http.Request) {
	os, err := a.st.ListOrders(r.Context(), intParam(r, "limit", 20, 1, 100))
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"data": os})
}

func (a *API) createOrder(w http.ResponseWriter, r *http.Request) {
	var in store.OrderInput
	if !decode(w, r, &in) {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	o, replayed, err := a.st.CreateOrder(r.Context(), in, key)
	if err != nil {
		handleErr(w, err)
		return
	}
	if replayed {
		w.Header().Set("Idempotent-Replay", "true")
	}
	writeJSON(w, 201, o)
}

func (a *API) getOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	o, err := a.st.GetOrder(r.Context(), id)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, o)
}

func (a *API) cancelOrder(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	o, err := a.st.CancelOrder(r.Context(), id)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, o)
}

func (a *API) dailySales(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		loc, err := time.LoadLocation("Asia/Jakarta")
		if err != nil {
			loc = time.UTC
		}
		date = time.Now().In(loc).Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		validation(w, store.FieldError{Field: "date", Message: "date must be YYYY-MM-DD"})
		return
	}
	d, err := a.st.DailySales(r.Context(), date)
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, d)
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	et := r.URL.Query().Get("entity")
	if et != "" && et != "product" && et != "order" {
		validation(w, store.FieldError{Field: "entity", Message: "must be product or order"})
		return
	}
	res, err := a.st.ListAudit(r.Context(), store.AuditParams{
		EntityType: et,
		EntityID:   intParam(r, "entity_id", 0, 0, 1<<30),
		Page:       intParam(r, "page", 1, 1, 1<<30),
		Limit:      intParam(r, "limit", 20, 1, 100),
	})
	if err != nil {
		handleErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}
