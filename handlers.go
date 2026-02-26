package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
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
	HasJWT          bool // true if NEAR_INTENTS_JWT is set (0% protocol fee)
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
	recipient := strings.TrimSpace(r.FormValue("recipient"))
	refundAddr := strings.TrimSpace(r.FormValue("refund_addr"))
	slippage := r.FormValue("slippage")

	// Validation
	var errors []string
	if amount == "" {
		errors = append(errors, "Amount is required")
	}
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

	// Convert amount to atomic
	atomicAmount, err := humanToAtomic(amount, fromToken.Decimals)
	if err != nil {
		renderError(w, 400, "Invalid Amount", "Could not parse the amount: "+err.Error(), "Go Back", "/")
		return
	}

	slippageBPS, err := slippageToBPS(slippage)
	if err != nil {
		slippageBPS = 100 // default 1%
	}

	// Request dry quote from NEAR Intents
	quoteReq := &QuoteRequest{
		Dry:                true,
		SwapType:           "EXACT_INPUT",
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
		Referral:           "uswap-zero",
		QuoteWaitingTimeMs: 8000,
		AppFees:            []struct{}{},
	}

	dryResp, err := requestDryQuote(quoteReq)
	if err != nil {
		renderError(w, 502, "Quote Failed", "NEAR Intents API is temporarily unavailable. This usually resolves in a few minutes.", "Try Again", "/")
		return
	}

	// Extract amount from nested dry quote response
	amountOut := dryResp.Quote.AmountOut
	if amountOut == "" || amountOut == "0" {
		renderError(w, 502, "Quote Unavailable", "No market makers are currently offering a rate for this pair/amount. Try a larger amount or a different pair.", "Go Back", "/")
		return
	}
	humanOut := atomicToHuman(amountOut, toToken.Decimals)

	// USD values
	amountInUSD := ""
	amountOutUSD := ""
	spreadUSD := ""
	spreadPct := ""
	rate := ""

	if fromToken.Price > 0 {
		inFloat, _ := parseFloat(amount)
		inUSD := inFloat * fromToken.Price
		amountInUSD = formatUSD(inUSD)

		if toToken.Price > 0 {
			outFloat, _ := parseFloat(humanOut)
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

			if inFloat > 0 {
				rate = fmt.Sprintf("1 %s = %s %s", fromTicker, formatRate(outFloat/inFloat), toTicker)
			}
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
		AmountIn:     amount,
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
	recipient := r.FormValue("recipient")
	refundAddr := r.FormValue("refund_addr")
	slippageBPS := r.FormValue("slippage_bps")

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
		SwapType:           "EXACT_INPUT",
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
		Referral:           "uswap-zero",
		QuoteWaitingTimeMs: 8000,
		AppFees:            []struct{}{},
	}

	quoteResp, err := requestQuote(quoteReq)
	if err != nil {
		renderError(w, 502, "Swap Failed", "NEAR Intents API is temporarily unavailable. This usually resolves in a few minutes.", "Try Again", "/")
		return
	}

	// Encrypt order data into token — use API's canonical formatted amounts.
	orderData := &OrderData{
		DepositAddr: quoteResp.Quote.DepositAddress,
		Memo:        quoteResp.Quote.DepositMemo,
		FromTicker:  fromTicker,
		FromNet:     fromNet,
		ToTicker:    toTicker,
		ToNet:       toNet,
		AmountIn:    quoteResp.Quote.AmountInFmt,
		AmountOut:   quoteResp.Quote.AmountOutFmt,
		Deadline:    quoteResp.Quote.Deadline,
		CorrID:      quoteResp.CorrelationID,
		RefundAddr:  refundAddr,
		RecvAddr:    recipient,
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

	data := OrderPageData{
		PageData:      newPageData("Order Status"),
		Token:         path,
		Order:         order,
		Status:        status,
		QRCode:        qrSVG,
		TimeRemaining: timeRemaining,
		IsTerminal:    isTerminal,
		StatusStep:    statusStep,
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

// handleVerify renders the deployment verification page.
func handleVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "verify.html", newPageData("Verify"))
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
