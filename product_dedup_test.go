package stripeflow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stripe/stripe-go/v82"
)

// --------------------------------------------------------------------------
// Fake Stripe backend
//
// CreateProduct/CreatePrice/ProvisionProduct call the real Stripe network
// API, so exercising their dedup behavior needs something to talk to. This
// fake implements just enough of the Products and Prices APIs (create, get,
// list-by-lookup_key) to drive the tests below, wired in via
// stripe.SetBackend the same way the SDK expects any Backend implementation.
// --------------------------------------------------------------------------

type fakeStripeBackend struct {
	mu            sync.Mutex
	products      map[string]map[string]any
	prices        map[string]map[string]any
	priceByLookup map[string]string
	priceSeq      int
	meters        map[string]map[string]any
	meterSeq      int

	ProductPOSTs int
	PricePOSTs   int
	MeterPOSTs   int
}

func newFakeStripeBackend(t *testing.T) *fakeStripeBackend {
	t.Helper()

	fb := &fakeStripeBackend{
		products:      map[string]map[string]any{},
		prices:        map[string]map[string]any{},
		priceByLookup: map[string]string{},
		meters:        map[string]map[string]any{},
	}

	server := httptest.NewServer(http.HandlerFunc(fb.handle))
	t.Cleanup(server.Close)

	prevBackend := stripe.GetBackend(stripe.APIBackend)
	fakeBackend := stripe.GetBackendWithConfig(stripe.APIBackend, &stripe.BackendConfig{
		URL:               stripe.String(server.URL),
		MaxNetworkRetries: stripe.Int64(0),
	})
	stripe.SetBackend(stripe.APIBackend, fakeBackend)
	t.Cleanup(func() { stripe.SetBackend(stripe.APIBackend, prevBackend) })

	return fb
}

func (fb *fakeStripeBackend) handle(w http.ResponseWriter, r *http.Request) {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/products":
		fb.createProduct(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/products/"):
		fb.getProduct(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/prices":
		fb.createPrice(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/prices":
		fb.listPrices(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/billing/meters":
		fb.createMeter(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/v1/billing/meters":
		fb.listMeters(w, r)
	default:
		http.Error(w, fmt.Sprintf("fake stripe backend: unhandled %s %s", r.Method, r.URL.Path), http.StatusNotImplemented)
	}
}

func (fb *fakeStripeBackend) createProduct(w http.ResponseWriter, r *http.Request) {
	fb.ProductPOSTs++

	id := r.Form.Get("id")
	if id == "" {
		id = fmt.Sprintf("prod_auto_%d", len(fb.products)+1)
	}
	if _, exists := fb.products[id]; exists {
		writeStripeError(w, http.StatusBadRequest, stripe.ErrorTypeInvalidRequest, stripe.ErrorCodeResourceAlreadyExists, "product already exists")
		return
	}

	p := map[string]any{
		"id":          id,
		"object":      "product",
		"active":      true,
		"name":        r.Form.Get("name"),
		"description": r.Form.Get("description"),
		"created":     1700000000,
	}
	fb.products[id] = p
	writeJSON(w, http.StatusOK, p)
}

func (fb *fakeStripeBackend) getProduct(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/products/")
	p, ok := fb.products[id]
	if !ok {
		writeStripeError(w, http.StatusNotFound, stripe.ErrorTypeInvalidRequest, "resource_missing", "no such product")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (fb *fakeStripeBackend) createPrice(w http.ResponseWriter, r *http.Request) {
	fb.PricePOSTs++
	fb.priceSeq++

	id := fmt.Sprintf("price_auto_%d", fb.priceSeq)
	lookupKey := r.Form.Get("lookup_key")
	unitAmount, _ := strconv.ParseInt(r.Form.Get("unit_amount"), 10, 64)

	p := map[string]any{
		"id":          id,
		"object":      "price",
		"active":      true,
		"currency":    r.Form.Get("currency"),
		"unit_amount": unitAmount,
		"lookup_key":  lookupKey,
		"nickname":    r.Form.Get("nickname"),
		"type":        "one_time",
		"created":     1700000000,
	}
	if interval := r.Form.Get("recurring[interval]"); interval != "" {
		p["type"] = "recurring"
		p["recurring"] = map[string]any{
			"interval":       interval,
			"interval_count": 1,
			"usage_type":     "licensed",
		}
	}

	fb.prices[id] = p
	if lookupKey != "" {
		fb.priceByLookup[lookupKey] = id
	}
	writeJSON(w, http.StatusOK, p)
}

func (fb *fakeStripeBackend) listPrices(w http.ResponseWriter, r *http.Request) {
	lookupKey := r.URL.Query().Get("lookup_keys[0]")

	data := []any{}
	if lookupKey != "" {
		if id, ok := fb.priceByLookup[lookupKey]; ok {
			data = append(data, fb.prices[id])
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object":   "list",
		"url":      "/v1/prices",
		"has_more": false,
		"data":     data,
	})
}

func (fb *fakeStripeBackend) createMeter(w http.ResponseWriter, r *http.Request) {
	fb.MeterPOSTs++
	fb.meterSeq++

	id := fmt.Sprintf("mtr_test_auto_%d", fb.meterSeq)
	m := map[string]any{
		"id":           id,
		"object":       "billing.meter",
		"status":       "active",
		"display_name": r.Form.Get("display_name"),
		"event_name":   r.Form.Get("event_name"),
		"created":      1700000000,
	}
	fb.meters[id] = m
	writeJSON(w, http.StatusOK, m)
}

func (fb *fakeStripeBackend) listMeters(w http.ResponseWriter, r *http.Request) {
	data := []any{}
	for _, m := range fb.meters {
		data = append(data, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object":   "list",
		"url":      "/v1/billing/meters",
		"has_more": false,
		"data":     data,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeStripeError(w http.ResponseWriter, status int, errType stripe.ErrorType, code stripe.ErrorCode, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"type":    string(errType),
			"code":    string(code),
			"message": message,
		},
	})
}

// --------------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------------

func TestSlugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"My SaaS", "my-saas"},
		{"  My   SaaS  ", "my-saas"},
		{"Pro Plan!!", "pro-plan"},
		{"my-saas", "my-saas"},
		{"Ünïcode Name", "n-code-name"},
	}
	for _, c := range cases {
		if got := slugify(c.in); got != c.want {
			t.Errorf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPriceLookupKey(t *testing.T) {
	k1 := priceLookupKey("prod_x", "Monthly", "usd", 2999, "month")
	k2 := priceLookupKey("prod_x", "Monthly", "usd", 2999, "month")
	if k1 != k2 {
		t.Fatalf("priceLookupKey is not deterministic: %q != %q", k1, k2)
	}

	k3 := priceLookupKey("prod_x", "", "usd", 2999, "month")
	if k1 == k3 {
		t.Fatalf("expected different lookup keys for a named vs unnamed price")
	}

	k4 := priceLookupKey("prod_x", "", "usd", 2999, "month")
	if k3 != k4 {
		t.Fatalf("priceLookupKey fallback is not deterministic: %q != %q", k3, k4)
	}
}

func TestCreateProduct_Dedup(t *testing.T) {
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()
	sf := newTestClient(t, db, SQLite)
	fb := newFakeStripeBackend(t)
	ctx := context.Background()

	p1, err := sf.CreateProduct(ctx, CreateProductParams{Name: "My SaaS"})
	if err != nil {
		t.Fatalf("first CreateProduct: %v", err)
	}
	p2, err := sf.CreateProduct(ctx, CreateProductParams{Name: "My SaaS"})
	if err != nil {
		t.Fatalf("second CreateProduct: %v", err)
	}

	if p1.ID != p2.ID {
		t.Fatalf("expected the same product ID on repeat create, got %q and %q", p1.ID, p2.ID)
	}
	if fb.ProductPOSTs != 2 {
		t.Fatalf("expected 2 POST /v1/products calls (1 create + 1 resource_already_exists retry), got %d", fb.ProductPOSTs)
	}

	products, err := sf.ListProducts(ctx, true)
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected exactly 1 locally stored product, got %d", len(products))
	}
}

func TestCreatePrice_Dedup(t *testing.T) {
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()
	sf := newTestClient(t, db, SQLite)
	fb := newFakeStripeBackend(t)
	ctx := context.Background()

	prod, err := sf.CreateProduct(ctx, CreateProductParams{Name: "My SaaS"})
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}

	priceParams := CreatePriceParams{
		StripeProductID: prod.ID,
		UnitAmount:      2999,
		Currency:        "usd",
		Recurring:       &RecurringParams{Interval: IntervalMonth},
	}

	pr1, err := sf.CreatePrice(ctx, priceParams)
	if err != nil {
		t.Fatalf("first CreatePrice: %v", err)
	}
	pr2, err := sf.CreatePrice(ctx, priceParams)
	if err != nil {
		t.Fatalf("second CreatePrice: %v", err)
	}

	if pr1.ID != pr2.ID {
		t.Fatalf("expected the same price ID on repeat create, got %q and %q", pr1.ID, pr2.ID)
	}
	if fb.PricePOSTs != 1 {
		t.Fatalf("expected exactly 1 POST /v1/prices call (second call should reuse via lookup_key), got %d", fb.PricePOSTs)
	}
}

func TestProvisionProduct_Dedup(t *testing.T) {
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()
	sf := newTestClient(t, db, SQLite)
	fb := newFakeStripeBackend(t)
	ctx := context.Background()

	params := ProvisionParams{
		Product: ProvisionProductParams{Name: "My SaaS"},
		Prices: []ProvisionPriceParams{
			{
				Nickname:   "Monthly",
				Currency:   "usd",
				UnitAmount: 2999,
				Recurring:  &ProvisionRecurringParams{Interval: "month"},
			},
		},
	}

	res1, err := sf.ProvisionProduct(ctx, params)
	if err != nil {
		t.Fatalf("first ProvisionProduct: %v", err)
	}
	res2, err := sf.ProvisionProduct(ctx, params)
	if err != nil {
		t.Fatalf("second ProvisionProduct: %v", err)
	}

	if res1.ProductID != res2.ProductID {
		t.Fatalf("expected the same product ID on repeat provisioning, got %q and %q", res1.ProductID, res2.ProductID)
	}
	if len(res1.Prices) != 1 || len(res2.Prices) != 1 {
		t.Fatalf("expected 1 price in each result, got %d and %d", len(res1.Prices), len(res2.Prices))
	}
	if res1.Prices[0].PriceID != res2.Prices[0].PriceID {
		t.Fatalf("expected the same price ID on repeat provisioning, got %q and %q", res1.Prices[0].PriceID, res2.Prices[0].PriceID)
	}

	if fb.ProductPOSTs != 2 {
		t.Fatalf("expected 2 POST /v1/products calls (1 create + 1 resource_already_exists retry), got %d", fb.ProductPOSTs)
	}
	if fb.PricePOSTs != 1 {
		t.Fatalf("expected exactly 1 POST /v1/prices call (second run should reuse via lookup_key), got %d", fb.PricePOSTs)
	}
}

func TestCreateMeter_Dedup(t *testing.T) {
	db := setupTestDB(t, "sqlite", ":memory:")
	defer db.Close()
	sf := newTestClient(t, db, SQLite)
	fb := newFakeStripeBackend(t)
	ctx := context.Background()

	params := ProvisionParams{
		Product: ProvisionProductParams{Name: "My SaaS"},
		Prices: []ProvisionPriceParams{
			{
				Nickname:   "Starter overage",
				Currency:   "usd",
				UnitAmount: 8,
				Recurring: &ProvisionRecurringParams{
					Interval:       "month",
					UsageType:      "metered",
					MeterEventName: "check",
				},
			},
			{
				Nickname:   "Pro overage",
				Currency:   "usd",
				UnitAmount: 4,
				Recurring: &ProvisionRecurringParams{
					Interval:       "month",
					UsageType:      "metered",
					MeterEventName: "check",
				},
			},
		},
	}

	res1, err := sf.ProvisionProduct(ctx, params)
	if err != nil {
		t.Fatalf("first ProvisionProduct: %v", err)
	}
	if fb.MeterPOSTs != 1 {
		t.Fatalf("expected exactly 1 POST /v1/billing/meters call for 2 prices sharing one event_name, got %d", fb.MeterPOSTs)
	}

	res2, err := sf.ProvisionProduct(ctx, params)
	if err != nil {
		t.Fatalf("second ProvisionProduct: %v", err)
	}
	if fb.MeterPOSTs != 1 {
		t.Fatalf("expected meter to be reused on repeat provisioning, got %d POST /v1/billing/meters calls", fb.MeterPOSTs)
	}

	if len(res1.Prices) != 2 || len(res2.Prices) != 2 {
		t.Fatalf("expected 2 prices in each result, got %d and %d", len(res1.Prices), len(res2.Prices))
	}
}
