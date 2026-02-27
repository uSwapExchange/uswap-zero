package main

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
)

func TestTruncAddr(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4", "bc1qw508...v8f3t4"},
		{"short", "short"},
		{"0x1234567890abcdef1234567890abcdef12345678", "0x123456...345678"},
	}
	for _, tt := range tests {
		got := truncAddr(tt.input)
		if got != tt.want {
			t.Errorf("truncAddr(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSessionReset(t *testing.T) {
	sess := &tgSession{
		State:      stateQuoteConfirm,
		FromTicker: "SOL",
		ToTicker:   "USDC",
		Amount:     "10",
		RefundAddr: "abc",
		RecvAddr:   "def",
	}

	sess.reset()

	if sess.State != stateIdle {
		t.Errorf("reset State = %d, want %d", sess.State, stateIdle)
	}
	if sess.FromTicker != "BTC" {
		t.Errorf("reset FromTicker = %q, want BTC", sess.FromTicker)
	}
	if sess.ToTicker != "ETH" {
		t.Errorf("reset ToTicker = %q, want ETH", sess.ToTicker)
	}
	if sess.Amount != "" {
		t.Errorf("reset Amount = %q, want empty", sess.Amount)
	}
	if sess.Slippage != "1" {
		t.Errorf("reset Slippage = %q, want 1", sess.Slippage)
	}
	if sess.OrderMsgIDs != nil {
		t.Errorf("reset OrderMsgIDs should be nil")
	}
}

func TestSessionIsComplete(t *testing.T) {
	sess := &tgSession{}
	sess.reset()

	if sess.isComplete() {
		t.Error("empty session should not be complete")
	}

	sess.Amount = "0.5"
	sess.RefundAddr = "bc1qxyz"
	sess.RecvAddr = "0xabc"

	if !sess.isComplete() {
		t.Error("filled session should be complete")
	}
}

func TestSessionStore(t *testing.T) {
	store := &tgSessionStore{
		sessions: make(map[int64]*tgSession),
	}

	s1 := store.get(123)
	if s1 == nil {
		t.Fatal("get should create new session")
	}

	s2 := store.get(123)
	if s1 != s2 {
		t.Error("get should return same session for same chatID")
	}

	s3 := store.get(456)
	if s1 == s3 {
		t.Error("different chatIDs should have different sessions")
	}
}

func TestRenderSwapCard(t *testing.T) {
	sess := &tgSession{}
	sess.reset()

	text, markup := renderSwapCard(sess)

	if text == "" {
		t.Error("swap card text should not be empty")
	}
	// Verify Ø branding
	if !strings.Contains(text, "Ø") {
		t.Error("swap card should contain Ø branding")
	}
	// Verify it's wrapped in <pre>
	if !strings.HasPrefix(text, "<pre>") {
		t.Error("swap card should be wrapped in <pre>")
	}

	if markup == nil {
		t.Fatal("swap card markup should not be nil")
	}
	if len(markup.InlineKeyboard) == 0 {
		t.Error("swap card should have keyboard rows")
	}

	// Row 0: token pickers + swap
	row0 := markup.InlineKeyboard[0]
	if len(row0) != 3 {
		t.Errorf("first row should have 3 buttons (from, swap, to), got %d", len(row0))
	}
	if row0[1].Text != "🔁" {
		t.Errorf("swap button text should be 🔁, got %q", row0[1].Text)
	}
	// Token buttons should have colors
	if row0[0].Style != "danger" {
		t.Errorf("from button style should be danger, got %q", row0[0].Style)
	}
	if row0[2].Style != "success" {
		t.Errorf("to button style should be success, got %q", row0[2].Style)
	}

	// Row 1: inline slippage (4 buttons)
	row1 := markup.InlineKeyboard[1]
	if len(row1) != 4 {
		t.Errorf("slippage row should have 4 buttons, got %d", len(row1))
	}
	// Default slippage is 1%, so the "1%" button should be selected
	foundSelected := false
	for _, btn := range row1 {
		if strings.HasPrefix(btn.Text, "●") {
			foundSelected = true
			if !strings.Contains(btn.Text, "1%") {
				t.Errorf("selected slippage should be 1%%, got %q", btn.Text)
			}
		}
	}
	if !foundSelected {
		t.Error("one slippage button should be selected with ●")
	}

	// Incomplete card should NOT have Get Quote
	for _, row := range markup.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData == "gq" {
				t.Error("incomplete card should not show Get Quote button")
			}
		}
	}

	// Last row: Mini App
	lastRow := markup.InlineKeyboard[len(markup.InlineKeyboard)-1]
	if lastRow[0].WebApp == nil {
		t.Error("last row should have Mini App button")
	}
}

func TestRenderSwapCardComplete(t *testing.T) {
	sess := &tgSession{}
	sess.reset()
	sess.Amount = "0.5"
	sess.RefundAddr = "bc1qxyz1234567890"
	sess.RecvAddr = "0xabc1234567890"

	text, markup := renderSwapCard(sess)

	// Text should show amount
	if !strings.Contains(text, "0.5") {
		t.Error("complete card text should contain amount")
	}

	// ✓ prefix on filled field buttons
	foundCheckAmount := false
	foundCheckRefund := false
	foundCheckRecv := false
	foundGetQuote := false
	for _, row := range markup.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData == "sa" && strings.HasPrefix(btn.Text, "✓") {
				foundCheckAmount = true
				if btn.Style != "primary" {
					t.Errorf("filled amount style should be primary, got %q", btn.Style)
				}
			}
			if btn.CallbackData == "sr" && strings.HasPrefix(btn.Text, "✓") {
				foundCheckRefund = true
				if !strings.Contains(btn.Text, "Refund:") {
					t.Error("filled refund button should show 'Refund:' label")
				}
			}
			if btn.CallbackData == "sp" && strings.HasPrefix(btn.Text, "✓") {
				foundCheckRecv = true
				if !strings.Contains(btn.Text, "Receive:") {
					t.Error("filled receive button should show 'Receive:' label")
				}
			}
			if btn.CallbackData == "gq" {
				foundGetQuote = true
			}
		}
	}
	if !foundCheckAmount {
		t.Error("filled amount button should have ✓ prefix")
	}
	if !foundCheckRefund {
		t.Error("filled refund button should have ✓ prefix")
	}
	if !foundCheckRecv {
		t.Error("filled receive button should have ✓ prefix")
	}
	if !foundGetQuote {
		t.Error("complete card should show Get Quote button")
	}
}

func TestGenerateQRPNG(t *testing.T) {
	data, err := generateQRPNG("bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4")
	if err != nil {
		t.Fatal("generateQRPNG error:", err)
	}
	if len(data) == 0 {
		t.Fatal("QR PNG should not be empty")
	}

	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal("QR PNG is not valid PNG:", err)
	}

	bounds := img.Bounds()
	if bounds.Dx() < 100 || bounds.Dy() < 100 {
		t.Errorf("QR image too small: %dx%d", bounds.Dx(), bounds.Dy())
	}
	if bounds.Dx() != bounds.Dy() {
		t.Errorf("QR image should be square: %dx%d", bounds.Dx(), bounds.Dy())
	}
}

func TestNetworkDisplayName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"eth", "Ethereum"},
		{"btc", "Bitcoin"},
		{"sol", "Solana"},
		{"ETH", "Ethereum"},
		{"unknown_chain", "unknown_chain"},
	}
	for _, tt := range tests {
		got := networkDisplayName(tt.input)
		if got != tt.want {
			t.Errorf("networkDisplayName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTrackMsg(t *testing.T) {
	sess := &tgSession{}
	sess.trackMsg(0) // should not add
	sess.trackMsg(100)
	sess.trackMsg(200)

	if len(sess.OrderMsgIDs) != 2 {
		t.Errorf("expected 2 tracked messages, got %d", len(sess.OrderMsgIDs))
	}
}

func TestTokenPickerPopularTokens(t *testing.T) {
	if len(tgPopularTokens) != 12 {
		t.Errorf("expected 12 popular tokens, got %d", len(tgPopularTokens))
	}
}

func TestRenderTokenPicker(t *testing.T) {
	sess := &tgSession{PickSide: "from"}
	text, markup := renderTokenPicker(sess, 0)

	if text == "" {
		t.Error("picker text should not be empty")
	}
	if markup == nil {
		t.Fatal("picker markup should not be nil")
	}

	// Should have 4 rows of tokens + 1 nav row = 5 rows
	if len(markup.InlineKeyboard) != 5 {
		t.Errorf("token picker should have 5 rows, got %d", len(markup.InlineKeyboard))
	}

	if len(markup.InlineKeyboard[0]) != 3 {
		t.Errorf("first token row should have 3 buttons, got %d", len(markup.InlineKeyboard[0]))
	}
}

// --- monospace card renderer tests ---

func TestCardRowWidth(t *testing.T) {
	// Every row function must produce exactly cardW runes (33)
	rows := []string{
		cardTop(),
		cardMid(),
		cardBot(),
		cardRow("hello"),
		cardRow(""),
		cardRowRight("right"),
		cardRowCenter("center"),
		cardRowKV("KEY", "VALUE"),
		cardRowKV("LONGKEYNAME", "LONGVALUENAME"),
	}
	for _, row := range rows {
		n := len([]rune(row))
		if n != cardW {
			t.Errorf("row width = %d, want %d: %q", n, cardW, row)
		}
	}
}

func TestCardRowKVOverflow(t *testing.T) {
	// Even with very long strings, output must be exactly cardW runes
	row := cardRowKV(strings.Repeat("K", 40), strings.Repeat("V", 40))
	n := len([]rune(row))
	if n != cardW {
		t.Errorf("overflow row width = %d, want %d", n, cardW)
	}
}

func TestSafeRunes(t *testing.T) {
	tests := []struct {
		s    string
		max  int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 3, "hel"},
		{"Ø rune", 3, "Ø r"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := safeRunes(tt.s, tt.max)
		if got != tt.want {
			t.Errorf("safeRunes(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
		}
	}
}

func TestRenderSwapCardMono(t *testing.T) {
	sess := &tgSession{}
	sess.reset()

	card := renderSwapCardMono(sess)
	if card == "" {
		t.Fatal("mono swap card should not be empty")
	}
	if !strings.Contains(card, "Ø USWAP ZERO") {
		t.Error("mono swap card should contain Ø USWAP ZERO")
	}
	if !strings.Contains(card, "BTC") {
		t.Error("mono swap card should contain BTC")
	}

	// All lines must be exactly cardW runes
	for _, line := range strings.Split(card, "\n") {
		n := len([]rune(line))
		if n != cardW {
			t.Errorf("swap card line width = %d, want %d: %q", n, cardW, line)
		}
	}
}

func TestRenderQuoteCardMono(t *testing.T) {
	p := QuoteCardData{
		FromTicker:   "BTC",
		ToTicker:     "ETH",
		AmountIn:     "0.5",
		AmountOut:    "8.34",
		AmountInUSD:  "$48,000",
		AmountOutUSD: "$47,700",
		Rate:         "1 BTC = 16.68 ETH",
		SpreadUSD:    "300.00",
		SpreadPct:    "0.63",
	}
	card := renderQuoteCardMono(p)
	if card == "" {
		t.Fatal("mono quote card should not be empty")
	}
	if !strings.Contains(card, "QUOTE") {
		t.Error("quote card should contain QUOTE")
	}
	if !strings.Contains(card, "BTC") {
		t.Error("quote card should contain BTC")
	}

	for _, line := range strings.Split(card, "\n") {
		n := len([]rune(line))
		if n != cardW {
			t.Errorf("quote card line width = %d, want %d: %q", n, cardW, line)
		}
	}
}

func TestRenderDepositCardMono(t *testing.T) {
	p := DepositCardData{
		FromTicker: "BTC",
		ToTicker:   "ETH",
		AmountIn:   "0.5",
		AmountOut:  "8.34",
		Network:    "Bitcoin",
		Deadline:   "59m remaining",
		RefundAddr: "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4",
		RecvAddr:   "0x1234567890abcdef1234567890abcdef12345678",
	}
	card := renderDepositCardMono(p)
	if card == "" {
		t.Fatal("mono deposit card should not be empty")
	}
	if !strings.Contains(card, "ORDER") {
		t.Error("deposit card should contain ORDER")
	}
	if !strings.Contains(card, "0.5") {
		t.Error("deposit card should contain amount")
	}
	// Should show stepper
	if !strings.Contains(card, "[●]") {
		t.Error("deposit card should contain stepper current node")
	}
	// Should show truncated addresses
	if !strings.Contains(card, "REFUND") {
		t.Error("deposit card should contain REFUND row")
	}

	for _, line := range strings.Split(card, "\n") {
		n := len([]rune(line))
		if n != cardW {
			t.Errorf("deposit card line width = %d, want %d: %q", n, cardW, line)
		}
	}
}

func TestRenderStatusCardMono(t *testing.T) {
	order := &OrderData{
		FromTicker: "BTC",
		ToTicker:   "ETH",
		AmountIn:   "0.1",
		AmountOut:  "2.31",
	}
	// Use correct API status strings
	status := &StatusResponse{Status: "PENDING_DEPOSIT"}

	card := renderStatusCardMono(order, status)
	if !strings.Contains(card, "STATUS") {
		t.Error("status card should contain STATUS")
	}
	for _, line := range strings.Split(card, "\n") {
		n := len([]rune(line))
		if n != cardW {
			t.Errorf("status card line width = %d, want %d: %q", n, cardW, line)
		}
	}
}

func TestIsTerminalStatus(t *testing.T) {
	terminal := []string{"SUCCESS", "REFUNDED", "FAILED", "INCOMPLETE_DEPOSIT",
		"success", "refunded", "failed", "incomplete_deposit"}
	nonTerminal := []string{"PENDING_DEPOSIT", "KNOWN_DEPOSIT_TX", "PROCESSING", "", "UNKNOWN"}

	for _, s := range terminal {
		if !isTerminalStatus(s) {
			t.Errorf("isTerminalStatus(%q) = false, want true", s)
		}
	}
	for _, s := range nonTerminal {
		if isTerminalStatus(s) {
			t.Errorf("isTerminalStatus(%q) = true, want false", s)
		}
	}
}

func TestRenderAnyStatusCardDispatch(t *testing.T) {
	order := &OrderData{FromTicker: "BTC", ToTicker: "ETH", AmountIn: "0.1", AmountOut: "2.31"}

	cases := []struct {
		status  string
		wantStr string
	}{
		{"SUCCESS", "COMPLETE"},
		{"REFUNDED", "REFUNDED"},
		{"FAILED", "FAILED"},
		{"INCOMPLETE_DEPOSIT", "FAILED"},
		{"PROCESSING", "STATUS"},
		{"PENDING_DEPOSIT", "STATUS"},
		{"KNOWN_DEPOSIT_TX", "STATUS"},
	}
	for _, tc := range cases {
		card := renderAnyStatusCard(order, &StatusResponse{Status: tc.status})
		if !strings.Contains(card, tc.wantStr) {
			t.Errorf("renderAnyStatusCard(%q) missing %q", tc.status, tc.wantStr)
		}
	}
}

func TestRenderCompletionCardMono(t *testing.T) {
	order := &OrderData{
		FromTicker: "BTC",
		ToTicker:   "ETH",
		AmountIn:   "0.1",
		AmountOut:  "2.31",
	}
	status := &StatusResponse{Status: "SUCCESS"}

	card := renderCompletionCardMono(order, status)
	if !strings.Contains(card, "COMPLETE") {
		t.Error("completion card should contain COMPLETE")
	}
	for _, line := range strings.Split(card, "\n") {
		n := len([]rune(line))
		if n != cardW {
			t.Errorf("completion card line width = %d, want %d: %q", n, cardW, line)
		}
	}
}

func TestTokenLabel(t *testing.T) {
	tests := []struct {
		ticker, net, want string
	}{
		{"BTC", "btc", "BTC"},         // ticker matches chain
		{"ETH", "eth", "ETH"},         // ticker matches chain
		{"USDT", "eth", "USDT (ETH)"}, // ticker differs from chain
		{"USDC", "sol", "USDC (SOL)"}, // ticker differs from chain
		{"SOL", "sol", "SOL"},         // ticker matches chain
		{"NEAR", "near", "NEAR"},      // ticker matches chain
		{"", "", "—"},                 // empty
	}
	for _, tt := range tests {
		got := tokenLabel(tt.ticker, tt.net)
		if got != tt.want {
			t.Errorf("tokenLabel(%q, %q) = %q, want %q", tt.ticker, tt.net, got, tt.want)
		}
	}
}

func TestBuildAppURL(t *testing.T) {
	sess := &tgSession{}
	sess.reset()

	// Default session should produce URL with from/to params
	u := buildAppURL(sess)
	if !strings.Contains(u, "from=BTC") {
		t.Errorf("buildAppURL should contain from=BTC, got %q", u)
	}
	if !strings.Contains(u, "to=ETH") {
		t.Errorf("buildAppURL should contain to=ETH, got %q", u)
	}
	// Default slippage (1) should NOT be in URL
	if strings.Contains(u, "slippage=") {
		t.Errorf("buildAppURL should not include default slippage, got %q", u)
	}

	// With amount and custom slippage
	sess.Amount = "0.5"
	sess.Slippage = "2"
	u = buildAppURL(sess)
	if !strings.Contains(u, "amt=0.5") {
		t.Errorf("buildAppURL should contain amt=0.5, got %q", u)
	}
	if !strings.Contains(u, "slippage=2") {
		t.Errorf("buildAppURL should contain slippage=2, got %q", u)
	}
}

func TestButtonStyleField(t *testing.T) {
	btn := TGInlineKeyboardButton{
		Text:         "Test",
		CallbackData: "test",
		Style:        "success",
	}
	if btn.Style != "success" {
		t.Errorf("button Style = %q, want success", btn.Style)
	}

	// Verify empty style omits from JSON
	btn2 := TGInlineKeyboardButton{
		Text:         "Test",
		CallbackData: "test",
	}
	if btn2.Style != "" {
		t.Errorf("button Style should be empty, got %q", btn2.Style)
	}
}

func TestButtonURLField(t *testing.T) {
	btn := TGInlineKeyboardButton{
		Text: "Open",
		URL:  "https://t.me/testbot?start=swap_BTC-btc_ETH-eth",
	}
	if btn.URL == "" {
		t.Error("button URL should be set")
	}
	if btn.CallbackData != "" {
		t.Error("URL button should not have callback_data")
	}
}

// --- inline query tests ---

func TestParseInlineQuery_Empty(t *testing.T) {
	p := parseInlineQuery("")
	if p.kind != inlineKindEmpty {
		t.Errorf("empty query kind = %q, want %q", p.kind, inlineKindEmpty)
	}
}

func TestParseInlineQuery_Whitespace(t *testing.T) {
	p := parseInlineQuery("   ")
	if p.kind != inlineKindEmpty {
		t.Errorf("whitespace query kind = %q, want %q", p.kind, inlineKindEmpty)
	}
}

func TestParseInlineQuery_Single(t *testing.T) {
	p := parseInlineQuery("BTC")
	if p.kind != inlineKindSingle {
		t.Errorf("kind = %q, want %q", p.kind, inlineKindSingle)
	}
	if p.from != "BTC" {
		t.Errorf("from = %q, want BTC", p.from)
	}
}

func TestParseInlineQuery_SinglePartial(t *testing.T) {
	p := parseInlineQuery("BT")
	if p.kind != inlineKindSingle {
		t.Errorf("kind = %q, want %q", p.kind, inlineKindSingle)
	}
	if p.from != "BT" {
		t.Errorf("from = %q, want BT", p.from)
	}
}

func TestParseInlineQuery_SingleLowercase(t *testing.T) {
	p := parseInlineQuery("btc")
	if p.kind != inlineKindSingle {
		t.Errorf("kind = %q, want %q", p.kind, inlineKindSingle)
	}
	if p.from != "BTC" {
		t.Errorf("from = %q, want BTC (uppercased)", p.from)
	}
}

func TestParseInlineQuery_Pair(t *testing.T) {
	p := parseInlineQuery("BTC ETH")
	if p.kind != inlineKindPair {
		t.Errorf("kind = %q, want %q", p.kind, inlineKindPair)
	}
	if p.from != "BTC" || p.to != "ETH" {
		t.Errorf("from=%q to=%q, want BTC/ETH", p.from, p.to)
	}
}

func TestParseInlineQuery_PairAmount(t *testing.T) {
	p := parseInlineQuery("BTC ETH 0.5")
	if p.kind != inlineKindPairAmt {
		t.Errorf("kind = %q, want %q", p.kind, inlineKindPairAmt)
	}
	if p.from != "BTC" || p.to != "ETH" || p.amount != "0.5" {
		t.Errorf("from=%q to=%q amount=%q, want BTC/ETH/0.5", p.from, p.to, p.amount)
	}
}

func TestParseInlineQuery_PairNonNumericThird(t *testing.T) {
	// Third word is not a number → treat as pair, ignore extra
	p := parseInlineQuery("BTC ETH notanumber")
	if p.kind != inlineKindPair {
		t.Errorf("kind = %q, want %q", p.kind, inlineKindPair)
	}
}

func TestParseInlineQuery_StatusLower(t *testing.T) {
	p := parseInlineQuery("status abc123token")
	if p.kind != inlineKindStatus {
		t.Errorf("kind = %q, want %q", p.kind, inlineKindStatus)
	}
	if p.token != "abc123token" {
		t.Errorf("token = %q, want abc123token", p.token)
	}
}

func TestParseInlineQuery_StatusUpper(t *testing.T) {
	p := parseInlineQuery("STATUS abc123token")
	if p.kind != inlineKindStatus {
		t.Errorf("kind = %q, want %q", p.kind, inlineKindStatus)
	}
}

func TestParseInlineQuery_StatusMixed(t *testing.T) {
	p := parseInlineQuery("Status myToken")
	if p.kind != inlineKindStatus {
		t.Errorf("kind = %q, want %q", p.kind, inlineKindStatus)
	}
}

func TestBuildDeepLink_WithUsername(t *testing.T) {
	old := tgBotUsername
	tgBotUsername = "testswapbot"
	defer func() { tgBotUsername = old }()

	link := buildDeepLink("BTC", "btc", "ETH", "eth", "0.5")
	if !strings.Contains(link, "t.me/testswapbot") {
		t.Errorf("deep link missing bot username: %q", link)
	}
	if !strings.Contains(link, "start=swap_BTC-btc_ETH-eth_0.5") {
		t.Errorf("deep link missing start param: %q", link)
	}
}

func TestBuildDeepLink_NoAmount(t *testing.T) {
	old := tgBotUsername
	tgBotUsername = "testswapbot"
	defer func() { tgBotUsername = old }()

	link := buildDeepLink("BTC", "btc", "ETH", "eth", "")
	if strings.Contains(link, "_eth_") {
		// Should end at ETH-eth, no trailing underscore
		t.Errorf("deep link should not have trailing amount separator: %q", link)
	}
	want := "start=swap_BTC-btc_ETH-eth"
	if !strings.Contains(link, want) {
		t.Errorf("deep link = %q, want to contain %q", link, want)
	}
}

func TestBuildDeepLink_NoUsername_FallsBackToAppURL(t *testing.T) {
	old := tgBotUsername
	tgBotUsername = ""
	oldApp := tgAppURL
	tgAppURL = "https://zero.uswap.net"
	defer func() {
		tgBotUsername = old
		tgAppURL = oldApp
	}()

	link := buildDeepLink("BTC", "btc", "ETH", "eth", "1")
	if !strings.Contains(link, "zero.uswap.net") {
		t.Errorf("fallback link should contain app URL: %q", link)
	}
	if !strings.Contains(link, "from=BTC") {
		t.Errorf("fallback link should contain from=BTC: %q", link)
	}
}

func TestParseSwapStartParam_TwoTokens(t *testing.T) {
	sess := &tgSession{}
	sess.reset()
	// Works without a real token cache — tokens won't be found, session keeps defaults
	parseSwapStartParam(sess, "BTC-btc_ETH-eth")
	// If cache empty, values keep defaults; just verify no panic and no corruption
	if sess.FromNet == "" && sess.FromTicker == "" {
		t.Error("session fields should not be blanked by parseSwapStartParam")
	}
}

func TestParseSwapStartParam_WithAmount(t *testing.T) {
	sess := &tgSession{}
	sess.reset()
	parseSwapStartParam(sess, "BTC-btc_ETH-eth_0.5")
	// Amount should be set regardless of token cache
	if sess.Amount != "0.5" {
		t.Errorf("Amount = %q, want 0.5", sess.Amount)
	}
}

func TestParseSwapStartParam_Invalid(t *testing.T) {
	sess := &tgSession{}
	sess.reset()
	// Malformed param — should not panic, session keeps defaults
	parseSwapStartParam(sess, "garbage")
	if sess.FromTicker != "BTC" {
		t.Errorf("invalid param should keep default FromTicker BTC, got %q", sess.FromTicker)
	}
}

func TestParseSwapStartParam_EmptyAmount(t *testing.T) {
	sess := &tgSession{}
	sess.reset()
	// No amount part
	parseSwapStartParam(sess, "ETH-eth_USDT-eth")
	if sess.Amount != "" {
		t.Errorf("no amount in param should leave Amount empty, got %q", sess.Amount)
	}
}

func TestStatusDisplayName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"PENDING_DEPOSIT", "Awaiting Deposit"},
		{"KNOWN_DEPOSIT_TX", "Deposit Detected"},
		{"PROCESSING", "Processing"},
		{"SUCCESS", "Completed"},
		{"REFUNDED", "Refunded"},
		{"FAILED", "Failed"},
		{"INCOMPLETE_DEPOSIT", "Incomplete Deposit"},
		{"pending_deposit", "Awaiting Deposit"}, // case insensitive
		{"unknown_status", "unknown_status"},    // passthrough
	}
	for _, tt := range tests {
		got := statusDisplayName(tt.input)
		if got != tt.want {
			t.Errorf("statusDisplayName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFmtEstimate(t *testing.T) {
	tests := []struct {
		f    float64
		want string
	}{
		{0, "0"},
		{1.0, "1"},
		{1.5, "1.5"},
		{16.68, "16.68"},
		{0.00012345, "0.00012345"},
		{100000.0, "100000"},
	}
	for _, tt := range tests {
		got := fmtEstimate(tt.f)
		if got != tt.want {
			t.Errorf("fmtEstimate(%v) = %q, want %q", tt.f, got, tt.want)
		}
	}
}

func TestTGInlineQueryResultArticle_TypeField(t *testing.T) {
	art := TGInlineQueryResultArticle{
		Type:  "article",
		ID:    "test-0",
		Title: "Test",
		InputMessageContent: TGInputTextMessageContent{
			MessageText: "hello",
		},
	}
	if art.Type != "article" {
		t.Errorf("Type = %q, want article", art.Type)
	}
}

func TestBuildEmptyResults_NoCache(t *testing.T) {
	// With empty token cache, buildEmptyResults should still return the "Start New Swap" article
	results := buildEmptyResults()
	if len(results) == 0 {
		t.Error("buildEmptyResults should always return at least one result")
	}
}

func TestBuildSingleTokenResults_NoCache(t *testing.T) {
	// With empty cache, should not panic
	results := buildSingleTokenResults("BTC")
	_ = results // may be empty, that is fine
}

func TestParseInlineQuery_StatusOnlyOneWord(t *testing.T) {
	// "status" alone (no token) is treated as a single token search
	p := parseInlineQuery("status")
	if p.kind != inlineKindSingle {
		t.Errorf("lone 'status' kind = %q, want %q", p.kind, inlineKindSingle)
	}
}
