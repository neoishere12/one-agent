package mcp

import (
	"context"
	"testing"
	"time"

	"one-agent/internal/platforms"
	"one-agent/internal/types"
)

func TestGetSavedAddressesHydratesBrowserMetadata(t *testing.T) {
	st := newTestStore(t)
	session := defaultSession(types.PlatformBlinkit)
	session.Addresses = nil
	seedSession(t, st, session)
	client := &mockBrowserPlatform{
		hydratedSession: &types.AppSession{
			App: types.PlatformBlinkit,
			Addresses: []types.Address{
				{ID: "addr-remote", Label: "Home", Line1: "Sainik Colony, Jammu", IsDefault: true},
			},
		},
	}
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: client,
	})

	raw := mustRaw(t, getSavedAddressesInput{App: "blinkit"})
	result, err := server.call(context.Background(), "get_saved_addresses", raw)
	if err != nil {
		t.Fatalf("get_saved_addresses failed: %v", err)
	}

	out := result.(getSavedAddressesOutput)
	if client.hydratedSessionCall != 1 {
		t.Fatalf("expected hydrate call, got %d", client.hydratedSessionCall)
	}
	if len(out.Addresses) != 1 || out.Addresses[0].ID != "addr-remote" {
		t.Fatalf("unexpected hydrated addresses: %+v", out.Addresses)
	}
}

func TestPlaceOrderUsesBrowserOrderWhenSupported(t *testing.T) {
	st := newTestStore(t)
	seedSession(t, st, defaultSession(types.PlatformBlinkit))
	client := &mockBrowserPlatform{
		browserOrder: types.Order{
			ID: "BLK-browser-1", Status: "confirmed", ETAMinutes: 9, TotalRupees: 20, PlacedAt: time.Now(),
		},
	}
	server := newServerForTests(st, map[types.Platform]platforms.Platform{
		types.PlatformBlinkit: client,
	})

	raw := mustRaw(t, placeOrderInput{
		App: "blinkit", ProductID: "191439", AddressID: "addr-home", PaymentMode: paymentModeCOD, Confirm: true,
	})
	result, err := server.call(context.Background(), "place_order", raw)
	if err != nil {
		t.Fatalf("place_order failed: %v", err)
	}

	out := result.(placeOrderOutput)
	if client.browserOrderCalls != 1 {
		t.Fatalf("expected browser order path, got %d calls", client.browserOrderCalls)
	}
	if out.OrderID != "BLK-browser-1" {
		t.Fatalf("unexpected order id: %q", out.OrderID)
	}
	if client.addCartID != "" || client.lastPayToken != "" {
		t.Fatalf("expected raw API path unused, got addCartID=%q payToken=%q", client.addCartID, client.lastPayToken)
	}
}
