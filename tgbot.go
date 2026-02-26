package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
)

// Telegram bot configuration
var (
	tgBotToken      string
	tgWebhookSecret string
	tgAppURL        string
	tgAPIBase       string
	tgHTTPClient    = &http.Client{}
)

// initTelegramBot reads env vars and registers the webhook.
// Returns true if the bot is enabled (TG_BOT_TOKEN is set).
func initTelegramBot() bool {
	tgBotToken = os.Getenv("TG_BOT_TOKEN")
	if tgBotToken == "" {
		return false
	}

	tgAPIBase = "https://api.telegram.org/bot" + tgBotToken

	tgWebhookSecret = os.Getenv("TG_WEBHOOK_SECRET")
	if tgWebhookSecret == "" {
		b := make([]byte, 16)
		rand.Read(b)
		tgWebhookSecret = hex.EncodeToString(b)
		log.Printf("TG_WEBHOOK_SECRET auto-generated: %s", tgWebhookSecret)
	}

	tgAppURL = os.Getenv("TG_APP_URL")
	if tgAppURL == "" {
		tgAppURL = "https://zero.uswap.net"
	}

	// Register webhook
	appURL := tgAppURL + "/tg/webhook/" + tgWebhookSecret
	if err := tgSetWebhook(appURL); err != nil {
		log.Printf("WARNING: Failed to set Telegram webhook: %v", err)
	}

	// Set bot commands
	tgSetCommands()

	return true
}

// --- Telegram API Types ---

// TGUpdate represents an incoming update from Telegram.
type TGUpdate struct {
	UpdateID      int              `json:"update_id"`
	Message       *TGMessage       `json:"message,omitempty"`
	CallbackQuery *TGCallbackQuery `json:"callback_query,omitempty"`
}

// TGMessage represents a Telegram message.
type TGMessage struct {
	MessageID int     `json:"message_id"`
	Chat      TGChat  `json:"chat"`
	From      *TGUser `json:"from,omitempty"`
	Text      string  `json:"text,omitempty"`
	ReplyTo   *TGMessage `json:"reply_to_message,omitempty"`
}

// TGChat represents a Telegram chat.
type TGChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// TGUser represents a Telegram user.
type TGUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

// TGCallbackQuery represents a callback from an inline button press.
type TGCallbackQuery struct {
	ID      string     `json:"id"`
	From    TGUser     `json:"from"`
	Message *TGMessage `json:"message,omitempty"`
	Data    string     `json:"data"`
}

// TGInlineKeyboardMarkup holds inline keyboard buttons.
type TGInlineKeyboardMarkup struct {
	InlineKeyboard [][]TGInlineKeyboardButton `json:"inline_keyboard"`
}

// TGInlineKeyboardButton is a single inline button.
// Style can be "primary" (blue), "danger" (red), or "success" (green) — Bot API 9.4+.
type TGInlineKeyboardButton struct {
	Text         string    `json:"text"`
	CallbackData string    `json:"callback_data,omitempty"`
	WebApp       *TGWebApp `json:"web_app,omitempty"`
	Style        string    `json:"style,omitempty"`
}

// TGWebApp opens a Mini App URL.
type TGWebApp struct {
	URL string `json:"url"`
}

// TGForceReply forces the user to reply to a message.
type TGForceReply struct {
	ForceReply bool `json:"force_reply"`
	Selective  bool `json:"selective"`
	InputFieldPlaceholder string `json:"input_field_placeholder,omitempty"`
}

// TGAPIResponse is the generic Telegram API response wrapper.
type TGAPIResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

// TGSentMessage is the result of sendMessage/editMessage.
type TGSentMessage struct {
	MessageID int    `json:"message_id"`
	Chat      TGChat `json:"chat"`
}

// --- Telegram API Methods ---

// tgRequest makes a JSON POST to the Telegram Bot API.
func tgRequest(method string, payload interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("tg marshal: %w", err)
	}

	resp, err := tgHTTPClient.Post(tgAPIBase+"/"+method, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("tg request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tg read: %w", err)
	}

	var apiResp TGAPIResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, fmt.Errorf("tg parse: %w", err)
	}
	if !apiResp.OK {
		return nil, fmt.Errorf("tg API error: %s", apiResp.Description)
	}
	return apiResp.Result, nil
}

// tgSendMessage sends a text message with optional reply markup.
func tgSendMessage(chatID int64, text string, markup interface{}) (*TGSentMessage, error) {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	result, err := tgRequest("sendMessage", payload)
	if err != nil {
		return nil, err
	}
	var msg TGSentMessage
	json.Unmarshal(result, &msg)
	return &msg, nil
}

// tgEditMessage edits an existing message's text and markup.
func tgEditMessage(chatID int64, messageID int, text string, markup *TGInlineKeyboardMarkup) error {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       text,
		"parse_mode": "HTML",
	}
	if markup != nil {
		payload["reply_markup"] = markup
	}
	_, err := tgRequest("editMessageText", payload)
	return err
}

// tgDeleteMessage deletes a message.
func tgDeleteMessage(chatID int64, messageID int) {
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	tgRequest("deleteMessage", payload)
}

// tgAnswerCallback answers a callback query with an optional toast text.
func tgAnswerCallback(callbackID string, text string) {
	payload := map[string]interface{}{
		"callback_query_id": callbackID,
	}
	if text != "" {
		payload["text"] = text
	}
	tgRequest("answerCallbackQuery", payload)
}

// tgSendPhoto sends a photo (PNG bytes) with a caption and inline keyboard.
func tgSendPhoto(chatID int64, pngData []byte, caption string, markup *TGInlineKeyboardMarkup) (*TGSentMessage, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	w.WriteField("chat_id", strconv.FormatInt(chatID, 10))
	w.WriteField("caption", caption)
	w.WriteField("parse_mode", "HTML")

	if markup != nil {
		markupJSON, _ := json.Marshal(markup)
		w.WriteField("reply_markup", string(markupJSON))
	}

	part, err := w.CreateFormFile("photo", "qr.png")
	if err != nil {
		return nil, fmt.Errorf("tg create form file: %w", err)
	}
	part.Write(pngData)
	w.Close()

	resp, err := tgHTTPClient.Post(tgAPIBase+"/sendPhoto", w.FormDataContentType(), &buf)
	if err != nil {
		return nil, fmt.Errorf("tg send photo: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("tg read photo resp: %w", err)
	}

	var apiResp TGAPIResponse
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return nil, fmt.Errorf("tg parse photo resp: %w", err)
	}
	if !apiResp.OK {
		return nil, fmt.Errorf("tg sendPhoto error: %s", apiResp.Description)
	}

	var msg TGSentMessage
	json.Unmarshal(apiResp.Result, &msg)
	return &msg, nil
}

// tgSetWebhook registers the webhook URL with Telegram.
func tgSetWebhook(url string) error {
	payload := map[string]interface{}{
		"url":             url,
		"allowed_updates": []string{"message", "callback_query"},
	}
	_, err := tgRequest("setWebhook", payload)
	if err != nil {
		return err
	}
	log.Printf("Telegram webhook set to: %s", url)
	return nil
}

// tgSetCommands registers the bot's command list.
func tgSetCommands() {
	commands := []map[string]string{
		{"command": "start", "description": "Start a new swap"},
		{"command": "verify", "description": "Verify deployment integrity"},
		{"command": "status", "description": "Check order status"},
	}
	payload := map[string]interface{}{
		"commands": commands,
	}
	tgRequest("setMyCommands", payload)
}
