package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const explorerBaseURL = "https://explorer.near-intents.org/api"

var (
	explorerClient = &http.Client{Timeout: 30 * time.Second}
	explorerJWT    string // loaded from NEAR_INTENTS_EXPLORER_JWT
)

type ExplorerTx struct {
	DepositAddress           string           `json:"depositAddress"`
	DepositMemo              string           `json:"depositMemo"`
	Recipient                string           `json:"recipient"`
	Status                   string           `json:"status"`
	AmountInFormatted        string           `json:"amountInFormatted"`
	AmountOutFormatted       string           `json:"amountOutFormatted"`
	AmountInUsd              float64          `json:"amountInUsd"`
	AmountOutUsd             float64          `json:"amountOutUsd"`
	OriginAsset              string           `json:"originAsset"`
	DestinationAsset         string           `json:"destinationAsset"`
	Senders                  []string         `json:"senders"`
	NearTxHashes             []string         `json:"nearTxHashes"`
	OriginChainTxHashes      []string         `json:"originChainTxHashes"`
	DestinationChainTxHashes []string         `json:"destinationChainTxHashes"`
	AppFees                  []ExplorerAppFee `json:"appFees"`
	CreatedAt                string           `json:"createdAt"`
	CreatedAtTimestamp       int64            `json:"createdAtTimestamp"`
}

type ExplorerAppFee struct {
	Recipient string `json:"recipient"`
	Fee       int    `json:"fee"` // basis points
}

type explorerPageResp struct {
	Transactions []ExplorerTx `json:"transactions"`
}

// explorerGet makes a JWT-authenticated GET to the Explorer API.
func explorerGet(endpoint string) ([]byte, error) {
	req, err := http.NewRequest("GET", explorerBaseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if explorerJWT != "" {
		req.Header.Set("Authorization", "Bearer "+explorerJWT)
	}
	resp, err := explorerClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("explorer %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

// fetchExplorerTxs returns up to count SUCCESS txs for an affiliate.
// lastAddr/lastMemo are cursor tokens; empty = start from oldest.
func fetchExplorerTxs(affiliate, lastAddr, lastMemo string, count int) ([]ExplorerTx, error) {
	q := url.Values{}
	q.Set("affiliate", affiliate)
	q.Set("statuses", "SUCCESS")
	q.Set("numberOfTransactions", fmt.Sprintf("%d", count))
	q.Set("direction", "next")
	if lastAddr != "" {
		q.Set("lastDepositAddress", lastAddr)
		if lastMemo != "" {
			q.Set("lastDepositMemo", lastMemo)
		}
	}
	data, err := explorerGet("/v0/transactions?" + q.Encode())
	if err != nil {
		return nil, err
	}
	var r explorerPageResp
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return r.Transactions, nil
}

// txFeeUSD computes the USD fee taken from a transaction via appFees.
func txFeeUSD(tx ExplorerTx) float64 {
	var bps int
	for _, f := range tx.AppFees {
		bps += f.Fee
	}
	if bps == 0 || tx.AmountInUsd == 0 {
		return 0
	}
	return tx.AmountInUsd * float64(bps) / 10000.0
}

// txTokenLabel returns the token symbol for a defuse asset ID.
func txTokenLabel(assetID string) string {
	if t := findTokenByAssetID(assetID); t != nil && t.Ticker != "" {
		return t.Ticker
	}
	parts := strings.SplitN(assetID, ":", 2)
	if len(parts) == 2 && strings.ToUpper(parts[0]) != "NEP141" {
		return strings.ToUpper(parts[0])
	}
	if len(parts) == 2 {
		return strings.ToUpper(strings.SplitN(parts[1], ".", 2)[0])
	}
	return strings.ToUpper(assetID)
}

// txChainLabel returns the display chain name for a defuse asset ID.
func txChainLabel(assetID string) string {
	if t := findTokenByAssetID(assetID); t != nil && t.ChainName != "" {
		return explorerChainName(t.ChainName)
	}
	parts := strings.SplitN(assetID, ":", 2)
	if len(parts) >= 1 {
		return explorerChainName(parts[0])
	}
	return assetID
}

func explorerChainName(code string) string {
	m := map[string]string{
		"eth": "Ethereum", "btc": "Bitcoin", "sol": "Solana", "base": "Base",
		"arb": "Arbitrum", "ton": "TON", "tron": "TRON", "trx": "TRON",
		"bsc": "BNB Chain", "pol": "Polygon", "op": "Optimism",
		"avax": "Avalanche", "near": "NEAR", "sui": "Sui",
		"doge": "Dogecoin", "ltc": "Litecoin", "xrp": "XRP",
		"bch": "Bitcoin Cash", "xlm": "Stellar", "nep141": "NEAR",
	}
	if dn, ok := m[strings.ToLower(code)]; ok {
		return dn
	}
	return strings.ToUpper(code)
}
