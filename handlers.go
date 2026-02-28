package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
)

// Token brand colors for dynamic accent theming.
var tokenColors = map[string]string{
	"BTC":   "#F7931A",
	"ETH":   "#627EEA",
	"USDT":  "#50AF95",
	"USDC":  "#2775CA",
	"SOL":   "#9945FF",
	"BNB":   "#F3BA2F",
	"XRP":   "#23B5E8",
	"DOGE":  "#C2A633",
	"AVAX":  "#E84142",
	"DOT":   "#E6007A",
	"MATIC": "#8247E5",
	"NEAR":  "#00EC97",
	"UNI":   "#FF007A",
	"LINK":  "#2A5ADA",
	"DAI":   "#F5AC37",
	"AAVE":  "#B6509E",
	"WBTC":  "#F09242",
	"WETH":  "#627EEA",
	"ARB":   "#28A0F0",
	"OP":    "#FF0420",
	"TON":   "#0098EA",
	"LTC":   "#BFBBBB",
	"SHIB":  "#FFA409",
	"TRX":   "#FF0013",
}

func hexToRGB(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return fmt.Sprintf("%d, %d, %d", r, g, b)
}

func tokenColorPair(ticker string) (string, string) {
	hex := "#ffffff"
	if c, ok := tokenColors[strings.ToUpper(ticker)]; ok {
		hex = c
	}
	return hex, hexToRGB(hex)
}

// PageData is the base data passed to every template.
type PageData struct {
	Title       string
	Error       string
	MetaRefresh int // seconds; 0 = no refresh
	FromColor   string
	FromColorA  string
	ToColor     string
	ToColorA    string
	CommitHash  string
	BuildTime   string
	BuildLogURL string
	OnionURL    string
}

func newPageData(title string) PageData {
	return PageData{
		Title:       title,
		FromColor:   "#ffffff",
		FromColorA:  "255, 255, 255",
		ToColor:     "#ffffff",
		ToColorA:    "255, 255, 255",
		CommitHash:  commitHash,
		BuildTime:   buildTime,
		BuildLogURL: buildLogURL,
		OnionURL:    onionURL,
	}
}

// SwapPageData is the data for the swap form page.
type SwapPageData struct {
	PageData
	From       string
	FromNet    string
	To         string
	ToNet      string
	Amount     string
	AmountOut  string // receive amount for EXACT_OUTPUT
	Recipient  string
	RefundAddr string
	Slippage   string
	CSRFToken  string
	Networks   []NetworkGroup
	SearchFrom string
	SearchTo   string
	ModalOpen  string // "from" or "to" if a modal should be open
	FromToken  *TokenInfo
	ToToken    *TokenInfo
}

// QuotePageData is the data for the quote preview page.
type QuotePageData struct {
	PageData
	From            string
	FromNet         string
	FromTicker      string
	To              string
	ToNet           string
	ToTicker        string
	AmountIn        string
	AmountInUSD     string
	AmountOut       string
	AmountOutUSD    string
	Rate            string
	Recipient       string
	RefundAddr      string
	Slippage        string
	SlippageBPS     int
	CSRFToken       string
	OriginAsset     string
	DestAsset       string
	AtomicAmount    string
	SpreadUSD       string
	SpreadPct       string
	FromToken       *TokenInfo
	ToToken         *TokenInfo
	HasJWT          bool   // true if NEAR_INTENTS_JWT is set (0% protocol fee)
	SwapType        string // FLEX_INPUT or EXACT_OUTPUT
}

// OrderPageData is the data for the order status page.
type OrderPageData struct {
	PageData
	Token         string
	Order         *OrderData
	Status        *StatusResponse
	QRCode        string
	TimeRemaining string
	IsTerminal    bool
	StatusStep    int // 0=pending, 1=processing, 2=complete
	Withdrawals   *AnyInputWithdrawalsResponse
}

// CurrenciesPageData is the data for the currencies list page.
type CurrenciesPageData struct {
	PageData
	Networks   []NetworkGroup
	TotalCount int
	Search     string
}

func renderError(w http.ResponseWriter, status int, title, message, action, actionURL string) {
	w.WriteHeader(status)
	templates.ExecuteTemplate(w, "error.html", struct {
		PageData
		Message   string
		Action    string
		ActionURL string
	}{
		PageData:  newPageData(title),
		Message:   message,
		Action:    action,
		ActionURL: actionURL,
	})
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx > 0 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}

// handleSwap renders the main swap form.
func handleSwap(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		renderError(w, 404, "Not Found", "Page not found.", "Back to Home", "/")
		return
	}

	networks, _ := getNetworkGroups()

	data := SwapPageData{
		PageData:   newPageData("uSwap Zero"),
		From:       r.URL.Query().Get("from"),
		FromNet:    r.URL.Query().Get("from_net"),
		To:         r.URL.Query().Get("to"),
		ToNet:      r.URL.Query().Get("to_net"),
		Amount:     r.URL.Query().Get("amt"),
		AmountOut:  r.URL.Query().Get("amt_out"),
		Recipient:  r.URL.Query().Get("recipient"),
		Slippage:   r.URL.Query().Get("slippage"),
		CSRFToken:  generateCSRFToken("quote"),
		Networks:   networks,
		SearchFrom: r.URL.Query().Get("search_from"),
		SearchTo:   r.URL.Query().Get("search_to"),
		ModalOpen:  r.URL.Query().Get("modal"),
	}

	// Defaults
	if data.From == "" {
		data.From = "ETH"
		data.FromNet = "eth"
	}
	if data.To == "" {
		data.To = "USDT"
		data.ToNet = "eth"
	}
	if data.Slippage == "" {
		data.Slippage = "1"
	}

	// Set accent colors from selected currencies
	data.FromColor, data.FromColorA = tokenColorPair(data.From)
	data.ToColor, data.ToColorA = tokenColorPair(data.To)

	// Look up token info for display
	data.FromToken = findToken(data.From, data.FromNet)
	data.ToToken = findToken(data.To, data.ToNet)

	// Filter networks if search is active
	if data.SearchFrom != "" || data.SearchTo != "" {
		query := data.SearchFrom
		if query == "" {
			query = data.SearchTo
		}
		filtered := filterNetworks(networks, query)
		data.Networks = filtered
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "swap.html", data)
}

// handleQuote processes the quote form and shows a price preview.
func handleQuote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	r.ParseForm()

	// Rate limit
	ip := clientIP(r)
	if !limiter.allow(ip, 30, time.Minute) {
		renderError(w, 429, "Too Many Requests", "Please wait a moment before trying again.", "Back to Home", "/")
		return
	}

	// CSRF check
	if !verifyCSRFToken(r.FormValue("csrf"), "quote", time.Hour) {
		renderError(w, 403, "Invalid Request", "Form expired. Please go back and try again.", "Back to Home", "/")
		return
	}

	fromTicker := strings.ToUpper(r.FormValue("from"))
	fromNet := r.FormValue("from_net")
	toTicker := strings.ToUpper(r.FormValue("to"))
	toNet := r.FormValue("to_net")
	amount := r.FormValue("amount")
	amountOutForm := r.FormValue("amount_out")
	recipient := strings.TrimSpace(r.FormValue("recipient"))
	refundAddr := strings.TrimSpace(r.FormValue("refund_addr"))
	slippage := r.FormValue("slippage")

	// Validation (amount is optional — determines swap type)
	var errors []string
	if recipient == "" {
		errors = append(errors, "Recipient address is required")
	}
	if refundAddr == "" {
		errors = append(errors, "Refund address is required")
	}
	if len(errors) > 0 {
		renderError(w, 400, "Validation Error", "Please check your input:\n"+strings.Join(errors, "\n"), "Go Back", "/")
		return
	}

	// Find tokens
	fromToken := findToken(fromTicker, fromNet)
	toToken := findToken(toTicker, toNet)
	if fromToken == nil || toToken == nil {
		renderError(w, 400, "Unknown Token", "Could not find the selected tokens. Try selecting them again.", "Go Back", "/")
		return
	}

	slippageBPS, err := slippageToBPS(slippage)
	if err != nil {
		slippageBPS = 100 // default 1%
	}

	// Auto-detect swap type based on which amount field is filled.
	// Both filled: prefer send amount (FLEX_INPUT).
	swapType := "FLEX_INPUT"
	if amount == "" && amountOutForm == "" {
		swapType = "ANY_INPUT"
	} else if amountOutForm != "" && amount == "" {
		swapType = "EXACT_OUTPUT"
	}

	// ANY_INPUT: skip dry quote, go directly to real quote → deposit page.
	if swapType == "ANY_INPUT" {
		refAmount, _ := humanToAtomic("1", fromToken.Decimals)
		quoteReq := &QuoteRequest{
			Dry:                false,
			SwapType:           "ANY_INPUT",
			SlippageTolerance:  slippageBPS,
			OriginAsset:        fromToken.DefuseAssetID,
			DepositType:        "ORIGIN_CHAIN",
			DestinationAsset:   toToken.DefuseAssetID,
			Amount:             refAmount,
			RefundTo:           refundAddr,
			RefundType:         "ORIGIN_CHAIN",
			Recipient:          recipient,
			RecipientType:      "DESTINATION_CHAIN",
			Deadline:           buildDeadline(time.Hour),
			QuoteWaitingTimeMs: 8000,
			AppFees:            []struct{}{},
		}
		quoteResp, err := requestQuote(quoteReq)
		if err != nil {
			renderError(w, 502, "Quick Swap Failed", "NEAR Intents API is temporarily unavailable. This usually resolves in a few minutes.", "Try Again", "/")
			return
		}
		orderData := &OrderData{
			DepositAddr: quoteResp.Quote.DepositAddress,
			Memo:        quoteResp.Quote.DepositMemo,
			FromTicker:  fromTicker,
			FromNet:     fromNet,
			ToTicker:    toTicker,
			ToNet:       toNet,
			AmountIn:    "any",
			AmountOut:   "market rate",
			Deadline:    quoteResp.Quote.Deadline,
			CorrID:      quoteResp.CorrelationID,
			RefundAddr:  refundAddr,
			RecvAddr:    recipient,
			SwapType:    "ANY_INPUT",
		}
		token, err := encryptOrderData(orderData)
		if err != nil {
			renderError(w, 500, "Internal Error", "Failed to create order token.", "Back to Home", "/")
			return
		}
		http.Redirect(w, r, "/order/"+token, http.StatusFound)
		return
	}

	// FLEX_INPUT or EXACT_OUTPUT: convert the appropriate amount to atomic.
	var atomicAmount string
	if swapType == "EXACT_OUTPUT" {
		atomicAmount, err = humanToAtomic(amountOutForm, toToken.Decimals)
	} else {
		atomicAmount, err = humanToAtomic(amount, fromToken.Decimals)
	}
	if err != nil {
		renderError(w, 400, "Invalid Amount", "Could not parse the amount: "+err.Error(), "Go Back", "/")
		return
	}

	// Request dry quote from NEAR Intents
	quoteReq := &QuoteRequest{
		Dry:                true,
		SwapType:           swapType,
		SlippageTolerance:  slippageBPS,
		OriginAsset:        fromToken.DefuseAssetID,
		DepositType:        "ORIGIN_CHAIN",
		DestinationAsset:   toToken.DefuseAssetID,
		Amount:             atomicAmount,
		RefundTo:           refundAddr,
		RefundType:         "ORIGIN_CHAIN",
		Recipient:          recipient,
		RecipientType:      "DESTINATION_CHAIN",
		Deadline:           buildDeadline(time.Hour),
		QuoteWaitingTimeMs: 8000,
		AppFees:            []struct{}{},
	}

	dryResp, err := requestDryQuote(quoteReq)
	if err != nil {
		renderError(w, 502, "Quote Failed", "NEAR Intents API is temporarily unavailable. This usually resolves in a few minutes.", "Try Again", "/")
		return
	}

	// Extract amounts from dry quote response.
	// For EXACT_OUTPUT, AmountIn is estimated and AmountOut is exact.
	// For FLEX_INPUT, both are approximate.
	humanIn := dryResp.Quote.AmountInFormatted
	humanOut := dryResp.Quote.AmountOutFormatted
	if humanIn == "" {
		humanIn = amount
	}
	if humanOut == "" {
		humanOut = atomicToHuman(dryResp.Quote.AmountOut, toToken.Decimals)
	}

	if dryResp.Quote.AmountOut == "" || dryResp.Quote.AmountOut == "0" {
		renderError(w, 502, "Quote Unavailable", "No market makers are currently offering a rate for this pair/amount. Try a larger amount or a different pair.", "Go Back", "/")
		return
	}

	// USD values
	amountInUSD := ""
	amountOutUSD := ""
	spreadUSD := ""
	spreadPct := ""
	rate := ""

	inFloat, _ := parseFloat(humanIn)
	outFloat, _ := parseFloat(humanOut)

	if fromToken.Price > 0 && inFloat > 0 {
		inUSD := inFloat * fromToken.Price
		amountInUSD = formatUSD(inUSD)

		if toToken.Price > 0 && outFloat > 0 {
			outUSD := outFloat * toToken.Price
			amountOutUSD = formatUSD(outUSD)

			spread := inUSD - outUSD
			if spread < 0 {
				spread = 0
			}
			spreadUSD = formatUSD(spread)
			if inUSD > 0 {
				spreadPct = fmt.Sprintf("%.2f%%", (spread/inUSD)*100)
			}

			rate = fmt.Sprintf("1 %s = %s %s", fromTicker, formatRate(outFloat/inFloat), toTicker)
		}
	}

	data := QuotePageData{
		PageData:     newPageData("Quote Preview"),
		From:         fromTicker,
		FromNet:      fromNet,
		FromTicker:   fromTicker,
		To:           toTicker,
		ToNet:        toNet,
		ToTicker:     toTicker,
		AmountIn:     humanIn,
		AmountInUSD:  amountInUSD,
		AmountOut:    humanOut,
		AmountOutUSD: amountOutUSD,
		Rate:         rate,
		Recipient:    recipient,
		RefundAddr:   refundAddr,
		Slippage:     slippage,
		SlippageBPS:  slippageBPS,
		CSRFToken:    generateCSRFToken("swap"),
		OriginAsset:  fromToken.DefuseAssetID,
		DestAsset:    toToken.DefuseAssetID,
		AtomicAmount: atomicAmount,
		SpreadUSD:    spreadUSD,
		SpreadPct:    spreadPct,
		FromToken:    fromToken,
		ToToken:      toToken,
		HasJWT:       nearIntentsJWT != "",
		SwapType:     swapType,
	}

	data.FromColor, data.FromColorA = tokenColorPair(fromTicker)
	data.ToColor, data.ToColorA = tokenColorPair(toTicker)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "quote.html", data)
}

// handleSwapConfirm creates a real quote and redirects to the order page.
func handleSwapConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	r.ParseForm()

	ip := clientIP(r)
	if !limiter.allow(ip, 10, time.Minute) {
		renderError(w, 429, "Too Many Requests", "Please wait before creating another swap.", "Back to Home", "/")
		return
	}

	if !verifyCSRFToken(r.FormValue("csrf"), "swap", time.Hour) {
		renderError(w, 403, "Invalid Request", "Form expired. Please start over.", "Back to Home", "/")
		return
	}

	fromTicker := strings.ToUpper(r.FormValue("from"))
	fromNet := r.FormValue("from_net")
	toTicker := strings.ToUpper(r.FormValue("to"))
	toNet := r.FormValue("to_net")
	atomicAmount := r.FormValue("atomic_amount")
	userAmountIn := r.FormValue("amount_in")   // user's original input
	userAmountOut := r.FormValue("amount_out")  // user's original output (EXACT_OUTPUT)
	recipient := r.FormValue("recipient")
	refundAddr := r.FormValue("refund_addr")
	slippageBPS := r.FormValue("slippage_bps")
	swapType := r.FormValue("swap_type")
	if swapType == "" {
		swapType = "FLEX_INPUT"
	}

	fromToken := findToken(fromTicker, fromNet)
	toToken := findToken(toTicker, toNet)
	if fromToken == nil || toToken == nil {
		renderError(w, 400, "Unknown Token", "Token not found.", "Back to Home", "/")
		return
	}

	bps := 100
	fmt.Sscanf(slippageBPS, "%d", &bps)

	// Real quote (not dry)
	quoteReq := &QuoteRequest{
		Dry:                false,
		SwapType:           swapType,
		SlippageTolerance:  bps,
		OriginAsset:        fromToken.DefuseAssetID,
		DepositType:        "ORIGIN_CHAIN",
		DestinationAsset:   toToken.DefuseAssetID,
		Amount:             atomicAmount,
		RefundTo:           refundAddr,
		RefundType:         "ORIGIN_CHAIN",
		Recipient:          recipient,
		RecipientType:      "DESTINATION_CHAIN",
		Deadline:           buildDeadline(time.Hour),
		QuoteWaitingTimeMs: 8000,
		AppFees:            []struct{}{},
	}

	quoteResp, err := requestQuote(quoteReq)
	if err != nil {
		renderError(w, 502, "Swap Failed", "NEAR Intents API is temporarily unavailable. This usually resolves in a few minutes.", "Try Again", "/")
		return
	}

	// For FLEX_INPUT, use the user's original amount (the API may return a
	// different amountIn since FLEX_INPUT accepts a range). For EXACT_OUTPUT,
	// use the user's desired output and the API's estimated input.
	amountIn := quoteResp.Quote.AmountInFmt
	amountOut := quoteResp.Quote.AmountOutFmt
	if swapType == "FLEX_INPUT" && userAmountIn != "" {
		amountIn = userAmountIn
	}
	if swapType == "EXACT_OUTPUT" && userAmountOut != "" {
		amountOut = userAmountOut
	}

	orderData := &OrderData{
		DepositAddr: quoteResp.Quote.DepositAddress,
		Memo:        quoteResp.Quote.DepositMemo,
		FromTicker:  fromTicker,
		FromNet:     fromNet,
		ToTicker:    toTicker,
		ToNet:       toNet,
		AmountIn:    amountIn,
		AmountOut:   amountOut,
		Deadline:    quoteResp.Quote.Deadline,
		CorrID:      quoteResp.CorrelationID,
		RefundAddr:  refundAddr,
		RecvAddr:    recipient,
		SwapType:    swapType,
	}

	token, err := encryptOrderData(orderData)
	if err != nil {
		renderError(w, 500, "Internal Error", "Failed to create order token.", "Back to Home", "/")
		return
	}

	http.Redirect(w, r, "/order/"+token, http.StatusFound)
}

// handleOrder renders the order status page.
func handleOrder(w http.ResponseWriter, r *http.Request) {
	// Extract token from path: /order/{token} or /order/{token}/raw
	path := strings.TrimPrefix(r.URL.Path, "/order/")
	isRaw := strings.HasSuffix(path, "/raw")
	if isRaw {
		path = strings.TrimSuffix(path, "/raw")
	}

	if path == "" {
		renderError(w, 400, "Missing Order", "No order token provided.", "Create New Swap", "/")
		return
	}

	order, err := decryptOrderData(path)
	if err != nil {
		renderError(w, 400, "Invalid Order", "This order link is invalid or expired. It may have been created on a different server.", "Create New Swap", "/")
		return
	}

	// Fetch live status from NEAR Intents
	status, err := fetchStatus(order.DepositAddr, order.Memo)
	if err != nil {
		// If API is down, still show what we know from the token
		status = &StatusResponse{Status: "UNKNOWN"}
	}

	if isRaw {
		w.Header().Set("Content-Type", "application/json")
		if status.RawJSON != nil {
			w.Write(status.RawJSON)
		} else {
			json.NewEncoder(w).Encode(status)
		}
		return
	}

	// Determine status step and terminal state
	isTerminal := false
	statusStep := 0
	switch status.Status {
	case "PENDING_DEPOSIT":
		statusStep = 0
	case "PROCESSING":
		statusStep = 1
	case "SUCCESS":
		statusStep = 2
		isTerminal = true
	case "REFUNDED", "FAILED", "INCOMPLETE_DEPOSIT":
		statusStep = 2
		isTerminal = true
	default:
		statusStep = 0
	}

	// Calculate time remaining
	timeRemaining := ""
	if order.Deadline != "" {
		dl, err := time.Parse(time.RFC3339, order.Deadline)
		if err == nil {
			remaining := time.Until(dl)
			if remaining > 0 {
				mins := int(remaining.Minutes())
				if mins >= 60 {
					timeRemaining = fmt.Sprintf("%dh %dm", mins/60, mins%60)
				} else {
					timeRemaining = fmt.Sprintf("%dm", mins)
				}
			} else {
				timeRemaining = "Expired"
			}
		}
	}

	// Generate QR code
	qrData := order.DepositAddr
	qrSVG := generateQRSVG(qrData, 200)

	refresh := 0
	if !isTerminal {
		refresh = 10
	}

	// For ANY_INPUT orders, fetch withdrawal history.
	var withdrawals *AnyInputWithdrawalsResponse
	if order.SwapType == "ANY_INPUT" {
		withdrawals, _ = fetchAnyInputWithdrawals(order.DepositAddr)
	}

	data := OrderPageData{
		PageData:      newPageData("Order Status"),
		Token:         path,
		Order:         order,
		Status:        status,
		QRCode:        qrSVG,
		TimeRemaining: timeRemaining,
		IsTerminal:    isTerminal,
		StatusStep:    statusStep,
		Withdrawals:   withdrawals,
	}
	data.MetaRefresh = refresh
	data.FromColor, data.FromColorA = tokenColorPair(order.FromTicker)
	data.ToColor, data.ToColorA = tokenColorPair(order.ToTicker)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "order.html", data)
}

// handleCurrencies renders the full currency list.
func handleCurrencies(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")

	networks, err := getNetworkGroups()
	if err != nil {
		renderError(w, 502, "Unavailable", "Could not load currency list. NEAR Intents API may be temporarily unavailable.", "Try Again", "/currencies")
		return
	}

	totalCount := 0
	if search != "" {
		networks = filterNetworks(networks, search)
	}
	for _, ng := range networks {
		totalCount += len(ng.Tokens)
	}

	data := CurrenciesPageData{
		PageData:   newPageData("Supported Currencies"),
		Networks:   networks,
		TotalCount: totalCount,
		Search:     search,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "currencies.html", data)
}

// handleHowItWorks renders the educational page.
func handleHowItWorks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "how_it_works.html", newPageData("How It Works"))
}

// ResellerStats holds formatted display strings for a single reseller.
type ResellerStats struct {
	TotalSwaps   string
	TotalVolume  string
	TotalRevenue string
	FirstTx      string
	DaysActive   int
	DailyRevenue string
	UniqueSenders string
	BiggestUSD   string
}

// CombinedStats holds formatted combined stats.
type CombinedStats struct {
	TotalVolume  string
	TotalRevenue string
	TotalSwaps   string
	UniqueUsers  string
}

// CaseStudyPageData is the data for the case study page.
type CaseStudyPageData struct {
	PageData
	Eagle    ResellerStats
	Lizard   ResellerStats
	SwapMy   ResellerStats
	Combined CombinedStats
}

// caseStudyData is initialized once at startup from the embedded JSON.
var caseStudyData CaseStudyPageData

// rawAnalysis is the structure matching the JSON file.
type rawAnalysis struct {
	EagleSwap  rawReseller `json:"EagleSwap"`
	LizardSwap rawReseller `json:"LizardSwap"`
	SwapMy     rawReseller `json:"SwapMy"`
}

type rawReseller struct {
	TotalSwaps     int     `json:"total_swaps"`
	TotalVolumeUSD float64 `json:"total_volume_usd"`
	TotalRevenueUSD float64 `json:"total_revenue_usd"`
	UniqueSenders  int     `json:"unique_senders"`
	FirstTx        string  `json:"first_tx"`
	DaysActive     int     `json:"days_active"`
	DailyRevenueUSD float64 `json:"daily_revenue_usd"`
	BiggestSwapUSD float64 `json:"biggest_swap_usd"`
}

func formatResellerStats(r rawReseller) ResellerStats {
	return ResellerStats{
		TotalSwaps:    formatCommas(int64(r.TotalSwaps)),
		TotalVolume:   formatUSD(r.TotalVolumeUSD),
		TotalRevenue:  formatUSD(r.TotalRevenueUSD),
		FirstTx:       r.FirstTx,
		DaysActive:    r.DaysActive,
		DailyRevenue:  formatUSD(r.DailyRevenueUSD),
		UniqueSenders: formatCommas(int64(r.UniqueSenders)),
		BiggestUSD:    formatUSD(r.BiggestSwapUSD),
	}
}

func initCaseStudy() {
	var raw rawAnalysis
	if err := json.Unmarshal(analysisJSON, &raw); err != nil {
		log.Printf("WARNING: Failed to parse case study data: %v", err)
		return
	}

	caseStudyData.Eagle = formatResellerStats(raw.EagleSwap)
	caseStudyData.Lizard = formatResellerStats(raw.LizardSwap)
	caseStudyData.SwapMy = formatResellerStats(raw.SwapMy)
	caseStudyData.Combined = CombinedStats{
		TotalVolume:  formatUSD(raw.EagleSwap.TotalVolumeUSD + raw.LizardSwap.TotalVolumeUSD + raw.SwapMy.TotalVolumeUSD),
		TotalRevenue: formatUSD(raw.EagleSwap.TotalRevenueUSD + raw.LizardSwap.TotalRevenueUSD + raw.SwapMy.TotalRevenueUSD),
		TotalSwaps:   formatCommas(int64(raw.EagleSwap.TotalSwaps + raw.LizardSwap.TotalSwaps + raw.SwapMy.TotalSwaps)),
		UniqueUsers:  formatCommas(int64(raw.EagleSwap.UniqueSenders + raw.LizardSwap.UniqueSenders + raw.SwapMy.UniqueSenders)),
	}
}

// handleCaseStudy renders the competitor analysis page.
func handleCaseStudy(w http.ResponseWriter, r *http.Request) {
	data := CaseStudyPageData{
		PageData: newPageData("The Crypto Swap Reseller Problem"),
		Eagle:    caseStudyData.Eagle,
		Lizard:   caseStudyData.Lizard,
		SwapMy:   caseStudyData.SwapMy,
		Combined: caseStudyData.Combined,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "case_study.html", data)
}

// VerifyPageData is the data for the /verify page.
type VerifyPageData struct {
	PageData
	GoVersion   string
	Uptime      string
	Requests    string
	BinarySize  string
	EnvVars     []EnvVarStatus
}

// EnvVarStatus shows whether an env var is configured.
type EnvVarStatus struct {
	Key   string
	Set   bool
}

// handleVerify renders the deployment verification page.
func handleVerify(w http.ResponseWriter, r *http.Request) {
	// Go version from build info
	goVersion := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		goVersion = info.GoVersion
	}

	// Uptime
	uptime := time.Since(serverStartTime).Round(time.Second).String()

	// Request count
	reqs := formatCommas(atomic.LoadInt64(&requestCounter))

	// Binary size
	binSize := "unknown"
	if exe, err := os.Executable(); err == nil {
		if fi, err := os.Stat(exe); err == nil {
			mb := float64(fi.Size()) / 1024 / 1024
			binSize = fmt.Sprintf("%.1f MB", mb)
		}
	}

	// Env var status (key names only — never values)
	envKeys := []string{
		"ORDER_SECRET", "NEAR_INTENTS_JWT", "NEAR_INTENTS_EXPLORER_JWT", "NEAR_INTENTS_API_URL", "PORT",
		"TG_BOT_TOKEN", "TG_APP_URL", "TG_WEBHOOK_SECRET",
		"TG_MONITOR_GROUP_ID", "TG_MAIN_CHAT_ID",
		"TG_SWAPMY_THREAD_ID", "TG_EAGLESWAP_THREAD_ID", "TG_LIZARDSWAP_THREAD_ID",
	}
	var envVars []EnvVarStatus
	for _, k := range envKeys {
		envVars = append(envVars, EnvVarStatus{Key: k, Set: os.Getenv(k) != ""})
	}

	data := VerifyPageData{
		PageData:  newPageData("Verify"),
		GoVersion: goVersion,
		Uptime:    uptime,
		Requests:  reqs,
		BinarySize: binSize,
		EnvVars:   envVars,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "verify.html", data)
}

// handleGenIcon serves dynamically generated token icon SVGs.
func handleGenIcon(w http.ResponseWriter, r *http.Request) {
	ticker := strings.TrimPrefix(r.URL.Path, "/icons/gen/")
	ticker = strings.ToUpper(ticker)
	if ticker == "" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	fmt.Fprint(w, generateTokenIconSVG(ticker))
}

// filterNetworks filters network groups by a search query.
func filterNetworks(networks []NetworkGroup, query string) []NetworkGroup {
	q := strings.ToLower(query)
	var filtered []NetworkGroup
	for _, ng := range networks {
		var tokens []TokenInfo
		for _, t := range ng.Tokens {
			if strings.Contains(strings.ToLower(t.Ticker), q) ||
				strings.Contains(strings.ToLower(t.Name), q) ||
				strings.Contains(strings.ToLower(ng.Name), q) {
				tokens = append(tokens, t)
			}
		}
		if len(tokens) > 0 {
			filtered = append(filtered, NetworkGroup{Name: ng.Name, Tokens: tokens})
		}
	}
	return filtered
}

// parseFloat is a simple float parser for display purposes only.
func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

// formatRate formats an exchange rate for display.
func formatRate(rate float64) string {
	if rate >= 1000 {
		return formatUSD(rate)[1:] // strip $
	}
	if rate >= 1 {
		return fmt.Sprintf("%.2f", rate)
	}
	if rate >= 0.0001 {
		return fmt.Sprintf("%.6f", rate)
	}
	// Very small rate
	return fmt.Sprintf("%.8f", math.Abs(rate))
}
