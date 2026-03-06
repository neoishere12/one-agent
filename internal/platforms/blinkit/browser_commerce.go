package blinkit

import (
	"context"
	"fmt"
	"time"

	"one-agent/internal/types"
)

// PlaceOrderViaBrowser executes Blinkit cart/checkout/pay through the browser
// worker tied to the persistent web profile.
func (c *Client) PlaceOrderViaBrowser(
	ctx context.Context,
	productID, addressID, paymentToken string,
	quantity int,
) (types.Order, error) {
	workerURL := browserWorkerURL()
	if workerURL == "" {
		return types.Order{}, ErrBrowserWorkerNotConfigured
	}
	order, err := browserWorkerPlaceOrder(ctx, workerURL, productID, addressID, paymentToken, quantity)
	if err != nil {
		return types.Order{}, fmt.Errorf("blinkit browser order: %w", err)
	}
	if order.PlacedAt.IsZero() {
		order.PlacedAt = time.Now().UTC()
	}
	return order, nil
}

// HydrateBrowserSessionMetadata pulls addresses/payment methods from the live
// browser session, merges them into the stored Blinkit session, and persists.
func (c *Client) HydrateBrowserSessionMetadata(ctx context.Context) (*types.AppSession, error) {
	workerURL := browserWorkerURL()
	if workerURL == "" {
		return nil, ErrBrowserWorkerNotConfigured
	}
	session, err := c.base.LoadSessionRaw(ctx)
	if err != nil {
		return nil, err
	}
	addresses, payments, err := browserWorkerMetadata(ctx, workerURL)
	if err != nil {
		return nil, fmt.Errorf("blinkit browser metadata: %w", err)
	}
	updated := cloneAppSession(session)
	if len(addresses) > 0 {
		updated.Addresses = addresses
	}
	if len(payments) > 0 {
		updated.Payments = payments
	}
	updated.UpdatedAt = time.Now().UTC()
	if err := c.base.SaveSession(ctx, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func cloneAppSession(session *types.AppSession) *types.AppSession {
	if session == nil {
		return &types.AppSession{
			App:           types.PlatformBlinkit,
			DeviceHeaders: map[string]string{},
		}
	}
	cloned := *session
	cloned.DeviceHeaders = cloneSessionHeaders(session.DeviceHeaders)
	cloned.Addresses = append([]types.Address(nil), session.Addresses...)
	cloned.Payments = append([]types.PaymentMethod(nil), session.Payments...)
	return &cloned
}
