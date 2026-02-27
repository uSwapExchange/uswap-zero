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
		// Support both "AMOUNT FROM TO" (natural) and "FROM TO AMOUNT" (legacy).
		if _, err := strconv.ParseFloat(parts[0], 64); err == nil {
			return parsedInlineQuery{kind: inlineKindPairAmt, from: parts[1], to: parts[2], amount: parts[0]}
		}
		if _, err := strconv.ParseFloat(parts[2], 64); err == nil {
			return parsedInlineQuery{kind: inlineKindPairAmt, from: parts[0], to: parts[1], amount: parts[2]}
		}
		return parsedInlineQuery{kind: inlineKindPair, from: parts[0], to: parts[1]}
	}
}

// findAllTokenNetworks returns all tokens with an exact ticker match, one per chain,
// in cache order (which reflects API ordering by liquidity/popularity).
func findAllTokenNetworks(ticker string) []TokenInfo {
	matches := searchTokens(ticker)
	upper := strings.ToUpper(ticker)
	seen := make(map[string]bool)
	var result []TokenInfo
	for _, t := range matches {
		if strings.ToUpper(t.Ticker) != upper {
			continue
		}
		key := strings.ToLower(t.ChainName)
		if !seen[key] {
			seen[key] = true
			result = append(result, t)
		}
	}
	return result
}

// estimateOutputForTokens estimates swap output from pre-resolved token instances.
// Returns ("", "") when prices are unavailable.
func estimateOutputForTokens(from, to TokenInfo, amountStr string) (outAmt, outUSD string) {
	if from.Price == 0 || to.Price == 0 {
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

// buildEmptyResults returns results for an empty inline query (@botname with no text).
// Uses hardcoded canonical pairs; tokenLabel naturally shows "(ETH)", "(SOL)" etc.
// for multi-chain tokens so network context is visible in the title.
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
		desc := networkDisplayName(from.ChainName) + " → " + networkDisplayName(to.ChainName) + " · Zero fees"
		if from.Price > 0 && to.Price > 0 {
			desc = fmt.Sprintf("1 %s ≈ %s %s · %s → %s",
				fromLabel, fmtEstimate(from.Price/to.Price), toLabel,
				networkDisplayName(from.ChainName), networkDisplayName(to.ChainName))
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

// buildSingleTokenResults returns results for a single-word query.
// The FROM token is the first/canonical match per unique ticker.
// The TO side shows ALL network variants of each popular target so users
// can see "Swap BTC → USDT (ETH)", "Swap BTC → USDT (SOL)", etc.
func buildSingleTokenResults(query string) []interface{} {
	matches := searchTokens(query)

	// Deduplicate FROM candidates by ticker; keep up to 3 unique tickers.
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

	popularTargetTickers := []string{"ETH", "USDT", "BTC", "USDC", "SOL"}

	var results []interface{}
	for i, from := range froms {
		for _, targetTicker := range popularTargetTickers {
			if strings.EqualFold(from.Ticker, targetTicker) {
				continue
			}
			// All network variants of this target ticker.
			toVariants := findAllTokenNetworks(targetTicker)
			for k, to := range toVariants {
				fromLabel := tokenLabel(from.Ticker, from.ChainName)
				toLabel := tokenLabel(to.Ticker, to.ChainName)
				title := fmt.Sprintf("Swap %s → %s", fromLabel, toLabel)
				desc := networkDisplayName(from.ChainName) + " → " + networkDisplayName(to.ChainName) + " · Zero fees"
				if from.Price > 0 && to.Price > 0 {
					desc = fmt.Sprintf("1 %s ≈ %s %s · %s → %s",
						fromLabel, fmtEstimate(from.Price/to.Price), toLabel,
						networkDisplayName(from.ChainName), networkDisplayName(to.ChainName))
				}
				results = append(results, buildSwapArticle(
					fmt.Sprintf("single-%d-%s-%d", i, strings.ToLower(targetTicker), k),
					title, desc,
					from.Ticker, from.ChainName,
					to.Ticker, to.ChainName, "",
				))
				if len(results) >= 10 {
					return results
				}
			}
		}
	}
	return results
}

// buildPairResults returns results for a two-token query (e.g. "BTC ETH").
//
// Forward direction: canonical FROM → every TO network variant.
// Reverse direction: canonical TO → every FROM network variant.
//
// Example for "BTC USDT":
//   Swap BTC → USDT (ETH), Swap BTC → USDT (SOL), Swap BTC → USDT (TRON) ...
//   Swap USDT (ETH) → BTC
func buildPairResults(fromQuery, toQuery string) []interface{} {
	fromVariants := findAllTokenNetworks(fromQuery)
	toVariants := findAllTokenNetworks(toQuery)
	if len(fromVariants) == 0 || len(toVariants) == 0 {
		return buildSingleTokenResults(fromQuery)
	}

	from := fromVariants[0] // canonical FROM
	to := toVariants[0]     // canonical TO (used as FROM in reverse)

	var results []interface{}

	// Forward: canonical FROM → all TO network variants
	for i, toVar := range toVariants {
		if len(results) >= 8 {
			break
		}
		fromLabel := tokenLabel(from.Ticker, from.ChainName)
		toLabel := tokenLabel(toVar.Ticker, toVar.ChainName)
		title := fmt.Sprintf("Swap %s → %s", fromLabel, toLabel)
		desc := networkDisplayName(from.ChainName) + " → " + networkDisplayName(toVar.ChainName) + " · Zero fees"
		if from.Price > 0 && toVar.Price > 0 {
			desc = fmt.Sprintf("1 %s ≈ %s %s · %s → %s",
				fromLabel, fmtEstimate(from.Price/toVar.Price), toLabel,
				networkDisplayName(from.ChainName), networkDisplayName(toVar.ChainName))
		}
		results = append(results, buildSwapArticle(
			fmt.Sprintf("pair-fwd-%d", i),
			title, desc,
			from.Ticker, from.ChainName,
			toVar.Ticker, toVar.ChainName, "",
		))
	}

	// Reverse: canonical TO → all FROM network variants
	for i, fromVar := range fromVariants {
		if len(results) >= 12 {
			break
		}
		toLabel := tokenLabel(to.Ticker, to.ChainName)
		fromLabel := tokenLabel(fromVar.Ticker, fromVar.ChainName)
		title := fmt.Sprintf("Swap %s → %s", toLabel, fromLabel)
		desc := networkDisplayName(to.ChainName) + " → " + networkDisplayName(fromVar.ChainName) + " · Zero fees"
		if to.Price > 0 && fromVar.Price > 0 {
			desc = fmt.Sprintf("1 %s ≈ %s %s · %s → %s",
				toLabel, fmtEstimate(to.Price/fromVar.Price), fromLabel,
				networkDisplayName(to.ChainName), networkDisplayName(fromVar.ChainName))
		}
		results = append(results, buildSwapArticle(
			fmt.Sprintf("pair-rev-%d", i),
			title, desc,
			to.Ticker, to.ChainName,
			fromVar.Ticker, fromVar.ChainName, "",
		))
	}

	return results
}

// buildPairAmountResults returns results for a two-token + amount query (e.g. "BTC ETH 0.5").
// Shows all TO network variants, each with an estimated output.
func buildPairAmountResults(fromQuery, toQuery, amount string) []interface{} {
	fromVariants := findAllTokenNetworks(fromQuery)
	toVariants := findAllTokenNetworks(toQuery)
	if len(fromVariants) == 0 || len(toVariants) == 0 {
		return buildPairResults(fromQuery, toQuery)
	}

	if _, err := strconv.ParseFloat(amount, 64); err != nil {
		return buildPairResults(fromQuery, toQuery)
	}

	from := fromVariants[0] // canonical FROM

	var results []interface{}
	for i, to := range toVariants {
		if len(results) >= 8 {
			break
		}
		fromLabel := tokenLabel(from.Ticker, from.ChainName)
		toLabel := tokenLabel(to.Ticker, to.ChainName)
		title := fmt.Sprintf("Swap %s %s → %s", amount, fromLabel, toLabel)

		outAmt, outUSD := estimateOutputForTokens(from, to, amount)
		desc := networkDisplayName(from.ChainName) + " → " + networkDisplayName(to.ChainName) + " · Tap to quote"
		if outAmt != "" {
			desc = fmt.Sprintf("≈ %s %s (%s) · %s → %s",
				outAmt, toLabel, outUSD,
				networkDisplayName(from.ChainName), networkDisplayName(to.ChainName))
		}
		results = append(results, buildSwapArticle(
			fmt.Sprintf("amount-%d", i),
			title, desc,
			from.Ticker, from.ChainName,
			to.Ticker, to.ChainName, amount,
		))
	}
	return results
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
