package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// handleTelegramWebhook processes incoming updates from Telegram.
func handleTelegramWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	var update TGUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		http.Error(w, "parse error", http.StatusBadRequest)
		return
	}

	// Always respond 200 to acknowledge the update
	w.WriteHeader(http.StatusOK)

	// Route to handler
	if update.InlineQuery != nil {
		go handleTGInlineQuery(update.InlineQuery)
		return
	}
	if update.CallbackQuery != nil {
		go handleTGCallback(update.CallbackQuery)
		return
	}
	if update.Message != nil {
		go handleTGMessage(update.Message)
		return
	}
}

// handleTGMessage routes text messages and commands.
func handleTGMessage(msg *TGMessage) {
	// Only handle private chats
	if msg.Chat.Type != "private" {
		return
	}

	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	// Handle commands
	if strings.HasPrefix(text, "/") {
		cmd := strings.SplitN(text, " ", 2)
		switch strings.ToLower(strings.TrimSuffix(cmd[0], "@"+botUsername())) {
		case "/start":
			startParam := ""
			if len(cmd) > 1 {
				startParam = strings.TrimSpace(cmd[1])
			}
			handleTGStart(chatID, startParam)
		case "/verify":
			handleTGVerify(chatID)
		case "/status":
			if len(cmd) > 1 {
				handleTGStatus(chatID, strings.TrimSpace(cmd[1]))
			} else {
				tgSendMessage(chatID, "Usage: /status <order_token>", nil)
			}
		default:
			tgSendMessage(chatID, "Unknown command. Use /start to begin a swap.", nil)
		}
		return
	}

	// Handle text input (replies to ForceReply prompts)
	sess := tgSessions.get(chatID)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	switch sess.State {
	case stateEnterAmount:
		handleTGAmountInput(chatID, sess, msg)
	case stateEnterRefund:
		handleTGRefundInput(chatID, sess, msg)
	case stateEnterRecv:
		handleTGRecvInput(chatID, sess, msg)
	case statePickToken:
		// Token search by typing
		handleTGTokenSearch(chatID, sess, msg)
	default:
		// Ignore unexpected text
	}
}

// handleTGCallback routes inline button presses.
func handleTGCallback(cb *TGCallbackQuery) {
	if cb.Message == nil {
		tgAnswerCallback(cb.ID, "")
		return
	}

	// Only private chats
	if cb.Message.Chat.Type != "private" {
		tgAnswerCallback(cb.ID, "")
		return
	}

	chatID := cb.Message.Chat.ID
	data := cb.Data

	sess := tgSessions.get(chatID)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	// Acknowledge callback
	switch {
	case data == "pf":
		tgAnswerCallback(cb.ID, "")
		handleTGPickToken(chatID, sess, "from")
	case data == "pt":
		tgAnswerCallback(cb.ID, "")
		handleTGPickToken(chatID, sess, "to")
	case data == "sw":
		tgAnswerCallback(cb.ID, "Swapped!")
		handleTGSwapDirection(chatID, sess)
	case data == "sa":
		tgAnswerCallback(cb.ID, "")
		handleTGPromptAmount(chatID, sess)
	case data == "sr":
		tgAnswerCallback(cb.ID, "")
		handleTGPromptRefund(chatID, sess)
	case data == "sp":
		tgAnswerCallback(cb.ID, "")
		handleTGPromptRecv(chatID, sess)
	case strings.HasPrefix(data, "sl:"):
		tgAnswerCallback(cb.ID, "Slippage: "+data[3:]+"%")
		handleTGSetSlippage(chatID, sess, data[3:])
	case strings.HasPrefix(data, "ts:"):
		tgAnswerCallback(cb.ID, "")
		handleTGTokenSelected(chatID, sess, data[3:])
	case strings.HasPrefix(data, "tn:"):
		tgAnswerCallback(cb.ID, "")
		handleTGNetworkSelected(chatID, sess, data[3:])
	case strings.HasPrefix(data, "tp:"):
		tgAnswerCallback(cb.ID, "")
		handleTGTokenPage(chatID, sess, data[3:])
	case data == "gq":
		tgAnswerCallback(cb.ID, "Loading quote...")
		handleTGGetQuote(chatID, sess)
	case data == "cs":
		tgAnswerCallback(cb.ID, "Confirming swap...")
		handleTGConfirmSwap(chatID, sess)
	case data == "cq":
		tgAnswerCallback(cb.ID, "Cancelled")
		handleTGCancelQuote(chatID, sess)
	case data == "bk":
		tgAnswerCallback(cb.ID, "")
		handleTGBackToCard(chatID, sess)
	case data == "rs":
		tgAnswerCallback(cb.ID, "Refreshing...")
		handleTGRefreshStatus(chatID, sess)
	case data == "dm":
		tgAnswerCallback(cb.ID, "Messages deleted")
		handleTGDeleteMessages(chatID, sess)
	case data == "ns":
		tgAnswerCallback(cb.ID, "")
		handleTGNewSwap(chatID, sess)
	default:
		tgAnswerCallback(cb.ID, "")
	}
}

// handleTGStart sends the welcome message and swap card.
// startParam may contain a deep-link pre-fill (e.g. "swap_BTC-btc_ETH-eth_0.5").
func handleTGStart(chatID int64, startParam string) {
	sess := tgSessions.get(chatID)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	// Delete old card if exists
	if sess.CardMsgID != 0 {
		tgDeleteMessage(chatID, sess.CardMsgID)
	}

	sess.reset()
	if strings.HasPrefix(startParam, "swap_") {
		parseSwapStartParam(sess, startParam[5:])
	}
	sess.State = stateSwapCard

	text, markup := renderSwapCard(sess)
	msg, err := tgSendMessage(chatID, text, markup)
	if err != nil {
		log.Printf("tg /start error: %v", err)
		return
	}
	sess.CardMsgID = msg.MessageID
	sess.trackMsg(msg.MessageID)
}

// handleTGVerify shows commit hash and link to verify page.
func handleTGVerify(chatID int64) {
	text := "<b>Ø uSwap Zero — 🔍 Verify</b>\n\n" +
		"Commit: <code>" + commitHash + "</code>\n" +
		"Build: " + buildTime + "\n\n" +
		"<a href=\"" + tgAppURL + "/verify\">Verify source →</a>"
	tgSendMessage(chatID, text, nil)
}

// handleTGNewSwap starts a fresh swap while lock is already held.
func handleTGNewSwap(chatID int64, sess *tgSession) {
	if sess.CardMsgID != 0 {
		tgDeleteMessage(chatID, sess.CardMsgID)
	}

	sess.reset()
	sess.State = stateSwapCard

	text, markup := renderSwapCard(sess)
	msg, err := tgSendMessage(chatID, text, markup)
	if err != nil {
		log.Printf("tg new swap error: %v", err)
		return
	}
	sess.CardMsgID = msg.MessageID
	sess.trackMsg(msg.MessageID)
}

// handleTGStatus looks up an order by token, sends the unified order card,
// and wires it into the session so Refresh/Clear/New Swap buttons work.
func handleTGStatus(chatID int64, token string) {
	order, err := decryptOrderData(token)
	if err != nil {
		tgSendMessage(chatID, "Invalid order token.", nil)
		return
	}

	status, err := fetchStatus(order.DepositAddr, order.Memo)
	if err != nil {
		tgSendMessage(chatID, "❌ Status check failed: "+err.Error(), nil)
		return
	}

	cardText, markup := buildOrderCard(order, status, token)

	sess := tgSessions.get(chatID)
	sess.mu.Lock()
	defer sess.mu.Unlock()

	// Replace any existing card
	if sess.CardMsgID != 0 {
		tgDeleteMessage(chatID, sess.CardMsgID)
	}

	msg, err := tgSendMessage(chatID, cardText, markup)
	if err != nil {
		log.Printf("tg /status send error: %v", err)
		return
	}
	sess.CardMsgID = msg.MessageID
	sess.OrderToken = token
	sess.State = stateOrderActive
}

// botUsername returns the bot's Telegram username for command suffix stripping.
func botUsername() string {
	return tgBotUsername
}
