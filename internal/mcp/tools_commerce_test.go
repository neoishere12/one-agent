package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/types"
)

func TestSearchProductPartialFailure(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: &mockPlatform{
			searchProducts: []types.Product{{ID: "p1", Name: "Lassi", PriceRupees: 28, Platform: types.PlatformBlinkit, InStock: true}},
		},
		types.PlatformZepto: &mockPlatform{searchErr: errors.New("session missing")},
	})

	raw := mustRaw(t, searchProductInput{
		Query: "lassi", Apps: []string{"blinkit", "zepto"}, Latitude: 18.5, Longitude: 73.8,
	})
	result, err := server.call(context.Background(), "search_product", raw)
	if err != nil {
		t.Fatalf("search_product returned error: %v", err)
	}

	out := result.(searchProductOutput)
	if len(out.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(out.Results))
	}
	if out.Results[0].App != "blinkit" {
		t.Errorf("App: got %q", out.Results[0].App)
	}
	if out.Errors["zepto"] == "" {
		t.Error("expected zepto failure entry")
	}
}

func TestComparePricesRanksAscending(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: &mockPlatform{
			searchProducts: []types.Product{{ID: "b", Name: "Lassi B", PriceRupees: 30}},
		},
		types.PlatformZepto: &mockPlatform{
			searchProducts: []types.Product{{ID: "z", Name: "Lassi Z", PriceRupees: 25}},
		},
		types.PlatformInstamart: &mockPlatform{
			searchProducts: []types.Product{{ID: "i", Name: "Lassi I", PriceRupees: 40}},
		},
	})

	raw := mustRaw(t, comparePricesInput{Query: "lassi", Latitude: 18.5, Longitude: 73.8})
	result, err := server.call(context.Background(), "compare_prices", raw)
	if err != nil {
		t.Fatalf("compare_prices returned error: %v", err)
	}

	out := result.(comparePricesOutput)
	if len(out.Ranked) != 3 {
		t.Fatalf("expected 3 ranked entries, got %d", len(out.Ranked))
	}
	if out.Ranked[0].App != "zepto" {
		t.Errorf("rank 1 app: got %q", out.Ranked[0].App)
	}
}

func TestGetSavedAddresses(t *testing.T) {
	st := newTestStore(t)
	seedSession(t, st, defaultSession(types.PlatformBlinkit))
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	raw := mustRaw(t, getSavedAddressesInput{App: "blinkit"})
	result, err := server.call(context.Background(), "get_saved_addresses", raw)
	if err != nil {
		t.Fatalf("get_saved_addresses failed: %v", err)
	}

	out := result.(getSavedAddressesOutput)
	if len(out.Addresses) != 1 {
		t.Fatalf("expected 1 address, got %d", len(out.Addresses))
	}
	if out.Addresses[0].FullAddress == "" {
		t.Error("full_address should not be empty")
	}
}

func TestGetSavedPaymentMethods(t *testing.T) {
	st := newTestStore(t)
	seedSession(t, st, defaultSession(types.PlatformBlinkit))
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	raw := mustRaw(t, getSavedPaymentMethodsInput{App: "blinkit"})
	result, err := server.call(context.Background(), "get_saved_payment_methods", raw)
	if err != nil {
		t.Fatalf("get_saved_payment_methods failed: %v", err)
	}
	out := result.(getSavedPaymentMethodsOutput)
	if len(out.SavedMethods) != 1 {
		t.Fatalf("expected 1 saved method, got %d", len(out.SavedMethods))
	}
	if len(out.ExtraModes) != 2 {
		t.Fatalf("expected 2 extra modes, got %d", len(out.ExtraModes))
	}
}

func TestPlaceOrderRequiresConfirm(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{})

	raw := mustRaw(t, placeOrderInput{App: "blinkit", ProductID: "p1", AddressID: "a1", PaymentToken: "t1"})
	_, err := server.call(context.Background(), "place_order", raw)
	if err == nil {
		t.Fatal("expected invalid params error")
	}
}

func TestPlaceOrderSuccess(t *testing.T) {
	st := newTestStore(t)
	session := defaultSession(types.PlatformBlinkit)
	seedSession(t, st, session)
	client := &mockPlatform{
		addCartID: "cart-1",
		checkoutResult: platforms.CheckoutResult{
			CheckoutID: "checkout-1", ETAMinutes: 12,
		},
		payOrder: types.Order{
			ID: "BLK-123", Status: "confirmed", ETAMinutes: 10, TotalRupees: 28, PlacedAt: time.Now(),
		},
	}
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: client,
	})

	raw := mustRaw(t, placeOrderInput{
		App: "blinkit", ProductID: "prod-1", AddressID: "addr-home", PaymentToken: "token-4242", Quantity: 1, Confirm: true,
	})
	result, err := server.call(context.Background(), "place_order", raw)
	if err != nil {
		t.Fatalf("place_order failed: %v", err)
	}

	out := result.(placeOrderOutput)
	if out.OrderID != "BLK-123" {
		t.Errorf("OrderID: got %q", out.OrderID)
	}
	if out.App != "blinkit" {
		t.Errorf("App: got %q", out.App)
	}
	if client.lastPayToken != "token-4242" {
		t.Errorf("lastPayToken: got %q", client.lastPayToken)
	}
	if out.PaymentMode != paymentModeSaved {
		t.Errorf("PaymentMode: got %q", out.PaymentMode)
	}
}

func TestPlaceOrderResolvesPaymentID(t *testing.T) {
	st := newTestStore(t)
	session := defaultSession(types.PlatformBlinkit)
	seedSession(t, st, session)
	client := &mockPlatform{
		addCartID: "cart-1",
		checkoutResult: platforms.CheckoutResult{
			CheckoutID: "checkout-1", ETAMinutes: 12,
		},
		payOrder: types.Order{
			ID: "BLK-124", Status: "confirmed", ETAMinutes: 10, TotalRupees: 28, PlacedAt: time.Now(),
		},
	}
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: client,
	})
	raw := mustRaw(t, placeOrderInput{
		App: "blinkit", ProductID: "prod-1", AddressID: "addr-home", PaymentID: "pay-card", Confirm: true,
	})
	_, err := server.call(context.Background(), "place_order", raw)
	if err != nil {
		t.Fatalf("place_order failed: %v", err)
	}
	if client.lastPayToken != "token-4242" {
		t.Errorf("lastPayToken from payment_id: got %q", client.lastPayToken)
	}
}

func TestPlaceOrderCODMode(t *testing.T) {
	st := newTestStore(t)
	session := defaultSession(types.PlatformBlinkit)
	seedSession(t, st, session)
	client := &mockPlatform{
		addCartID: "cart-1",
		checkoutResult: platforms.CheckoutResult{
			CheckoutID: "checkout-1", ETAMinutes: 12,
		},
		payOrder: types.Order{
			ID: "BLK-125", Status: "confirmed", ETAMinutes: 10, TotalRupees: 28, PlacedAt: time.Now(),
		},
	}
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: client,
	})
	raw := mustRaw(t, placeOrderInput{
		App: "blinkit", ProductID: "prod-1", AddressID: "addr-home", PaymentMode: paymentModeCOD, Confirm: true,
	})
	result, err := server.call(context.Background(), "place_order", raw)
	if err != nil {
		t.Fatalf("place_order failed: %v", err)
	}
	out := result.(placeOrderOutput)
	if client.lastPayToken != paymentModeCOD {
		t.Errorf("cod token: got %q", client.lastPayToken)
	}
	if out.PaymentMode != paymentModeCOD {
		t.Errorf("PaymentMode: got %q", out.PaymentMode)
	}
}

func TestPlaceOrderUPIIntentMode(t *testing.T) {
	st := newTestStore(t)
	session := defaultSession(types.PlatformBlinkit)
	seedSession(t, st, session)
	client := &mockPlatform{
		addCartID: "cart-1",
		checkoutResult: platforms.CheckoutResult{
			CheckoutID: "checkout-1", ETAMinutes: 12,
		},
		payOrder: types.Order{
			ID: "BLK-126", Status: "pending_payment", ETAMinutes: 10, TotalRupees: 28, PlacedAt: time.Now(),
		},
	}
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: client,
	})
	raw := mustRaw(t, placeOrderInput{
		App: "blinkit", ProductID: "prod-1", AddressID: "addr-home", PaymentMode: paymentModeUPIIntent, Confirm: true,
	})
	result, err := server.call(context.Background(), "place_order", raw)
	if err != nil {
		t.Fatalf("place_order failed: %v", err)
	}
	out := result.(placeOrderOutput)
	if client.lastPayToken != paymentModeUPIIntent {
		t.Errorf("upi_intent token: got %q", client.lastPayToken)
	}
	if !out.UPIIntentRequired {
		t.Error("UPIIntentRequired: expected true")
	}
}

func TestGetOrderStatusSuccess(t *testing.T) {
	st := newTestStore(t)
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: &mockPlatform{status: "out_for_delivery", eta: 5},
	})

	raw := mustRaw(t, getOrderStatusInput{App: "blinkit", OrderID: "BLK-123"})
	result, err := server.call(context.Background(), "get_order_status", raw)
	if err != nil {
		t.Fatalf("get_order_status failed: %v", err)
	}

	out := result.(getOrderStatusOutput)
	if out.Status != "out_for_delivery" {
		t.Errorf("Status: got %q", out.Status)
	}
	if out.ETAMinutes != 5 {
		t.Errorf("ETA: got %d", out.ETAMinutes)
	}
}
