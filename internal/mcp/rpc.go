package mcp

import (
	"encoding/json"
	"io"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func decodeRPCRequests(body io.Reader) ([]rpcRequest, bool, *toolError) {
	var raw json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, false, invalidRequest("invalid JSON-RPC request body")
	}
	if len(raw) == 0 {
		return nil, false, invalidRequest("empty JSON-RPC request body")
	}
	if raw[0] == '[' {
		var batch []rpcRequest
		if err := json.Unmarshal(raw, &batch); err != nil {
			return nil, true, invalidRequest("invalid JSON-RPC batch body")
		}
		if len(batch) == 0 {
			return nil, true, invalidRequest("empty JSON-RPC batch")
		}
		for i := range batch {
			if reqErr := validateRPCRequest(&batch[i]); reqErr != nil {
				return nil, true, reqErr
			}
		}
		return batch, true, nil
	}
	var single rpcRequest
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, false, invalidRequest("invalid JSON-RPC request body")
	}
	if reqErr := validateRPCRequest(&single); reqErr != nil {
		return nil, false, reqErr
	}
	return []rpcRequest{single}, false, nil
}

func validateRPCRequest(req *rpcRequest) *toolError {
	if req.JSONRPC != "" && req.JSONRPC != "2.0" {
		return invalidRequest("unsupported JSON-RPC version")
	}
	if req.Method == "" {
		return invalidRequest("missing method")
	}
	return nil
}

func errorResponse(id json.RawMessage, err *toolError) rpcResponse {
	return rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    err.Code,
			Message: err.Error(),
		},
	}
}

func successResponse(id json.RawMessage, result any) rpcResponse {
	return rpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}
