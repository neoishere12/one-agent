package proxyman

import "strings"

func isAddressPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "address") || strings.Contains(lower, "location")
}

func isPaymentPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.Contains(lower, "payment") || strings.Contains(lower, "instrument") || strings.Contains(lower, "wallet")
}

func consumeAddresses(agg *aggregate, decoded any) {
	walkJSON(decoded, func(m map[string]any) {
		addr, ok := addressFromMap(m)
		if !ok {
			return
		}
		mergeAddress(agg.addresses, addr)
	})
}

func addressFromMap(m map[string]any) (IngestAddress, bool) {
	id := valueString(m, "id", "address_id", "addressId")
	if id == "" {
		return IngestAddress{}, false
	}
	full := addressFullAddress(m)
	label := getString(m, "label", "name", "tag", "title")
	if full == "" && !looksAddressLikeID(id) {
		return IngestAddress{}, false
	}
	return IngestAddress{ID: id, Label: label, FullAddress: full}, true
}

func addressFullAddress(m map[string]any) string {
	if s := getString(m, "full_address", "fullAddress", "address", "display_address", "displayAddress"); s != "" {
		return s
	}
	return joinNonEmpty(
		getString(m, "line1", "address_line_1", "addressLine1"),
		getString(m, "line2", "address_line_2", "addressLine2"),
		getString(m, "city"),
		getString(m, "pin_code", "pincode", "zip", "postal_code"),
	)
}

func looksAddressLikeID(id string) bool {
	lower := strings.ToLower(id)
	return strings.Contains(lower, "addr")
}

func mergeAddress(dst map[string]IngestAddress, addr IngestAddress) {
	cur, ok := dst[addr.ID]
	if !ok {
		dst[addr.ID] = addr
		return
	}
	if cur.Label == "" && addr.Label != "" {
		cur.Label = addr.Label
	}
	if cur.FullAddress == "" && addr.FullAddress != "" {
		cur.FullAddress = addr.FullAddress
	}
	dst[addr.ID] = cur
}

func consumePayments(agg *aggregate, decoded any) {
	walkJSON(decoded, func(m map[string]any) {
		pay, ok := paymentFromMap(m)
		if !ok {
			return
		}
		mergePayment(agg.payments, pay)
	})
}

func paymentFromMap(m map[string]any) (IngestPayment, bool) {
	token := valueString(m, "token", "payment_token", "paymentToken", "instrument_token", "instrumentToken")
	id := valueString(m, "id", "payment_id", "paymentId", "instrument_id", "instrumentId")
	if token == "" && id == "" {
		return IngestPayment{}, false
	}
	if token == "" {
		return IngestPayment{}, false
	}
	label := paymentLabel(m)
	typ := strings.ToLower(getString(m, "type", "payment_type", "paymentType", "method"))
	if id == "" {
		id = token
	}
	return IngestPayment{ID: id, Label: label, Token: token, Type: typ}, true
}

func paymentLabel(m map[string]any) string {
	if s := getString(m, "label", "display_name", "displayName", "name"); s != "" {
		return s
	}
	masked := getString(m, "masked_number", "maskedNumber", "last4")
	brand := getString(m, "brand", "issuer", "scheme")
	if masked == "" && brand == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join([]string{brand, masked}, " "))
}

func mergePayment(dst map[string]IngestPayment, pay IngestPayment) {
	key := pay.ID
	if key == "" {
		key = pay.Token
	}
	cur, ok := dst[key]
	if !ok {
		dst[key] = pay
		return
	}
	if cur.Label == "" && pay.Label != "" {
		cur.Label = pay.Label
	}
	if cur.Type == "" && pay.Type != "" {
		cur.Type = pay.Type
	}
	if cur.Token == "" && pay.Token != "" {
		cur.Token = pay.Token
	}
	if cur.ID == "" && pay.ID != "" {
		cur.ID = pay.ID
	}
	dst[key] = cur
}

func valueString(m map[string]any, keys ...string) string {
	if s := getString(m, keys...); s != "" {
		return s
	}
	raw, ok := getAny(m, keys...)
	if !ok {
		return ""
	}
	return stringValue(raw)
}
