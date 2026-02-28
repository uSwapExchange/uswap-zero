package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain sets up the test environment once before all tests.
func TestMain(m *testing.M) {
	// Initialize crypto with a random key (test mode)
	initCrypto()
	initNearIntents()
	initTemplates()
	startCacheRefresher()
	os.Exit(m.Run())
}

// ════════════════════════════════════════════════════════════
// Unit Tests — Amount Math
// ════════════════════════════════════════════════════════════

func TestHumanToAtomic(t *testing.T) {
	tests := []struct {
		amount   string
		decimals int
		want     string
		wantErr  bool
	}{
		{"1", 18, "1000000000000000000", false},
		{"0.5", 18, "500000000000000000", false},
		{"0.000001", 6, "1", false},
		{"100", 6, "100000000", false},
		{"1.5", 8, "150000000", false},
		{"0", 18, "0", false},
		{"", 18, "", true},
		{"abc", 18, "", true},
		{"0.123456789", 6, "123456", false}, // truncates excess precision
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%d", tt.amount, tt.decimals), func(t *testing.T) {
			got, err := humanToAtomic(tt.amount, tt.decimals)
			if tt.wantErr {
				if err == nil {
					t.Errorf("humanToAtomic(%q, %d) expected error, got %q", tt.amount, tt.decimals, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("humanToAtomic(%q, %d) unexpected error: %v", tt.amount, tt.decimals, err)
			}
			if got != tt.want {
				t.Errorf("humanToAtomic(%q, %d) = %q, want %q", tt.amount, tt.decimals, got, tt.want)
			}
		})
	}
}

func TestAtomicToHuman(t *testing.T) {
	tests := []struct {
		atomic   string
		decimals int
		want     string
	}{
		{"1000000000000000000", 18, "1"},
		{"500000000000000000", 18, "0.5"},
		{"1", 6, "0.000001"},
		{"100000000", 6, "100"},
		{"150000000", 8, "1.5"},
		{"0", 18, "0"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%d", tt.atomic, tt.decimals), func(t *testing.T) {
			got := atomicToHuman(tt.atomic, tt.decimals)
			if got != tt.want {
				t.Errorf("atomicToHuman(%q, %d) = %q, want %q", tt.atomic, tt.decimals, got, tt.want)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Verify that humanToAtomic → atomicToHuman is lossless for clean values
	cases := []struct {
		human    string
		decimals int
	}{
		{"1", 18},
		{"0.5", 18},
		{"100", 6},
		{"0.000001", 6},
		{"1.5", 8},
	}

	for _, c := range cases {
		atomic, err := humanToAtomic(c.human, c.decimals)
		if err != nil {
			t.Fatalf("humanToAtomic(%q, %d) error: %v", c.human, c.decimals, err)
		}
		back := atomicToHuman(atomic, c.decimals)
		if back != c.human {
			t.Errorf("Round-trip failed: %q → %q → %q", c.human, atomic, back)
		}
	}
}

func TestFormatUSD(t *testing.T) {
	tests := []struct {
		amount float64
		want   string
	}{
		{0.000001, "$0.000001"},
		{0.05, "$0.0500"},
		{1.50, "$1.50"},
		{999.99, "$999.99"},
		{1000, "$1,000"},
		{1234.56, "$1,234.56"},
		{1000000, "$1,000,000"},
	}

	for _, tt := range tests {
		got := formatUSD(tt.amount)
		if got != tt.want {
			t.Errorf("formatUSD(%v) = %q, want %q", tt.amount, got, tt.want)
		}
	}
}

func TestSlippageToBPS(t *testing.T) {
	tests := []struct {
		pct     string
		want    int
		wantErr bool
	}{
		{"1", 100, false},
		{"0.5", 50, false},
		{"2", 200, false},
		{"3", 300, false},
		{"", 100, false}, // default
		{"51", 0, true},  // out of range
	}

	for _, tt := range tests {
		got, err := slippageToBPS(tt.pct)
		if tt.wantErr {
			if err == nil {
				t.Errorf("slippageToBPS(%q) expected error, got %d", tt.pct, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("slippageToBPS(%q) unexpected error: %v", tt.pct, err)
		}
		if got != tt.want {
			t.Errorf("slippageToBPS(%q) = %d, want %d", tt.pct, got, tt.want)
		}
	}
}

// ════════════════════════════════════════════════════════════
// Unit Tests — Crypto
// ════════════════════════════════════════════════════════════

func TestEncryptDecryptOrderData(t *testing.T) {
	order := &OrderData{
		DepositAddr: "0xabc123",
		FromTicker:  "ETH",
		FromNet:     "eth",
		ToTicker:    "USDT",
		ToNet:       "eth",
		AmountIn:    "1",
		AmountOut:   "1826.34",
		Deadline:    time.Now().Add(time.Hour).Format(time.RFC3339),
		CorrID:      "corr-123",
	}

	token, err := encryptOrderData(order)
	if err != nil {
		t.Fatalf("encryptOrderData failed: %v", err)
	}

	if token == "" {
		t.Fatal("encrypted token is empty")
	}

	// Decrypt
	decrypted, err := decryptOrderData(token)
	if err != nil {
		t.Fatalf("decryptOrderData failed: %v", err)
	}

	if decrypted.DepositAddr != order.DepositAddr {
		t.Errorf("DepositAddr: got %q, want %q", decrypted.DepositAddr, order.DepositAddr)
	}
	if decrypted.FromTicker != order.FromTicker {
		t.Errorf("FromTicker: got %q, want %q", decrypted.FromTicker, order.FromTicker)
	}
	if decrypted.CorrID != order.CorrID {
		t.Errorf("CorrID: got %q, want %q", decrypted.CorrID, order.CorrID)
	}
}

func TestDecryptInvalidToken(t *testing.T) {
	_, err := decryptOrderData("not-a-valid-token")
	if err == nil {
		t.Error("expected error for invalid token, got nil")
	}
}

func TestCSRFToken(t *testing.T) {
	token := generateCSRFToken("test")
	if token == "" {
		t.Fatal("CSRF token is empty")
	}

	if !verifyCSRFToken(token, "test", time.Hour) {
		t.Error("valid CSRF token failed verification")
	}

	// Wrong form ID
	if verifyCSRFToken(token, "wrong", time.Hour) {
		t.Error("CSRF token verified with wrong form ID")
	}

	// Tampered token
	if verifyCSRFToken(token+"x", "test", time.Hour) {
		t.Error("tampered CSRF token should not verify")
	}
}

// ════════════════════════════════════════════════════════════
// Integration Tests — NEAR Intents Production API
// ════════════════════════════════════════════════════════════

func TestFetchTokensFromAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping API test in short mode")
	}

	tokens, err := fetchTokens()
	if err != nil {
		t.Fatalf("fetchTokens() failed: %v", err)
	}

	if len(tokens) < 10 {
		t.Errorf("expected at least 10 tokens, got %d", len(tokens))
	}

	// Verify ETH exists
	foundETH := false
	foundUSDT := false
	for _, tok := range tokens {
		if strings.EqualFold(tok.Ticker, "ETH") || strings.EqualFold(tok.Symbol, "ETH") {
			foundETH = true
		}
		if strings.EqualFold(tok.Ticker, "USDT") || strings.EqualFold(tok.Symbol, "USDT") {
			foundUSDT = true
		}
	}

	if !foundETH {
		t.Error("ETH not found in token list")
	}
	if !foundUSDT {
		t.Error("USDT not found in token list")
	}

	t.Logf("Fetched %d tokens from production API", len(tokens))
}

func TestTokenCacheAndLookup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping API test in short mode")
	}

	// Force cache refresh
	if err := refreshTokenCache(); err != nil {
		t.Fatalf("refreshTokenCache() failed: %v", err)
	}

	// Test findToken
	eth := findToken("ETH", "eth")
	if eth == nil {
		t.Fatal("findToken(ETH, eth) returned nil")
	}
	if eth.Decimals != 18 {
		t.Errorf("ETH decimals: got %d, want 18", eth.Decimals)
	}
	if eth.Price <= 0 {
		t.Errorf("ETH price should be > 0, got %f", eth.Price)
	}
	if eth.DefuseAssetID == "" {
		t.Error("ETH DefuseAssetID is empty")
	}

	t.Logf("ETH: decimals=%d, price=$%.2f, assetID=%s", eth.Decimals, eth.Price, eth.DefuseAssetID)

	// Test USDT lookup
	usdt := findToken("USDT", "eth")
	if usdt == nil {
		t.Fatal("findToken(USDT, eth) returned nil")
	}
	if usdt.Decimals != 6 {
		t.Errorf("USDT decimals: got %d, want 6", usdt.Decimals)
	}

	t.Logf("USDT: decimals=%d, price=$%.2f, assetID=%s", usdt.Decimals, usdt.Price, usdt.DefuseAssetID)
}

func TestNetworkGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping API test in short mode")
	}

	networks, err := getNetworkGroups()
	if err != nil {
		t.Fatalf("getNetworkGroups() failed: %v", err)
	}

	if len(networks) < 3 {
		t.Errorf("expected at least 3 networks, got %d", len(networks))
	}

	// Ethereum should be first
	if networks[0].Name != "Ethereum" {
		t.Errorf("first network should be Ethereum, got %q", networks[0].Name)
	}

	totalTokens := 0
	for _, ng := range networks {
		totalTokens += len(ng.Tokens)
		t.Logf("Network: %s (%d tokens)", ng.Name, len(ng.Tokens))
	}

	t.Logf("Total: %d tokens across %d networks", totalTokens, len(networks))
}

func TestDryQuoteETHtoUSDT(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping API test in short mode")
	}

	// Ensure cache is loaded
	if err := refreshTokenCache(); err != nil {
		t.Fatalf("refreshTokenCache() failed: %v", err)
	}

	eth := findToken("ETH", "eth")
	usdt := findToken("USDT", "eth")
	if eth == nil || usdt == nil {
		t.Fatal("ETH or USDT not found in cache")
	}

	// Convert 1 ETH to atomic (larger amount for reliable quotes)
	atomicAmount, err := humanToAtomic("1", eth.Decimals)
	if err != nil {
		t.Fatalf("humanToAtomic failed: %v", err)
	}

	quoteReq := &QuoteRequest{
		Dry:                true,
		SwapType:           "FLEX_INPUT",
		SlippageTolerance:  100, // 1%
		OriginAsset:        eth.DefuseAssetID,
		DepositType:        "ORIGIN_CHAIN",
		DestinationAsset:   usdt.DefuseAssetID,
		Amount:             atomicAmount,
		RefundTo:           "0xab5801a7d398351b8be11c439e05c5b3259aec9b",
		RefundType:         "ORIGIN_CHAIN",
		Recipient:          "0xab5801a7d398351b8be11c439e05c5b3259aec9b",
		RecipientType:      "DESTINATION_CHAIN",
		Deadline:           buildDeadline(time.Hour),
		QuoteWaitingTimeMs: 10000,
		AppFees:            []struct{}{},
	}

	resp, err := requestDryQuote(quoteReq)
	if err != nil {
		t.Skipf("dry quote API unavailable (may be temporary): %v", err)
	}

	amountOut := resp.Quote.AmountOut
	if amountOut == "" || amountOut == "0" {
		t.Fatalf("dry quote returned zero amountOut: %q", amountOut)
	}

	humanOut := atomicToHuman(amountOut, usdt.Decimals)
	outFloat, _ := parseFloat(humanOut)

	t.Logf("Dry quote: 1 ETH → %s USDT ($%.2f)", humanOut, outFloat)
	t.Logf("ETH assetID: %s", eth.DefuseAssetID)
	t.Logf("USDT assetID: %s", usdt.DefuseAssetID)

	// Sanity check: 1 ETH should give us between $100 and $100000 of USDT
	if outFloat < 100 || outFloat > 100000 {
		t.Errorf("quote result out of sane range: %s USDT", humanOut)
	}
}

func TestDryQuoteBTCtoETH(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping API test in short mode")
	}

	if err := refreshTokenCache(); err != nil {
		t.Fatalf("refreshTokenCache() failed: %v", err)
	}

	btc := findToken("BTC", "btc")
	eth := findToken("ETH", "eth")
	if btc == nil || eth == nil {
		t.Skip("BTC or ETH not found in cache — skipping cross-chain test")
	}

	// Use 0.1 BTC for a more reliable quote
	atomicAmount, err := humanToAtomic("0.1", btc.Decimals)
	if err != nil {
		t.Fatalf("humanToAtomic failed: %v", err)
	}

	quoteReq := &QuoteRequest{
		Dry:                true,
		SwapType:           "FLEX_INPUT",
		SlippageTolerance:  200, // 2%
		OriginAsset:        btc.DefuseAssetID,
		DepositType:        "ORIGIN_CHAIN",
		DestinationAsset:   eth.DefuseAssetID,
		Amount:             atomicAmount,
		RefundTo:           "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4",
		RefundType:         "ORIGIN_CHAIN",
		Recipient:          "0xab5801a7d398351b8be11c439e05c5b3259aec9b",
		RecipientType:      "DESTINATION_CHAIN",
		Deadline:           buildDeadline(time.Hour),
		QuoteWaitingTimeMs: 10000,
		AppFees:            []struct{}{},
	}

	resp, err := requestDryQuote(quoteReq)
	if err != nil {
		t.Skipf("dry quote API unavailable (may be temporary): %v", err)
	}

	amountOut := resp.Quote.AmountOut
	if amountOut == "" || amountOut == "0" {
		t.Fatalf("dry quote returned zero amount for BTC→ETH")
	}

	humanOut := atomicToHuman(amountOut, eth.Decimals)
	t.Logf("Dry quote: 0.1 BTC → %s ETH", humanOut)
}

// ════════════════════════════════════════════════════════════
// HTTP Handler Tests
// ════════════════════════════════════════════════════════════

func TestSwapPageHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handleSwap(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("GET / status: got %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	// Check key elements
	checks := []string{
		"uSwap Zero",
		"You Send",
		"You Receive",
		"ETH",
		"USDT",
		"Get Quote",
		"csrf",
		"Slippage",
		"tooltip-trigger",
		"logo.png",
	}
	for _, check := range checks {
		if !strings.Contains(bodyStr, check) {
			t.Errorf("swap page missing %q", check)
		}
	}
}

func TestSwapPage404(t *testing.T) {
	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	handleSwap(w, req)

	if w.Code != 404 {
		t.Errorf("GET /nonexistent status: got %d, want 404", w.Code)
	}
}

func TestCurrenciesHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping API test in short mode")
	}

	req := httptest.NewRequest("GET", "/currencies", nil)
	w := httptest.NewRecorder()
	handleCurrencies(w, req)

	if w.Code != 200 {
		t.Errorf("GET /currencies status: got %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Ethereum") {
		t.Error("currencies page missing Ethereum network")
	}
}

func TestCurrenciesSearchHandler(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping API test in short mode")
	}

	req := httptest.NewRequest("GET", "/currencies?search=eth", nil)
	w := httptest.NewRecorder()
	handleCurrencies(w, req)

	if w.Code != 200 {
		t.Errorf("GET /currencies?search=eth status: got %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "ETH") {
		t.Error("currencies search for 'eth' missing ETH token")
	}
}

func TestHowItWorksHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/how-it-works", nil)
	w := httptest.NewRecorder()
	handleHowItWorks(w, req)

	if w.Code != 200 {
		t.Errorf("GET /how-it-works status: got %d, want 200", w.Code)
	}
}

func TestCaseStudyHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/case-study", nil)
	w := httptest.NewRecorder()
	handleCaseStudy(w, req)

	if w.Code != 200 {
		t.Errorf("GET /case-study status: got %d, want 200", w.Code)
	}
}

func TestVerifyHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/verify", nil)
	w := httptest.NewRecorder()
	handleVerify(w, req)

	if w.Code != 200 {
		t.Errorf("GET /verify status: got %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "development") {
		t.Error("verify page missing commit hash (should show 'development' in test)")
	}
}

func TestGenIconHandler(t *testing.T) {
	req := httptest.NewRequest("GET", "/icons/gen/ETH", nil)
	w := httptest.NewRecorder()
	handleGenIcon(w, req)

	if w.Code != 200 {
		t.Errorf("GET /icons/gen/ETH status: got %d, want 200", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "image/svg+xml" {
		t.Errorf("Content-Type: got %q, want image/svg+xml", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "<svg") {
		t.Error("generated icon is not valid SVG")
	}
	if !strings.Contains(body, "ETH") {
		t.Error("generated icon missing ticker text")
	}
}

func TestQuoteHandlerValidation(t *testing.T) {
	// POST without required fields should return error
	form := url.Values{
		"csrf":       {generateCSRFToken("quote")},
		"from":       {"ETH"},
		"from_net":   {"eth"},
		"to":         {"USDT"},
		"to_net":     {"eth"},
		"amount":     {""},      // missing
		"recipient":  {""},      // missing
		"refund_addr": {""},     // missing
		"slippage":   {"1"},
		"deadline":   {"1h"},
	}

	req := httptest.NewRequest("POST", "/quote", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleQuote(w, req)

	if w.Code != 400 {
		t.Errorf("POST /quote with empty fields: got %d, want 400", w.Code)
	}
}

func TestQuoteHandlerCSRFReject(t *testing.T) {
	form := url.Values{
		"csrf":       {"invalid-csrf-token"},
		"from":       {"ETH"},
		"from_net":   {"eth"},
		"to":         {"USDT"},
		"to_net":     {"eth"},
		"amount":     {"1"},
		"recipient":  {"0xabc"},
		"refund_addr": {"0xdef"},
		"slippage":   {"1"},
		"deadline":   {"1h"},
	}

	req := httptest.NewRequest("POST", "/quote", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleQuote(w, req)

	if w.Code != 403 {
		t.Errorf("POST /quote with bad CSRF: got %d, want 403", w.Code)
	}
}

func TestQuoteHandlerGetRedirects(t *testing.T) {
	req := httptest.NewRequest("GET", "/quote", nil)
	w := httptest.NewRecorder()
	handleQuote(w, req)

	if w.Code != 303 {
		t.Errorf("GET /quote should redirect: got %d, want 303", w.Code)
	}
}

func TestQuoteHandlerEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping API test in short mode")
	}

	// Ensure cache is populated
	if err := refreshTokenCache(); err != nil {
		t.Fatalf("refreshTokenCache() failed: %v", err)
	}

	form := url.Values{
		"csrf":       {generateCSRFToken("quote")},
		"from":       {"ETH"},
		"from_net":   {"eth"},
		"to":         {"USDT"},
		"to_net":     {"eth"},
		"amount":     {"1"},
		"recipient":  {"0x000000000000000000000000000000000000dEaD"},
		"refund_addr": {"0x000000000000000000000000000000000000dEaD"},
		"slippage":   {"1"},
		"deadline":   {"1h"},
	}

	req := httptest.NewRequest("POST", "/quote", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleQuote(w, req)

	if w.Code == 502 {
		t.Skip("quote API unavailable (may be temporary)")
	}
	if w.Code != 200 {
		body := w.Body.String()
		t.Fatalf("POST /quote status: got %d, want 200\nBody: %s", w.Code, body[:min(500, len(body))])
	}

	body := w.Body.String()

	// Verify the quote page has actual data
	checks := []string{
		"You Send",
		"You Receive",
		"ETH",
		"USDT",
		"Fee Breakdown",
		"Confirm Swap",
	}
	for _, check := range checks {
		if !strings.Contains(body, check) {
			t.Errorf("quote page missing %q", check)
		}
	}

	// The amount out should NOT be "0" — this was the bug the user reported
	if strings.Contains(body, `<div class="quote-card__amount">0 USDT</div>`) {
		t.Error("BUG: quote page shows 0 USDT — amount estimate is not being parsed correctly")
	}

	t.Logf("Quote page rendered successfully with real API data")
}

// ════════════════════════════════════════════════════════════
// QR Code Tests
// ════════════════════════════════════════════════════════════

func TestGenerateQRSVG(t *testing.T) {
	svg := generateQRSVG("0x1234567890abcdef", 200)
	if svg == "" {
		t.Fatal("generateQRSVG returned empty string")
	}
	if !strings.Contains(svg, "<svg") {
		t.Error("QR output is not valid SVG")
	}
	if !strings.Contains(svg, "rect") {
		t.Error("QR SVG missing rect elements")
	}
}

// ════════════════════════════════════════════════════════════
// Rate Limiter Tests
// ════════════════════════════════════════════════════════════

func TestRateLimiter(t *testing.T) {
	rl := &rateLimiter{counters: make(map[string]*rateBucket)}

	// Should allow first N requests
	for i := 0; i < 5; i++ {
		if !rl.allow("1.2.3.4", 5, time.Minute) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 6th request should be denied
	if rl.allow("1.2.3.4", 5, time.Minute) {
		t.Error("6th request should be denied")
	}

	// Different IP should still be allowed
	if !rl.allow("5.6.7.8", 5, time.Minute) {
		t.Error("different IP should be allowed")
	}
}

func TestRateLimiterIPPrefix(t *testing.T) {
	rl := &rateLimiter{counters: make(map[string]*rateBucket)}

	// Same /24 prefix should share rate limit
	for i := 0; i < 3; i++ {
		rl.allow("192.168.1.1", 3, time.Minute)
	}

	// Same /24 — should be denied
	if rl.allow("192.168.1.99", 3, time.Minute) {
		t.Error("same /24 prefix should share rate limit bucket")
	}
}

// ════════════════════════════════════════════════════════════
// Icon Generation Tests
// ════════════════════════════════════════════════════════════

func TestGenerateTokenIconSVG(t *testing.T) {
	tickers := []string{"ETH", "BTC", "USDT", "SOL", "NEAR", "XRP"}

	for _, ticker := range tickers {
		svg := generateTokenIconSVG(ticker)
		if !strings.Contains(svg, "<svg") {
			t.Errorf("icon for %s is not valid SVG", ticker)
		}
		if !strings.Contains(svg, ticker) {
			t.Errorf("icon for %s missing ticker text", ticker)
		}
	}
}

// ════════════════════════════════════════════════════════════
// Static File Serving Test
// ════════════════════════════════════════════════════════════

func TestStaticCSS(t *testing.T) {
	mux := http.NewServeMux()

	// Replicate static file serving from main.go
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	req := httptest.NewRequest("GET", "/static/style.css", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET /static/style.css status: got %d, want 200", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "--bg:") {
		t.Error("CSS missing --bg variable")
	}
	if !strings.Contains(body, "#000000") {
		t.Error("CSS should use OLED black (#000000)")
	}
}

func TestStaticFavicon(t *testing.T) {
	mux := http.NewServeMux()

	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	req := httptest.NewRequest("GET", "/static/favicon.ico", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET /static/favicon.ico status: got %d, want 200", w.Code)
	}
}

func TestStaticLogo(t *testing.T) {
	mux := http.NewServeMux()

	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	req := httptest.NewRequest("GET", "/static/logo.png", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("GET /static/logo.png status: got %d, want 200", w.Code)
	}
}

// ════════════════════════════════════════════════════════════
// HasJWT Fee Display Test
// ════════════════════════════════════════════════════════════

func TestFeeDisplayWithoutJWT(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping API test in short mode")
	}

	// When no JWT is set, HasJWT should be false
	saved := nearIntentsJWT
	nearIntentsJWT = ""
	defer func() { nearIntentsJWT = saved }()

	if err := refreshTokenCache(); err != nil {
		t.Fatalf("refreshTokenCache() failed: %v", err)
	}

	form := url.Values{
		"csrf":       {generateCSRFToken("quote")},
		"from":       {"ETH"},
		"from_net":   {"eth"},
		"to":         {"USDT"},
		"to_net":     {"eth"},
		"amount":     {"1"},
		"recipient":  {"0x000000000000000000000000000000000000dEaD"},
		"refund_addr": {"0x000000000000000000000000000000000000dEaD"},
		"slippage":   {"1"},
		"deadline":   {"1h"},
	}

	req := httptest.NewRequest("POST", "/quote", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	handleQuote(w, req)

	if w.Code != 200 {
		t.Skip("quote API call failed — network issue")
	}

	body := w.Body.String()

	// Without JWT, protocol fee should show "Standard rate"
	if !strings.Contains(body, "Standard rate") {
		t.Error("without JWT, protocol fee should show 'Standard rate'")
	}

	// uSwap Zero fee should always show Ø
	if !strings.Contains(body, "\u00d8") { // Ø
		t.Error("uSwap Zero fee should always show Ø symbol")
	}
}

// ════════════════════════════════════════════════════════════
// Helper Tests
// ════════════════════════════════════════════════════════════

func TestBuildDeadline(t *testing.T) {
	dl := buildDeadline(time.Hour)
	parsed, err := time.Parse(time.RFC3339, dl)
	if err != nil {
		t.Fatalf("buildDeadline returned invalid RFC3339: %v", err)
	}

	diff := time.Until(parsed)
	if diff < 59*time.Minute || diff > 61*time.Minute {
		t.Errorf("deadline should be ~1h from now, got %v", diff)
	}
}

func TestFormatRate(t *testing.T) {
	tests := []struct {
		rate float64
		want string
	}{
		{1826.34, "1,826.34"},
		{1.50, "1.50"},
		{0.0005, "0.000500"},
		{0.00000001, "0.00000001"},
	}

	for _, tt := range tests {
		got := formatRate(tt.rate)
		if got != tt.want {
			t.Errorf("formatRate(%v) = %q, want %q", tt.rate, got, tt.want)
		}
	}
}

// ════════════════════════════════════════════════════════════
// JSON Serialization Tests
// ════════════════════════════════════════════════════════════

func TestQuoteRequestJSON(t *testing.T) {
	req := &QuoteRequest{
		Dry:               true,
		SwapType:          "FLEX_INPUT",
		SlippageTolerance: 100,
		Amount:            "1000000000000000000",
		AppFees:           []struct{}{},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal QuoteRequest failed: %v", err)
	}

	// Verify AppFees serializes as empty array, not null
	if !strings.Contains(string(data), `"appFees":[]`) {
		t.Errorf("AppFees should serialize as [], got: %s", string(data))
	}
}

// Helper
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
