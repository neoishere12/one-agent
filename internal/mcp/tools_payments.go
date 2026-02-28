package mcp

import (
	"strings"

	"one-agent/internal/types"
)

const (
	paymentModeSaved     = "saved"
	paymentModeCOD       = "cod"
	paymentModeUPIIntent = "upi_intent"
)

type resolvedPayment struct {
	Token string
	Label string
}

func mapPaymentMethods(session *types.AppSession) []paymentMethodResult {
	out := make([]paymentMethodResult, 0, len(session.Payments))
	for _, payment := range session.Payments {
		out = append(out, paymentMethodResult{
			ID:        payment.ID,
			Type:      payment.Type,
			Label:     payment.Label,
			IsDefault: payment.IsDefault,
		})
	}
	return out
}

func defaultPaymentModes() []paymentModeOption {
	return []paymentModeOption{
		{Mode: paymentModeCOD, Label: "Cash on Delivery"},
		{Mode: paymentModeUPIIntent, Label: "UPI Intent"},
	}
}

func normalizePaymentMode(raw string) string {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return paymentModeSaved
	}
	return mode
}

func isValidPaymentMode(mode string) bool {
	switch mode {
	case paymentModeSaved, paymentModeCOD, paymentModeUPIIntent:
		return true
	default:
		return false
	}
}

func resolvePaymentForOrder(session *types.AppSession, input validatedPlaceOrder) (resolvedPayment, *toolError) {
	switch input.paymentMode {
	case paymentModeCOD:
		return resolvedPayment{Token: paymentModeCOD, Label: "Cash on Delivery"}, nil
	case paymentModeUPIIntent:
		return resolvedPayment{Token: paymentModeUPIIntent, Label: "UPI Intent"}, nil
	default:
		return resolveSavedPayment(session, input)
	}
}

func resolveSavedPayment(session *types.AppSession, input validatedPlaceOrder) (resolvedPayment, *toolError) {
	if input.paymentToken != "" {
		return resolvedPayment{
			Token: input.paymentToken,
			Label: paymentDisplay(session, input.paymentToken),
		}, nil
	}
	for _, payment := range session.Payments {
		if payment.ID != input.paymentID {
			continue
		}
		return resolvedPayment{Token: payment.Token, Label: paymentDisplay(session, payment.Token)}, nil
	}
	return resolvedPayment{}, invalidParams("payment_id not found in saved payment methods", nil)
}

func paymentDisplay(session *types.AppSession, paymentToken string) string {
	switch paymentToken {
	case paymentModeCOD:
		return "Cash on Delivery"
	case paymentModeUPIIntent:
		return "UPI Intent"
	}
	for _, payment := range session.Payments {
		if payment.Token != paymentToken {
			continue
		}
		if payment.Label != "" {
			return payment.Label
		}
		return payment.ID
	}
	return redactedToken(paymentToken)
}

func redactedToken(token string) string {
	if len(token) <= 4 {
		return "****"
	}
	return "**** " + token[len(token)-4:]
}
