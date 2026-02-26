package main

import (
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
)

// Popular tokens for the token picker grid.
var tgPopularTokens = []string{
	"BTC", "ETH", "USDT",
	"USDC", "SOL", "BNB",
	"XRP", "DOGE", "AVAX",
	"TON", "TRX", "NEAR",
}

// tokenLabel returns "BTC" or "USDT (ETH)" — shows chain only when relevant.
func tokenLabel(ticker, net string) string {
	if ticker == "" {
		return "—"
	}
	// Show chain when ticker != chain (e.g. USDT on eth, USDC on sol)
	if !strings.EqualFold(ticker, net) && net != "" {
		return ticker + " (" + strings.ToUpper(net) + ")"
	}
	return ticker
}

// buildAppURL builds the webapp URL with session params pre-filled.
func buildAppURL(sess *tgSession) string {
	params := url.Values{}
	if sess.FromTicker != "" {
		params.Set("from", sess.FromTicker)
	}
	if sess.FromNet != "" {
		params.Set("from_net", sess.FromNet)
	}
	if sess.ToTicker != "" {
		params.Set("to", sess.ToTicker)
	}
	if sess.ToNet != "" {
		params.Set("to_net", sess.ToNet)
	}
	if sess.Amount != "" {
		params.Set("amt", sess.Amount)
	}
	if sess.RecvAddr != "" {
		params.Set("recipient", sess.RecvAddr)
	}
	if sess.Slippage != "" && sess.Slippage != "1" {
		params.Set("slippage", sess.Slippage)
	}
	q := params.Encode()
	if q != "" {
		return tgAppURL + "/?" + q
	}
	return tgAppURL + "/"
}

// renderSwapCard builds the swap card text and inline keyboard.
func renderSwapCard(sess *tgSession) (string, *TGInlineKeyboardMarkup) {
	var sb strings.Builder

	// Live counter line — only shown when monitor is running
	if total := monitorTotalFeeUSD(); total > 0 {
		sb.WriteString("Don't be a part of the " + formatUSD(total) + " lost to @APIWrappers\n\n")
	}

	sb.WriteString("<pre>" + renderSwapCardMono(sess) + "</pre>")

	// Footer links
	sb.WriteString("\n\n")
	if commitHash != "development" && commitHash != "unknown" && commitHash != "" {
		sb.WriteString("<a href=\"https://github.com/uSwapExchange/zero/commit/" + commitHash + "\">[Commit]</a> · ")
	}
	sb.WriteString("<a href=\"https://github.com/uSwapExchange/zero\">[Source Code]</a>")
	if buildLogURL != "" && buildLogURL != "unknown" {
		sb.WriteString(" · <a href=\"" + buildLogURL + "\">[Build Log]</a>")
	}

	text := sb.String()

	fromLabel := tokenLabel(sess.FromTicker, sess.FromNet)
	toLabel := tokenLabel(sess.ToTicker, sess.ToNet)

	var rows [][]TGInlineKeyboardButton

	// Row 1: Token pickers + flip button
	rows = append(rows, []TGInlineKeyboardButton{
		{Text: "[Send] " + fromLabel, CallbackData: "pf", Style: "danger"},
		{Text: "🔁", CallbackData: "sw"},
		{Text: "[Recv] " + toLabel, CallbackData: "pt", Style: "success"},
	})

	// Row 2: Inline slippage — always visible, selected marked with ●
	slippageOpts := []string{"0.5", "1", "2", "3"}
	var slipRow []TGInlineKeyboardButton
	for _, s := range slippageOpts {
		label := s + "%"
		style := ""
		if s == sess.Slippage {
			label = "● " + label
			style = "primary"
		}
		slipRow = append(slipRow, TGInlineKeyboardButton{
			Text:         label,
			CallbackData: "sl:" + s,
			Style:        style,
		})
	}
	rows = append(rows, slipRow)

	// Row 3: Set Amount
	amountBtn := TGInlineKeyboardButton{CallbackData: "sa"}
	if sess.Amount != "" {
		amountBtn.Text = "✓ Amount: " + sess.Amount + " " + fromLabel
		amountBtn.Style = "primary"
	} else {
		amountBtn.Text = "Set Amount"
	}
	rows = append(rows, []TGInlineKeyboardButton{amountBtn})

	// Row 4: Set Refund Address
	refundBtn := TGInlineKeyboardButton{CallbackData: "sr"}
	if sess.RefundAddr != "" {
		refundBtn.Text = "✓ Refund: " + truncAddr(sess.RefundAddr)
		refundBtn.Style = "primary"
	} else {
		refundBtn.Text = "Set Refund Address"
	}
	rows = append(rows, []TGInlineKeyboardButton{refundBtn})

	// Row 5: Set Receive Address
	recvBtn := TGInlineKeyboardButton{CallbackData: "sp"}
	if sess.RecvAddr != "" {
		recvBtn.Text = "✓ Receive: " + truncAddr(sess.RecvAddr)
		recvBtn.Style = "primary"
	} else {
		recvBtn.Text = "Set Receive Address"
	}
	rows = append(rows, []TGInlineKeyboardButton{recvBtn})

	// Row 6: Get Quote (only when all fields filled)
	if sess.isComplete() {
		rows = append(rows, []TGInlineKeyboardButton{
			{Text: "✅ Get Quote →", CallbackData: "gq", Style: "success"},
		})
	}

	// Row 7: Open in web app with session params pre-filled
	rows = append(rows, []TGInlineKeyboardButton{
		{Text: "📱 Open Quote in App", WebApp: &TGWebApp{URL: buildAppURL(sess)}},
	})

	return text, &TGInlineKeyboardMarkup{InlineKeyboard: rows}
}

// updateSwapCard edits the existing card message.
func updateSwapCard(chatID int64, sess *tgSession) {
	if sess.CardMsgID == 0 {
		return
	}
	text, markup := renderSwapCard(sess)
	if err := tgEditMessage(chatID, sess.CardMsgID, text, markup); err != nil {
		log.Printf("tg edit card error: %v", err)
	}
}

// --- Token Picker ---

// handleTGPickToken shows the token picker grid.
func handleTGPickToken(chatID int64, sess *tgSession, side string) {
	sess.PickSide = side
	sess.PickPage = 0
	sess.State = statePickToken

	text, markup := renderTokenPicker(sess, 0)
	if sess.CardMsgID != 0 {
		tgEditMessage(chatID, sess.CardMsgID, text, markup)
	}
}

// renderTokenPicker builds the token picker grid.
func renderTokenPicker(sess *tgSession, page int) (string, *TGInlineKeyboardMarkup) {
	side := "Send"
	if sess.PickSide == "to" {
		side = "Receive"
	}
	text := fmt.Sprintf("<b>Select %s Token</b>\n\nTap a token or type to search.", side)

	var rows [][]TGInlineKeyboardButton

	// 4 rows of 3 tokens
	for i := 0; i < len(tgPopularTokens); i += 3 {
		var row []TGInlineKeyboardButton
		for j := i; j < i+3 && j < len(tgPopularTokens); j++ {
			ticker := tgPopularTokens[j]
			row = append(row, TGInlineKeyboardButton{
				Text:         ticker,
				CallbackData: "ts:" + ticker,
			})
		}
		rows = append(rows, row)
	}

	// Back row
	rows = append(rows, []TGInlineKeyboardButton{
		{Text: "← Back", CallbackData: "bk"},
	})

	return text, &TGInlineKeyboardMarkup{InlineKeyboard: rows}
}

// handleTGTokenSearch handles text input during token picker state.
func handleTGTokenSearch(chatID int64, sess *tgSession, query string) {
	results := searchTokens(query)
	if len(results) == 0 {
		text := "<b>No tokens found for:</b> " + query + "\n\nTry a different ticker or name."
		markup := &TGInlineKeyboardMarkup{
			InlineKeyboard: [][]TGInlineKeyboardButton{
				{{Text: "← Back", CallbackData: "bk"}},
			},
		}
		if sess.CardMsgID != 0 {
			tgEditMessage(chatID, sess.CardMsgID, text, markup)
		}
		return
	}

	// Deduplicate by ticker
	seen := make(map[string]bool)
	var unique []TokenInfo
	for _, t := range results {
		if !seen[t.Ticker] {
			seen[t.Ticker] = true
			unique = append(unique, t)
		}
		if len(unique) >= 12 {
			break
		}
	}

	text := fmt.Sprintf("<b>Results for:</b> %s", query)
	var rows [][]TGInlineKeyboardButton
	for i := 0; i < len(unique); i += 3 {
		var row []TGInlineKeyboardButton
		for j := i; j < i+3 && j < len(unique); j++ {
			row = append(row, TGInlineKeyboardButton{
				Text:         unique[j].Ticker,
				CallbackData: "ts:" + unique[j].Ticker,
			})
		}
		rows = append(rows, row)
	}
	rows = append(rows, []TGInlineKeyboardButton{
		{Text: "← Back", CallbackData: "bk"},
	})

	if sess.CardMsgID != 0 {
		tgEditMessage(chatID, sess.CardMsgID, text, &TGInlineKeyboardMarkup{InlineKeyboard: rows})
	}
}

// handleTGTokenSelected handles selection of a token ticker.
func handleTGTokenSelected(chatID int64, sess *tgSession, ticker string) {
	// Find all networks for this ticker
	results := searchTokens(ticker)
	var networks []TokenInfo
	for _, t := range results {
		if strings.EqualFold(t.Ticker, ticker) {
			networks = append(networks, t)
		}
	}

	if len(networks) == 0 {
		handleTGBackToCard(chatID, sess)
		return
	}

	if len(networks) == 1 {
		applyTokenSelection(sess, networks[0])
		sess.State = stateSwapCard
		updateSwapCard(chatID, sess)
		return
	}

	// Multiple networks — show network picker
	sess.State = statePickNet

	// Deduplicate by chain name
	seen := make(map[string]bool)
	var unique []TokenInfo
	for _, t := range networks {
		key := strings.ToLower(t.ChainName)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, t)
		}
	}

	text := fmt.Sprintf("<b>Select network for %s:</b>", ticker)
	var rows [][]TGInlineKeyboardButton
	for i := 0; i < len(unique); i += 3 {
		var row []TGInlineKeyboardButton
		for j := i; j < i+3 && j < len(unique); j++ {
			label := networkDisplayName(unique[j].ChainName)
			row = append(row, TGInlineKeyboardButton{
				Text:         label,
				CallbackData: "tn:" + ticker + ":" + unique[j].ChainName,
			})
		}
		rows = append(rows, row)
	}
	rows = append(rows, []TGInlineKeyboardButton{
		{Text: "← Back", CallbackData: "bk"},
	})

	if sess.CardMsgID != 0 {
		tgEditMessage(chatID, sess.CardMsgID, text, &TGInlineKeyboardMarkup{InlineKeyboard: rows})
	}
}

// handleTGNetworkSelected handles selection of a specific network for a token.
func handleTGNetworkSelected(chatID int64, sess *tgSession, data string) {
	// data format: "TICKER:network"
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		handleTGBackToCard(chatID, sess)
		return
	}
	ticker, net := parts[0], parts[1]

	token := findToken(ticker, net)
	if token == nil {
		handleTGBackToCard(chatID, sess)
		return
	}

	applyTokenSelection(sess, *token)
	sess.State = stateSwapCard
	updateSwapCard(chatID, sess)
}

// handleTGTokenPage handles pagination in the token picker.
func handleTGTokenPage(chatID int64, sess *tgSession, pageStr string) {
	page, _ := strconv.Atoi(pageStr)
	sess.PickPage = page
	text, markup := renderTokenPicker(sess, page)
	if sess.CardMsgID != 0 {
		tgEditMessage(chatID, sess.CardMsgID, text, markup)
	}
}

// applyTokenSelection sets the selected token on the correct side.
func applyTokenSelection(sess *tgSession, token TokenInfo) {
	if sess.PickSide == "from" {
		sess.FromTicker = token.Ticker
		sess.FromNet = token.ChainName
		sess.RefundAddr = "" // clear since chain changed
	} else {
		sess.ToTicker = token.Ticker
		sess.ToNet = token.ChainName
		sess.RecvAddr = "" // clear since chain changed
	}
}

// --- Swap Direction ---

func handleTGSwapDirection(chatID int64, sess *tgSession) {
	sess.FromTicker, sess.ToTicker = sess.ToTicker, sess.FromTicker
	sess.FromNet, sess.ToNet = sess.ToNet, sess.FromNet
	sess.RefundAddr, sess.RecvAddr = sess.RecvAddr, sess.RefundAddr
	sess.Amount = "" // clear amount on swap
	sess.State = stateSwapCard
	updateSwapCard(chatID, sess)
}

// --- Amount & Address Input ---

func handleTGPromptAmount(chatID int64, sess *tgSession) {
	sess.State = stateEnterAmount
	prompt := fmt.Sprintf("Enter amount of %s to swap:", sess.FromTicker)
	msg, err := tgSendMessage(chatID, prompt, &TGForceReply{
		ForceReply:            true,
		Selective:             true,
		InputFieldPlaceholder: "e.g. 0.5",
	})
	if err == nil {
		sess.PromptMsgID = msg.MessageID
	}
}

func handleTGAmountInput(chatID int64, sess *tgSession, msg *TGMessage) {
	amount := strings.TrimSpace(msg.Text)

	// Basic validation
	if _, err := humanToAtomic(amount, 8); err != nil {
		errMsg, _ := tgSendMessage(chatID, "Invalid amount. Please enter a number (e.g. 0.5).", nil)
		if errMsg != nil {
			go func() {
				tgDeleteMessage(chatID, errMsg.MessageID)
			}()
		}
		return
	}

	sess.Amount = amount
	sess.State = stateSwapCard

	cleanupPromptReply(chatID, sess, msg.MessageID)
	updateSwapCard(chatID, sess)
}

func handleTGPromptRefund(chatID int64, sess *tgSession) {
	sess.State = stateEnterRefund
	prompt := fmt.Sprintf("Enter your %s refund address:", sess.FromTicker)
	msg, err := tgSendMessage(chatID, prompt, &TGForceReply{
		ForceReply:            true,
		Selective:             true,
		InputFieldPlaceholder: "Paste address...",
	})
	if err == nil {
		sess.PromptMsgID = msg.MessageID
	}
}

func handleTGRefundInput(chatID int64, sess *tgSession, msg *TGMessage) {
	addr := strings.TrimSpace(msg.Text)
	if len(addr) < 10 {
		tgSendMessage(chatID, "Address seems too short. Please try again.", nil)
		return
	}

	sess.RefundAddr = addr
	sess.State = stateSwapCard

	cleanupPromptReply(chatID, sess, msg.MessageID)
	updateSwapCard(chatID, sess)
}

func handleTGPromptRecv(chatID int64, sess *tgSession) {
	sess.State = stateEnterRecv
	prompt := fmt.Sprintf("Enter your %s receive address:", sess.ToTicker)
	msg, err := tgSendMessage(chatID, prompt, &TGForceReply{
		ForceReply:            true,
		Selective:             true,
		InputFieldPlaceholder: "Paste address...",
	})
	if err == nil {
		sess.PromptMsgID = msg.MessageID
	}
}

func handleTGRecvInput(chatID int64, sess *tgSession, msg *TGMessage) {
	addr := strings.TrimSpace(msg.Text)
	if len(addr) < 10 {
		tgSendMessage(chatID, "Address seems too short. Please try again.", nil)
		return
	}

	sess.RecvAddr = addr
	sess.State = stateSwapCard

	cleanupPromptReply(chatID, sess, msg.MessageID)
	updateSwapCard(chatID, sess)
}

// --- Slippage ---

func handleTGSetSlippage(chatID int64, sess *tgSession, value string) {
	sess.Slippage = value
	sess.State = stateSwapCard
	updateSwapCard(chatID, sess)
}

// --- Back to Card ---

func handleTGBackToCard(chatID int64, sess *tgSession) {
	sess.State = stateSwapCard
	sess.PickSide = ""
	updateSwapCard(chatID, sess)
}

// --- Helpers ---

// cleanupPromptReply deletes the prompt and user reply messages.
func cleanupPromptReply(chatID int64, sess *tgSession, replyMsgID int) {
	if sess.PromptMsgID != 0 {
		tgDeleteMessage(chatID, sess.PromptMsgID)
		sess.PromptMsgID = 0
	}
	if replyMsgID != 0 {
		tgDeleteMessage(chatID, replyMsgID)
	}
}

// truncAddr shortens an address for display.
func truncAddr(addr string) string {
	if len(addr) <= 16 {
		return addr
	}
	return addr[:8] + "..." + addr[len(addr)-6:]
}

// networkDisplayName maps chain codes to display names.
func networkDisplayName(chain string) string {
	names := map[string]string{
		"eth": "Ethereum", "btc": "Bitcoin", "sol": "Solana", "base": "Base",
		"arb": "Arbitrum", "ton": "TON", "tron": "TRON", "bsc": "BNB Chain",
		"pol": "Polygon", "op": "Optimism", "avax": "Avalanche", "near": "NEAR",
		"sui": "Sui", "apt": "Aptos", "aptos": "Aptos", "doge": "Dogecoin",
		"ltc": "Litecoin", "xrp": "XRP", "bch": "Bitcoin Cash",
		"xlm": "Stellar", "stellar": "Stellar", "zec": "Zcash",
	}
	if name, ok := names[strings.ToLower(chain)]; ok {
		return name
	}
	return chain
}
