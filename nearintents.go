package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

var (
	nearIntentsBaseURL = "https://1click.chaindefuser.com"
	nearIntentsJWT     string
	nearHTTPClient     = &http.Client{Timeout: 30 * time.Second}
)

func initNearIntents() {
	if url := os.Getenv("NEAR_INTENTS_API_URL"); url != "" {
		nearIntentsBaseURL = url
	}
	nearIntentsJWT = os.Getenv("NEAR_INTENTS_JWT")
}

// QuoteRequest is the payload for POST /v0/quote
type QuoteRequest struct {
	Dry               bool       `json:"dry"`
	SwapType          string     `json:"swapType"`
	SlippageTolerance int        `json:"slippageTolerance"`
	OriginAsset       string     `json:"originAsset"`
	DepositType       string     `json:"depositType"`
	DestinationAsset  string     `json:"destinationAsset"`
	Amount            string     `json:"amount"`
	RefundTo          string     `json:"refundTo"`
	RefundType        string     `json:"refundType"`
	Recipient         string     `json:"recipient"`
	RecipientType     string     `json:"recipientType"`
	Deadline          string     `json:"deadline"`
	Referral          string     `json:"referral"`
	QuoteWaitingTimeMs int       `json:"quoteWaitingTimeMs"`
	AppFees           []struct{} `json:"appFees"`
}

// QuoteResponse is the response from POST /v0/quote (real, non-dry quote).
// The API nests quote details inside a "quote" field.
type QuoteResponse struct {
	CorrelationID string      `json:"correlationId"`
	Timestamp     string      `json:"timestamp"`
	Signature     string      `json:"signature"`
	Quote         QuoteDetail `json:"quote"`
}

// QuoteDetail contains the swap parameters inside a QuoteResponse.
type QuoteDetail struct {
	DepositAddress string `json:"depositAddress"`
	DepositMemo    string `json:"depositMemo,omitempty"`
	AmountIn       string `json:"amountIn"`
	AmountInFmt    string `json:"amountInFormatted"`
	AmountOut      string `json:"amountOut"`
	AmountOutFmt   string `json:"amountOutFormatted"`
	Deadline       string `json:"deadline,omitempty"`
	TimeEstimate   int    `json:"timeEstimate"`
}

// DryQuoteResponse is the response from POST /v0/quote with dry=true.
// The API nests the quote data inside a "quote" field.
type DryQuoteResponse struct {
	Quote struct {
		AmountIn           string `json:"amountIn"`
		AmountInFormatted  string `json:"amountInFormatted"`
		AmountInUSD        string `json:"amountInUsd"`
		AmountOut          string `json:"amountOut"`
		AmountOutFormatted string `json:"amountOutFormatted"`
		AmountOutUSD       string `json:"amountOutUsd"`
		MinAmountOut       string `json:"minAmountOut"`
		TimeEstimate       int    `json:"timeEstimate"`
	} `json:"quote"`
	CorrelationID string `json:"correlationId"`
}

// StatusResponse is the response from GET /v0/status
type StatusResponse struct {
	CorrelationID string       `json:"correlationId"`
	Status        string       `json:"status"`
	UpdatedAt     string       `json:"updatedAt,omitempty"`
	SwapDetails   *SwapDetails `json:"swapDetails,omitempty"`
	// Keep raw JSON for the /raw endpoint
	RawJSON json.RawMessage `json:"-"`
}

// SwapDetails contains the execution details of a swap.
type SwapDetails struct {
	AmountIn        string              `json:"amountIn,omitempty"`
	AmountInFmt     string              `json:"amountInFormatted,omitempty"`
	AmountOut       string              `json:"amountOut,omitempty"`
	AmountOutFmt    string              `json:"amountOutFormatted,omitempty"`
	OriginTxs       []TransactionDetail `json:"originChainTxHashes,omitempty"`
	DestTxs         []TransactionDetail `json:"destinationChainTxHashes,omitempty"`
	RefundedAmount  string              `json:"refundedAmount,omitempty"`
	RefundReason    string              `json:"refundReason,omitempty"`
}

// TransactionDetail is a tx hash with an explorer link.
type TransactionDetail struct {
	Hash        string `json:"hash"`
	ExplorerURL string `json:"explorerUrl"`
}

// TokenInfo represents a single token from the /v0/tokens endpoint.
type TokenInfo struct {
	DefuseAssetID   string  `json:"assetId"`
	Ticker          string  `json:"ticker,omitempty"`
	Symbol          string  `json:"symbol,omitempty"`
	Name            string  `json:"name,omitempty"`
	Decimals        int     `json:"decimals"`
	ChainName       string  `json:"blockchain,omitempty"`
	ChainID         string  `json:"chain_id,omitempty"`
	Price           float64 `json:"price,omitempty"`
	IconURL         string  `json:"icon,omitempty"`
	ContractAddress string  `json:"contractAddress,omitempty"`
}

// nearRequest makes an authenticated request to the NEAR Intents API.
// On 5xx responses it retries once after a short delay.
func nearRequest(method, path string, body interface{}) ([]byte, error) {
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyBytes = b
	}

	const maxAttempts = 2
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var bodyReader io.Reader
		if bodyBytes != nil {
			bodyReader = bytes.NewReader(bodyBytes)
		}

		req, err := http.NewRequest(method, nearIntentsBaseURL+path, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if nearIntentsJWT != "" {
			req.Header.Set("Authorization", "Bearer "+nearIntentsJWT)
		}

		resp, err := nearHTTPClient.Do(req)
		if err != nil {
			if attempt < maxAttempts-1 {
				time.Sleep(2 * time.Second)
				continue
			}
			return nil, fmt.Errorf("request failed: %w", err)
		}

		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		if resp.StatusCode >= 500 && attempt < maxAttempts-1 {
			time.Sleep(2 * time.Second)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(data))
		}

		return data, nil
	}

	return nil, fmt.Errorf("request failed after %d attempts", maxAttempts)
}

// fetchTokens retrieves the supported token list from NEAR Intents.
func fetchTokens() ([]TokenInfo, error) {
	data, err := nearRequest("GET", "/v0/tokens", nil)
	if err != nil {
		return nil, err
	}

	// The API may return tokens in different structures.
	// Try parsing as array first, then as object with "tokens" key.
	var tokens []TokenInfo
	if err := json.Unmarshal(data, &tokens); err != nil {
		// Try as wrapper object
		var wrapper struct {
			Tokens []TokenInfo `json:"tokens"`
		}
		if err2 := json.Unmarshal(data, &wrapper); err2 != nil {
			return nil, fmt.Errorf("parse tokens: %w (also tried: %w)", err, err2)
		}
		tokens = wrapper.Tokens
	}
	return tokens, nil
}

// requestDryQuote sends a dry quote request and parses the nested response.
func requestDryQuote(req *QuoteRequest) (*DryQuoteResponse, error) {
	req.Dry = true
	data, err := nearRequest("POST", "/v0/quote", req)
	if err != nil {
		return nil, err
	}

	var resp DryQuoteResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse dry quote response: %w", err)
	}
	return &resp, nil
}

// requestQuote sends a real (non-dry) quote request to NEAR Intents.
func requestQuote(req *QuoteRequest) (*QuoteResponse, error) {
	req.Dry = false
	data, err := nearRequest("POST", "/v0/quote", req)
	if err != nil {
		return nil, err
	}

	var resp QuoteResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse quote response: %w", err)
	}
	return &resp, nil
}

// fetchStatus checks the status of a swap by deposit address.
// depositMemo is optional but required for chains that use memos (TON, XRP, NEAR, Stellar).
func fetchStatus(depositAddress, depositMemo string) (*StatusResponse, error) {
	q := url.Values{"depositAddress": {depositAddress}}
	if depositMemo != "" {
		q.Set("depositMemo", depositMemo)
	}
	data, err := nearRequest("GET", "/v0/status?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}

	var resp StatusResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse status response: %w", err)
	}
	resp.RawJSON = data
	return &resp, nil
}

// buildDeadline returns an ISO 8601 deadline string from a duration.
func buildDeadline(d time.Duration) string {
	return time.Now().UTC().Add(d).Format(time.RFC3339)
}

