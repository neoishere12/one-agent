package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"one-agent/internal/platforms"
	"one-agent/internal/platforms/blinkit"
	"one-agent/internal/types"
)

func (s *Server) handleGetSavedAddresses(ctx context.Context, raw []byte) (any, *toolError) {
	var input getSavedAddressesInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	app, appErr := parsePlatform(strings.ToLower(strings.TrimSpace(input.App)))
	if appErr != nil {
		return nil, appErr
	}

	session, sessionErr := s.sessionForAppWithMetadata(ctx, app, true, false)
	if sessionErr != nil {
		return nil, sessionErr
	}
	return getSavedAddressesOutput{App: string(app), Addresses: mapAddresses(session)}, nil
}

func (s *Server) handleGetSavedPaymentMethods(ctx context.Context, raw []byte) (any, *toolError) {
	var input getSavedPaymentMethodsInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	app, appErr := parsePlatform(strings.ToLower(strings.TrimSpace(input.App)))
	if appErr != nil {
		return nil, appErr
	}
	session, sessionErr := s.sessionForAppWithMetadata(ctx, app, false, true)
	if sessionErr != nil {
		return nil, sessionErr
	}
	return getSavedPaymentMethodsOutput{
		App:          string(app),
		SavedMethods: mapPaymentMethods(session),
		ExtraModes:   defaultPaymentModes(),
	}, nil
}

func (s *Server) handlePlaceOrder(ctx context.Context, raw []byte) (any, *toolError) {
	var input placeOrderInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	validated, validateErr := validatePlaceOrderInput(input)
	if validateErr != nil {
		return nil, validateErr
	}

	session, sessionErr := s.sessionForApp(ctx, validated.app)
	if sessionErr != nil {
		return nil, sessionErr
	}
	client, clientErr := s.platform(validated.app)
	if clientErr != nil {
		return nil, clientErr
	}
	payment, paymentErr := resolvePaymentForOrder(session, validated)
	if paymentErr != nil {
		return nil, paymentErr
	}

	order, placeErr := s.executeOrder(ctx, client, validated, payment.Token)
	if placeErr != nil {
		return nil, internalError("place_order failed", placeErr)
	}
	nextAction := ""
	upiIntentRequired := false
	if validated.paymentMode == paymentModeUPIIntent {
		nextAction = "complete UPI intent authorization on your phone"
		upiIntentRequired = true
	}
	return placeOrderOutput{
		OrderID:           order.ID,
		App:               string(validated.app),
		Product:           validated.productID,
		TotalCharged:      order.TotalRupees,
		Address:           addressDisplay(session, validated.addressID),
		Payment:           payment.Label,
		PaymentMode:       validated.paymentMode,
		UPIIntentRequired: upiIntentRequired,
		NextAction:        nextAction,
		ETAMinutes:        order.ETAMinutes,
		Status:            order.Status,
		PlacedAt:          order.PlacedAt,
	}, nil
}

func (s *Server) executeOrder(
	ctx context.Context,
	client platforms.Platform,
	input validatedPlaceOrder,
	paymentToken string,
) (types.Order, error) {
	if browserClient, ok := client.(browserOrderPlatform); ok {
		order, err := browserClient.PlaceOrderViaBrowser(ctx, input.productID, input.addressID, paymentToken, input.quantity)
		if err == nil {
			return order, nil
		}
		if !errors.Is(err, blinkit.ErrBrowserWorkerNotConfigured) {
			return types.Order{}, err
		}
	}
	cartID, err := client.AddToCart(ctx, input.productID, input.quantity)
	if err != nil {
		return types.Order{}, err
	}
	checkout, err := client.Checkout(ctx, cartID, input.addressID)
	if err != nil {
		return types.Order{}, err
	}
	order, err := client.Pay(ctx, checkout.CheckoutID, paymentToken)
	if err != nil {
		return types.Order{}, err
	}
	if order.ETAMinutes == 0 {
		order.ETAMinutes = checkout.ETAMinutes
	}
	return order, nil
}

func (s *Server) sessionForApp(ctx context.Context, app types.Platform) (*types.AppSession, *toolError) {
	session, err := s.st.Get(ctx, app)
	if err != nil {
		if isStoreNotFound(err) {
			return nil, invalidParams("session not found for app", err)
		}
		return nil, internalError("session lookup failed", err)
	}
	return session, nil
}

func (s *Server) handleGetOrderStatus(ctx context.Context, raw []byte) (any, *toolError) {
	var input getOrderStatusInput
	if err := decodeParams(raw, &input); err != nil {
		return nil, err
	}
	app, appErr := parsePlatform(strings.ToLower(strings.TrimSpace(input.App)))
	if appErr != nil {
		return nil, appErr
	}
	if strings.TrimSpace(input.OrderID) == "" {
		return nil, invalidParams("order_id is required", nil)
	}
	client, getErr := s.platform(app)
	if getErr != nil {
		return nil, getErr
	}
	status, eta, err := client.OrderStatus(ctx, strings.TrimSpace(input.OrderID))
	if err != nil {
		return nil, internalError("get_order_status failed", err)
	}
	return getOrderStatusOutput{
		OrderID:     input.OrderID,
		Status:      status,
		ETAMinutes:  eta,
		LastUpdated: s.now(),
	}, nil
}

type validatedPlaceOrder struct {
	app          types.Platform
	productID    string
	addressID    string
	paymentID    string
	paymentToken string
	paymentMode  string
	quantity     int
}

func validatePlaceOrderInput(input placeOrderInput) (validatedPlaceOrder, *toolError) {
	if !input.Confirm {
		return validatedPlaceOrder{}, invalidParams("place_order requires confirm=true", nil)
	}
	app, appErr := parsePlatform(strings.ToLower(strings.TrimSpace(input.App)))
	if appErr != nil {
		return validatedPlaceOrder{}, appErr
	}
	productID := strings.TrimSpace(input.ProductID)
	addressID := strings.TrimSpace(input.AddressID)
	paymentID := strings.TrimSpace(input.PaymentID)
	paymentToken := strings.TrimSpace(input.PaymentToken)
	paymentMode := normalizePaymentMode(input.PaymentMode)
	if productID == "" || addressID == "" {
		return validatedPlaceOrder{}, invalidParams("product_id and address_id are required", nil)
	}
	if !isValidPaymentMode(paymentMode) {
		return validatedPlaceOrder{}, invalidParams("payment_mode must be one of: saved, cod, upi_intent", nil)
	}
	if paymentMode == paymentModeSaved && paymentToken == "" && paymentID == "" {
		return validatedPlaceOrder{}, invalidParams("saved payment requires payment_token or payment_id", nil)
	}
	quantity := input.Quantity
	if quantity <= 0 {
		quantity = 1
	}
	return validatedPlaceOrder{
		app:          app,
		productID:    productID,
		addressID:    addressID,
		paymentID:    paymentID,
		paymentToken: paymentToken,
		paymentMode:  paymentMode,
		quantity:     quantity,
	}, nil
}

func mapAddresses(session *types.AppSession) []addressResult {
	out := make([]addressResult, 0, len(session.Addresses))
	for _, address := range session.Addresses {
		out = append(out, addressResult{
			ID:          address.ID,
			Label:       address.Label,
			FullAddress: fullAddress(address),
		})
	}
	return out
}

func fullAddress(address types.Address) string {
	parts := make([]string, 0, 5)
	for _, part := range []string{address.Line1, address.Line2, address.City, address.PinCode} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, strings.TrimSpace(part))
		}
	}
	return strings.Join(parts, ", ")
}

func addressDisplay(session *types.AppSession, addressID string) string {
	for _, address := range session.Addresses {
		if address.ID != addressID {
			continue
		}
		if address.Label == "" {
			return fullAddress(address)
		}
		return fmt.Sprintf("%s — %s", address.Label, fullAddress(address))
	}
	return addressID
}
