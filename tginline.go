package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Inline query kind constants.
const (
	inlineKindEmpty   = "empty"
	inlineKindSingle  = "single"
	inlineKindPair    = "pair"
	inlineKindPairAmt = "pairamount"
	inlineKindStatus  = "status"
)

type parsedInlineQuery struct {
	kind   string
	from   string
	to     string
	amount string
	token  string
}

// handleTGInlineQuery handles an @botname inline query from any chat.
func handleTGInlineQuery(q *TGInlineQuery) {
	parsed := parseInlineQuery(q.Query)
	var results []interface{}
	cacheTime := 30

	switch parsed.kind {
	case inlineKindEmpty:
		results = buildEmptyResults()
		cacheTime = 300
	case inlineKindSingle:
		results = buildSingleTokenResults(parsed.from)
		cacheTime = 30
	case inlineKindPair:
		results = buildPairResults(parsed.from, parsed.to)
		cacheTime = 30
	case inlineKindPairAmt:
		results = buildPairAmountResults(parsed.from, parsed.to, parsed.amount)
		cacheTime = 15
	case inlineKindStatus:
		results = buildStatusResults(parsed.token)
		cacheTime = 10
	}

	tgAnswerInlineQuery(q.ID, results, cacheTime)
}

// parseInlineQuery classifies a raw inline query string into a structured form.
func parseInlineQuery(raw string) parsedInlineQuery {
	query := strings.TrimSpace(raw)
	if query == "" {
		return parsedInlineQuery{kind: inlineKindEmpty}
	}

	parts := strings.Fields(query)

	// "status <token>" — check before uppercasing
	if len(parts) == 2 && strings.EqualFold(parts[0], "status") {
		return parsedInlineQuery{kind: inlineKindStatus, token: parts[1]}
	}

	// Normalize tickers to uppercase
	for i := range parts {
		parts[i] = strings.ToUpper(parts[i])
	}

	switch len(parts) {
	case 1:
		return parsedInlineQuery{kind: inlineKindSingle, from: parts[0]}
	case 2:
		return parsedInlineQuery{kind: inlineKindPair, from: parts[0], to: parts[1]}
	default:
		if _, err := strconv.ParseFloat(parts[2], 64); err == nil {
			return parsedInlineQuery{kind: inlineKindPairAmt, from: parts[0], to: parts[1], amount: parts[2]}
		}
		return parsedInlineQuery{kind: inlineKindPair, from: parts[0], to: parts[1]}
	}
}

// buildEmptyResults returns results for an empty inline query (@botname with no text).
func buildEmptyResults() []interface{} {
	results := []interface{}{buildStartNewSwapArticle()}

	popularPairs := [][4]string{
		{"BTC", "btc", "ETH", "eth"},
		{"ETH", "eth", "BTC", "btc"},
		{"BTC", "btc", "USDT", "eth"},
		{"ETH", "eth", "USDC", "eth"},
		{"SOL", "sol", "USDT", "eth"},
	}

	for i, pair := range popularPairs {
		from := findToken(pair[0], pair[1])
		to := findToken(pair[2], pair[3])
		if from == nil || to == nil {
			continue
		}
		fromLabel := tokenLabel(from.Ticker, from.ChainName)
		toLabel := tokenLabel(to.Ticker, to.ChainName)
		title := fmt.Sprintf("Swap %s → %s", fromLabel, toLabel)
		desc := networkDisplayName(from.ChainName) + " · Zero fees"
		if from.Price > 0 && to.Price > 0 {
			desc = fmt.Sprintf("1 %s ≈ %s %s", from.Ticker, fmtEstimate(from.Price/to.Price), to.Ticker)
		}
		results = append(results, buildSwapArticle(
			fmt.Sprintf("empty-%d", i),
			title, desc,
			from.Ticker, from.ChainName,
			to.Ticker, to.ChainName, "",
		))
	}

	return results
}

// buildSingleTokenResults returns results for a single-word query (full ticker or partial like "BT").
func buildSingleTokenResults(query string) []interface{} {
	matches := searchTokens(query)

	// Deduplicate by ticker, keep top 3
	seen := make(map[string]bool)
	var froms []TokenInfo
	for _, t := range matches {
		key := strings.ToUpper(t.Ticker)
		if !seen[key] {
			seen[key] = true
			froms = append(froms, t)
		}
		if len(froms) >= 3 {
			break
		}
	}

	if len(froms) == 0 {
		return buildEmptyResults()
	}

	popularTargets := []struct{ ticker, net string }{
		{"ETH", "eth"},
		{"USDT", "eth"},
		{"BTC", "btc"},
		{"USDC", "eth"},
		{"SOL", "sol"},
	}

	var results []interface{}
	for i, from := range froms {
		for j, target := range popularTargets {
			if strings.EqualFold(from.Ticker, target.ticker) {
				continue
			}
			to := findToken(target.ticker, target.net)
			if to == nil {
				continue
			}
			title := fmt.Sprintf("Swap %s → %s",
				tokenLabel(from.Ticker, from.ChainName),
				tokenLabel(to.Ticker, to.ChainName))
			desc := networkDisplayName(from.ChainName) + " · Zero fees"
			if from.Price > 0 && to.Price > 0 {
				desc = fmt.Sprintf("1 %s ≈ %s %s",
					from.Ticker, fmtEstimate(from.Price/to.Price), to.Ticker)
			}
			results = append(results, buildSwapArticle(
				fmt.Sprintf("single-%d-%d", i, j),
				title, desc,
				from.Ticker, from.ChainName,
				to.Ticker, to.ChainName, "",
			))
			if len(results) >= 10 {
				return results
			}
		}
	}
	return results
}

// buildPairResults returns results for a two-token query (e.g. "BTC ETH").
func buildPairResults(fromQuery, toQuery string) []interface{} {
	from := findToken(fromQuery, "")
	to := findToken(toQuery, "")
	if from == nil || to == nil {
		return buildSingleTokenResults(fromQuery)
	}

	fromLabel := tokenLabel(from.Ticker, from.ChainName)
	toLabel := tokenLabel(to.Ticker, to.ChainName)

	desc := "Zero fees · Tap to get quote"
	descRev := "Swap in reverse · Zero fees"
	if from.Price > 0 && to.Price > 0 {
		desc = fmt.Sprintf("1 %s ≈ %s %s", from.Ticker, fmtEstimate(from.Price/to.Price), to.Ticker)
		descRev = fmt.Sprintf("1 %s ≈ %s %s", to.Ticker, fmtEstimate(to.Price/from.Price), from.Ticker)
	}

	return []interface{}{
		buildSwapArticle("pair-0",
			fmt.Sprintf("Swap %s → %s", fromLabel, toLabel), desc,
			from.Ticker, from.ChainName, to.Ticker, to.ChainName, ""),
		buildSwapArticle("pair-1",
			fmt.Sprintf("Swap %s → %s", toLabel, fromLabel), descRev,
			to.Ticker, to.ChainName, from.Ticker, from.ChainName, ""),
	}
}

// buildPairAmountResults returns results for a two-token + amount query (e.g. "BTC ETH 0.5").
func buildPairAmountResults(fromQuery, toQuery, amount string) []interface{} {
	from := findToken(fromQuery, "")
	to := findToken(toQuery, "")
	if from == nil || to == nil {
		return buildPairResults(fromQuery, toQuery)
	}

	if _, err := strconv.ParseFloat(amount, 64); err != nil {
		return buildPairResults(fromQuery, toQuery)
	}

	fromLabel := tokenLabel(from.Ticker, from.ChainName)
	toLabel := tokenLabel(to.Ticker, to.ChainName)
	title := fmt.Sprintf("Swap %s %s → %s", amount, fromLabel, toLabel)

	outAmt, outUSD := estimateOutput(from.Ticker, to.Ticker, amount)
	desc := "Tap to get exact quote · Zero fees"
	if outAmt != "" {
		desc = fmt.Sprintf("≈ %s %s (%s) · Zero fees", outAmt, toLabel, outUSD)
	}

	return []interface{}{
		buildSwapArticle("amount-0", title, desc,
			from.Ticker, from.ChainName, to.Ticker, to.ChainName, amount),
	}
}

// buildStatusResults returns an inline result showing the current order status.
func buildStatusResults(token string) []interface{} {
	order, err := decryptOrderData(token)
	if err != nil {
		return nil
	}

	status, err := fetchStatus(order.DepositAddr, order.Memo)
	if err != nil {
		return nil
	}

	displayStatus := statusDisplayName(status.Status)
	title := fmt.Sprintf("Order: %s → %s — %s", order.FromTicker, order.ToTicker, displayStatus)
	desc := fmt.Sprintf("%s %s → %s %s", order.AmountIn, order.FromTicker, order.AmountOut, order.ToTicker)
	msgText := fmt.Sprintf(
		"<b>Ø uSwap Zero</b> — Order Status\n<b>%s → %s</b>\nAmount: %s %s → %s %s\nStatus: <b>%s</b>",
		order.FromTicker, order.ToTicker,
		order.AmountIn, order.FromTicker,
		order.AmountOut, order.ToTicker,
		displayStatus)

	orderURL := tgAppURL + "/order/" + token

	return []interface{}{
		TGInlineQueryResultArticle{
			Type:        "article",
			ID:          "status-0",
			Title:       title,
			Description: desc,
			InputMessageContent: TGInputTextMessageContent{
				MessageText:        msgText,
				ParseMode:          "HTML",
				LinkPreviewOptions: map[string]interface{}{"is_disabled": true},
			},
			ReplyMarkup: &TGInlineKeyboardMarkup{
				InlineKeyboard: [][]TGInlineKeyboardButton{
					{{Text: "View Order →", URL: orderURL}},
				},
			},
		},
	}
}

// buildSwapArticle constructs an article inline result for a swap pair.
func buildSwapArticle(id, title, desc, fromTicker, fromNet, toTicker, toNet, amount string) interface{} {
	deepLink := buildDeepLink(fromTicker, fromNet, toTicker, toNet, amount)
	fromLabel := tokenLabel(fromTicker, fromNet)
	toLabel := tokenLabel(toTicker, toNet)

	var msgText string
	if amount != "" {
		msgText = fmt.Sprintf(
			"<b>Ø uSwap Zero</b> — Swap <b>%s %s → %s</b>\nZero fees · Non-custodial\n\nTap below to open the swap →",
			amount, fromLabel, toLabel)
	} else {
		msgText = fmt.Sprintf(
			"<b>Ø uSwap Zero</b> — Swap <b>%s → %s</b>\nZero fees · Non-custodial\n\nTap below to open the swap →",
			fromLabel, toLabel)
	}

	return TGInlineQueryResultArticle{
		Type:        "article",
		ID:          id,
		Title:       title,
		Description: desc,
		InputMessageContent: TGInputTextMessageContent{
			MessageText:        msgText,
			ParseMode:          "HTML",
			LinkPreviewOptions: map[string]interface{}{"is_disabled": true},
		},
		ReplyMarkup: &TGInlineKeyboardMarkup{
			InlineKeyboard: [][]TGInlineKeyboardButton{
				{{Text: "Open in uSwap →", URL: deepLink}},
			},
		},
	}
}

// buildStartNewSwapArticle constructs the generic "Start New Swap" inline result.
func buildStartNewSwapArticle() interface{} {
	link := tgAppURL
	if tgBotUsername != "" {
		link = "https://t.me/" + tgBotUsername
	}
	return TGInlineQueryResultArticle{
		Type:        "article",
		ID:          "start-new-0",
		Title:       "Start New Swap",
		Description: "Zero fees · Non-custodial · NEAR Intents",
		InputMessageContent: TGInputTextMessageContent{
			MessageText:        "<b>Ø uSwap Zero</b> — Zero Fee Cross-Chain Swaps\nPowered by NEAR Intents\n\nTap below to start a new swap →",
			ParseMode:          "HTML",
			LinkPreviewOptions: map[string]interface{}{"is_disabled": true},
		},
		ReplyMarkup: &TGInlineKeyboardMarkup{
			InlineKeyboard: [][]TGInlineKeyboardButton{
				{{Text: "Open uSwap →", URL: link}},
			},
		},
	}
}

// buildDeepLink constructs a Telegram deep link for pre-filling the swap form.
// Format: https://t.me/<username>?start=swap_BTC-btc_ETH-eth[_amount]
func buildDeepLink(fromTicker, fromNet, toTicker, toNet, amount string) string {
	if tgBotUsername == "" {
		u := tgAppURL + "?from=" + strings.ToUpper(fromTicker) + "&to=" + strings.ToUpper(toTicker)
		if amount != "" {
			u += "&amt=" + amount
		}
		return u
	}
	param := "swap_" +
		strings.ToUpper(fromTicker) + "-" + strings.ToLower(fromNet) +
		"_" + strings.ToUpper(toTicker) + "-" + strings.ToLower(toNet)
	if amount != "" {
		param += "_" + amount
	}
	return "https://t.me/" + tgBotUsername + "?start=" + param
}

// parseSwapStartParam pre-fills a session from a deep link start parameter.
// param format after "swap_": "BTC-btc_ETH-eth" or "BTC-btc_ETH-eth_0.5"
func parseSwapStartParam(sess *tgSession, param string) {
	parts := strings.SplitN(param, "_", 3)
	if len(parts) < 2 {
		return
	}
	fromParts := strings.SplitN(parts[0], "-", 2)
	toParts := strings.SplitN(parts[1], "-", 2)
	if len(fromParts) < 2 || len(toParts) < 2 {
		return
	}
	fromTicker := strings.ToUpper(fromParts[0])
	fromNet := strings.ToLower(fromParts[1])
	toTicker := strings.ToUpper(toParts[0])
	toNet := strings.ToLower(toParts[1])

	if from := findToken(fromTicker, fromNet); from != nil {
		sess.FromTicker = from.Ticker
		sess.FromNet = from.ChainName
	}
	if to := findToken(toTicker, toNet); to != nil {
		sess.ToTicker = to.Ticker
		sess.ToNet = to.ChainName
	}
	if len(parts) == 3 && parts[2] != "" {
		sess.Amount = parts[2]
	}
}

// estimateOutput estimates swap output using cached token prices.
// Returns ("", "") when prices are not available.
func estimateOutput(fromTicker, toTicker, amountStr string) (outAmt, outUSD string) {
	from := findToken(fromTicker, "")
	to := findToken(toTicker, "")
	if from == nil || to == nil || from.Price == 0 || to.Price == 0 {
		return "", ""
	}
	amountF, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amountF <= 0 {
		return "", ""
	}
	valueUSD := amountF * from.Price
	output := valueUSD / to.Price
	return fmtEstimate(output), fmt.Sprintf("~$%.2f", valueUSD)
}

// fmtEstimate formats a float as a concise decimal string, trimming trailing zeros.
func fmtEstimate(f float64) string {
	if f == 0 {
		return "0"
	}
	s := strconv.FormatFloat(f, 'f', 8, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

// statusDisplayName maps an API status code to a human-readable label.
func statusDisplayName(s string) string {
	switch strings.ToUpper(s) {
	case "PENDING_DEPOSIT":
		return "Awaiting Deposit"
	case "KNOWN_DEPOSIT_TX":
		return "Deposit Detected"
	case "PROCESSING":
		return "Processing"
	case "SUCCESS":
		return "Completed"
	case "REFUNDED":
		return "Refunded"
	case "FAILED":
		return "Failed"
	case "INCOMPLETE_DEPOSIT":
		return "Incomplete Deposit"
	default:
		return s
	}
}
