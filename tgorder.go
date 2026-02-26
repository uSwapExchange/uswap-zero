package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// handleTGGetQuote fetches a dry quote and shows a monospace quote card.
func handleTGGetQuote(chatID int64, sess *tgSession) {
	if !sess.isComplete() {
		return
	}

	fromToken := findToken(sess.FromTicker, sess.FromNet)
	toToken := findToken(sess.ToTicker, sess.ToNet)
	if fromToken == nil || toToken == nil {
		showErrorAndCard(chatID, sess, "Token not found. Please reselect.")
		return
	}

	// Show loading state
	tgEditMessage(chatID, sess.CardMsgID, "⏳ Fetching quote...\n<i>(may take up to 24s)</i>", nil)

	atomic, err := humanToAtomic(sess.Amount, fromToken.Decimals)
	if err != nil {
		showErrorAndCard(chatID, sess, "Invalid amount: "+err.Error())
		return
	}

	bps, _ := slippageToBPS(sess.Slippage)

	req := &QuoteRequest{
		Dry:                true,
		SwapType:           "EXACT_INPUT",
		SlippageTolerance:  bps,
		OriginAsset:        fromToken.DefuseAssetID,
		DepositType:        "ORIGIN_CHAIN",
		DestinationAsset:   toToken.DefuseAssetID,
		Amount:             atomic,
		RefundTo:           sess.RefundAddr,
		RefundType:         "ORIGIN_CHAIN",
		Recipient:          sess.RecvAddr,
		RecipientType:      "DESTINATION_CHAIN",
		Deadline:           buildDeadline(1 * time.Hour),
		Referral:           "uswap-zero",
		QuoteWaitingTimeMs: 24000,
		AppFees:            []struct{}{},
	}

	dryResp, err := requestDryQuote(req)
	if err != nil {
		showErrorAndCard(chatID, sess, "Quote failed: "+err.Error())
		return
	}

	sess.DryQuote = dryResp
	sess.State = stateQuoteConfirm

	// Parse display values
	amountInUSD := ""
	amountOutUSD := ""
	spreadUSD := ""
	spreadPct := ""
	rate := ""

	if dryResp.Quote.AmountInUSD != "" {
		if v, err := strconv.ParseFloat(dryResp.Quote.AmountInUSD, 64); err == nil {
			amountInUSD = formatUSD(v)
		}
	}
	if dryResp.Quote.AmountOutUSD != "" {
		if v, err := strconv.ParseFloat(dryResp.Quote.AmountOutUSD, 64); err == nil {
			amountOutUSD = formatUSD(v)
		}
	}

	if dryResp.Quote.AmountInUSD != "" && dryResp.Quote.AmountOutUSD != "" {
		inUSD, _ := strconv.ParseFloat(dryResp.Quote.AmountInUSD, 64)
		outUSD, _ := strconv.ParseFloat(dryResp.Quote.AmountOutUSD, 64)
		if inUSD > 0 {
			diff := inUSD - outUSD
			pct := (diff / inUSD) * 100
			spreadUSD = fmt.Sprintf("%.2f", diff)
			spreadPct = fmt.Sprintf("%.2f", pct)
		}
	}

	if dryResp.Quote.AmountInFormatted != "" && dryResp.Quote.AmountOutFormatted != "" {
		inVal, _ := strconv.ParseFloat(dryResp.Quote.AmountInFormatted, 64)
		outVal, _ := strconv.ParseFloat(dryResp.Quote.AmountOutFormatted, 64)
		if inVal > 0 {
			r := outVal / inVal
			rate = fmt.Sprintf("1 %s = %s %s", sess.FromTicker, formatRate(r), sess.ToTicker)
		}
	}

	cardText := "<pre>" + renderQuoteCardMono(QuoteCardData{
		FromTicker:   sess.FromTicker,
		ToTicker:     sess.ToTicker,
		AmountIn:     dryResp.Quote.AmountInFormatted,
		AmountOut:    dryResp.Quote.AmountOutFormatted,
		AmountInUSD:  amountInUSD,
		AmountOutUSD: amountOutUSD,
		Rate:         rate,
		SpreadUSD:    spreadUSD,
		SpreadPct:    spreadPct,
	}) + "</pre>"

	markup := &TGInlineKeyboardMarkup{
		InlineKeyboard: [][]TGInlineKeyboardButton{
			{
				{Text: "✅ Confirm Swap", CallbackData: "cs", Style: "success"},
				{Text: "❌ Cancel", CallbackData: "cq", Style: "danger"},
			},
		},
	}

	if err := tgEditMessage(chatID, sess.CardMsgID, cardText, markup); err != nil {
		log.Printf("tg edit quote card error: %v", err)
	}
}

// handleTGConfirmSwap places a real quote and shows the unified deposit/order card.
func handleTGConfirmSwap(chatID int64, sess *tgSession) {
	if sess.State != stateQuoteConfirm {
		return
	}

	fromToken := findToken(sess.FromTicker, sess.FromNet)
	toToken := findToken(sess.ToTicker, sess.ToNet)
	if fromToken == nil || toToken == nil {
		return
	}

	atomic, err := humanToAtomic(sess.Amount, fromToken.Decimals)
	if err != nil {
		return
	}

	// Show loading state
	tgEditMessage(chatID, sess.CardMsgID, "⏳ Placing order...\n<i>(may take up to 24s)</i>", nil)

	bps, _ := slippageToBPS(sess.Slippage)

	req := &QuoteRequest{
		Dry:                false,
		SwapType:           "EXACT_INPUT",
		SlippageTolerance:  bps,
		OriginAsset:        fromToken.DefuseAssetID,
		DepositType:        "ORIGIN_CHAIN",
		DestinationAsset:   toToken.DefuseAssetID,
		Amount:             atomic,
		RefundTo:           sess.RefundAddr,
		RefundType:         "ORIGIN_CHAIN",
		Recipient:          sess.RecvAddr,
		RecipientType:      "DESTINATION_CHAIN",
		Deadline:           buildDeadline(1 * time.Hour),
		Referral:           "uswap-zero",
		QuoteWaitingTimeMs: 24000,
		AppFees:            []struct{}{},
	}

	quoteResp, err := requestQuote(req)
	if err != nil {
		showErrorAndCard(chatID, sess, "Order failed: "+err.Error())
		return
	}

	order := &OrderData{
		DepositAddr: quoteResp.Quote.DepositAddress,
		Memo:        quoteResp.Quote.DepositMemo,
		FromTicker:  sess.FromTicker,
		FromNet:     sess.FromNet,
		ToTicker:    sess.ToTicker,
		ToNet:       sess.ToNet,
		AmountIn:    quoteResp.Quote.AmountInFmt,
		AmountOut:   quoteResp.Quote.AmountOutFmt,
		Deadline:    quoteResp.Quote.Deadline, // use API's canonical deadline
		CorrID:      quoteResp.CorrelationID,
		RefundAddr:  sess.RefundAddr,
		RecvAddr:    sess.RecvAddr,
	}

	orderToken, err := encryptOrderData(order)
	if err != nil {
		log.Printf("tg encrypt order error: %v", err)
		return
	}
	sess.OrderToken = orderToken
	sess.State = stateOrderActive

	netName := networkDisplayName(sess.FromNet)
	timeLeft := deadlineString(quoteResp.Quote.Deadline)

	// Build unified deposit/order card (step 0 of stepper)
	depositCard := "<pre>" + renderDepositCardMono(DepositCardData{
		FromTicker: sess.FromTicker,
		ToTicker:   sess.ToTicker,
		AmountIn:   order.AmountIn,
		AmountOut:  order.AmountOut,
		Network:    netName,
		Deadline:   timeLeft,
		RefundAddr: sess.RefundAddr,
		RecvAddr:   sess.RecvAddr,
	}) + "</pre>"

	// Copyable amount above address
	depositCard += "\n\n<code>" + order.AmountIn + " " + sess.FromTicker + "</code>"
	depositCard += "\n\n<code>" + quoteResp.Quote.DepositAddress + "</code>"
	if quoteResp.Quote.DepositMemo != "" {
		depositCard += "\n\nMemo: <code>" + quoteResp.Quote.DepositMemo + "</code>"
	}

	orderURL := tgAppURL + "/order/" + orderToken
	markup := &TGInlineKeyboardMarkup{
		InlineKeyboard: [][]TGInlineKeyboardButton{
			{
				{Text: "🔄 Refresh Status", CallbackData: "rs"},
				{Text: "📱 Open Order", WebApp: &TGWebApp{URL: orderURL}},
			},
		},
	}

	if err := tgEditMessage(chatID, sess.CardMsgID, depositCard, markup); err != nil {
		log.Printf("tg edit deposit card error: %v", err)
	}
}

// handleTGCancelQuote returns to the swap card by editing CardMsgID in place.
func handleTGCancelQuote(chatID int64, sess *tgSession) {
	sess.State = stateSwapCard
	sess.DryQuote = nil

	text, markup := renderSwapCard(sess)
	if sess.CardMsgID != 0 {
		if err := tgEditMessage(chatID, sess.CardMsgID, text, markup); err != nil {
			log.Printf("tg cancel quote edit error: %v", err)
		}
	}
}

// buildOrderCard builds the unified order card text and markup for any order state.
func buildOrderCard(order *OrderData, status *StatusResponse, orderToken string) (string, *TGInlineKeyboardMarkup) {
	isTerminal := isTerminalStatus(status.Status)

	var cardText string
	statusUpper := strings.ToUpper(status.Status)
	if statusUpper == "PENDING_DEPOSIT" || statusUpper == "KNOWN_DEPOSIT_TX" {
		netName := networkDisplayName(order.FromNet)
		timeLeft := deadlineString(order.Deadline)
		cardText = "<pre>" + renderDepositCardMono(DepositCardData{
			FromTicker: order.FromTicker,
			ToTicker:   order.ToTicker,
			AmountIn:   order.AmountIn,
			AmountOut:  order.AmountOut,
			Network:    netName,
			Deadline:   timeLeft,
			RefundAddr: order.RefundAddr,
			RecvAddr:   order.RecvAddr,
		}) + "</pre>"
		cardText += "\n\n<code>" + order.AmountIn + " " + order.FromTicker + "</code>"
		cardText += "\n\n<code>" + order.DepositAddr + "</code>"
		if order.Memo != "" {
			cardText += "\n\nMemo: <code>" + order.Memo + "</code>"
		}
	} else {
		cardText = "<pre>" + renderAnyStatusCard(order, status) + "</pre>"
	}

	var rows [][]TGInlineKeyboardButton

	if !isTerminal {
		rows = append(rows, []TGInlineKeyboardButton{
			{Text: "🔄 Refresh Status", CallbackData: "rs"},
		})
	}

	if status.SwapDetails != nil {
		for _, tx := range status.SwapDetails.DestTxs {
			if tx.ExplorerURL != "" {
				rows = append(rows, []TGInlineKeyboardButton{
					{Text: "🔗 View TX", WebApp: &TGWebApp{URL: tx.ExplorerURL}},
				})
				break
			}
		}
	}

	if isTerminal {
		rows = append(rows, []TGInlineKeyboardButton{
			{Text: "🗑 Clear", CallbackData: "dm", Style: "danger"},
			{Text: "🆕 New Swap", CallbackData: "ns", Style: "success"},
		})
	}

	orderURL := tgAppURL + "/order/" + orderToken
	rows = append(rows, []TGInlineKeyboardButton{
		{Text: "📱 Open Order", WebApp: &TGWebApp{URL: orderURL}},
	})

	return cardText, &TGInlineKeyboardMarkup{InlineKeyboard: rows}
}

// handleTGRefreshStatus fetches and updates the order card in place.
func handleTGRefreshStatus(chatID int64, sess *tgSession) {
	if sess.OrderToken == "" {
		return
	}

	order, err := decryptOrderData(sess.OrderToken)
	if err != nil {
		return
	}

	status, err := fetchStatus(order.DepositAddr, order.Memo)
	if err != nil {
		tgEditMessage(chatID, sess.CardMsgID, "❌ Status check failed: "+err.Error(), nil)
		return
	}

	cardText, markup := buildOrderCard(order, status, sess.OrderToken)

	if err := tgEditMessage(chatID, sess.CardMsgID, cardText, markup); err != nil {
		log.Printf("tg refresh status edit error: %v", err)
	}
}

// isTerminalStatus returns true when the status indicates a finished swap.
// API status values: PENDING_DEPOSIT, KNOWN_DEPOSIT_TX, PROCESSING,
// INCOMPLETE_DEPOSIT, SUCCESS, REFUNDED, FAILED
func isTerminalStatus(s string) bool {
	switch strings.ToUpper(s) {
	case "SUCCESS", "REFUNDED", "FAILED", "INCOMPLETE_DEPOSIT":
		return true
	}
	return false
}

// showErrorAndCard edits CardMsgID to show an error notice above the restored swap card.
// All session inputs are preserved; no button tap required to recover.
func showErrorAndCard(chatID int64, sess *tgSession, errMsg string) {
	sess.State = stateSwapCard
	sess.DryQuote = nil
	cardText, markup := renderSwapCard(sess)
	text := "❌ " + errMsg + "\n\n" + cardText
	if err := tgEditMessage(chatID, sess.CardMsgID, text, markup); err != nil {
		log.Printf("tg show error+card: %v", err)
	}
}

// handleTGDeleteMessages deletes all tracked messages for the current swap.
func handleTGDeleteMessages(chatID int64, sess *tgSession) {
	for _, msgID := range sess.OrderMsgIDs {
		tgDeleteMessage(chatID, msgID)
	}
	if sess.DepositMsgID != 0 {
		tgDeleteMessage(chatID, sess.DepositMsgID)
	}
	sess.reset()
}
