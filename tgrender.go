package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// trimAmount formats a decimal amount string to at most maxDecimals places,
// stripping trailing zeros. Falls back to the original string if unparseable.
func trimAmount(s string, maxDecimals int) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	formatted := strconv.FormatFloat(f, 'f', maxDecimals, 64)
	// Strip trailing zeros after decimal point
	if strings.Contains(formatted, ".") {
		formatted = strings.TrimRight(formatted, "0")
		formatted = strings.TrimRight(formatted, ".")
	}
	return formatted
}

// --- Box-drawing constants ---

const (
	cardW     = 33 // total width including │ border chars
	cardInner = 31 // content width (between the │ chars)
)

// --- Low-level helpers ---

// safeRunes truncates s to at most max runes (Unicode-safe).
func safeRunes(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// padRight pads (or truncates) s to exactly n runes using spaces.
func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}

// runeLen returns the rune count of s.
func runeLen(s string) int {
	return len([]rune(s))
}

// --- Box-drawing row builders ---

func cardTop() string {
	return "┌" + strings.Repeat("─", cardInner) + "┐"
}

func cardMid() string {
	return "├" + strings.Repeat("─", cardInner) + "┤"
}

func cardBot() string {
	return "└" + strings.Repeat("─", cardInner) + "┘"
}

// cardRow renders a left-aligned row padded to cardInner chars.
func cardRow(s string) string {
	return "│" + padRight(s, cardInner) + "│"
}

// cardRowRight renders a right-aligned row.
func cardRowRight(s string) string {
	s = safeRunes(s, cardInner)
	pad := cardInner - runeLen(s)
	if pad < 0 {
		pad = 0
	}
	return "│" + strings.Repeat(" ", pad) + s + "│"
}

// cardRowCenter renders a centered row.
func cardRowCenter(s string) string {
	s = safeRunes(s, cardInner)
	n := runeLen(s)
	total := cardInner - n
	left := total / 2
	right := total - left
	return "│" + strings.Repeat(" ", left) + s + strings.Repeat(" ", right) + "│"
}

// cardRowKV renders a key-value row: " KEY   VALUE " with key left, value right.
// Always has 1 leading space and 1 trailing space; key and value separated by ≥1 space.
func cardRowKV(key, val string) string {
	// Inner layout: " " + key + spaces + val + " " = 31 chars
	// So spaces = 29 - len(key) - len(val)
	k := []rune(key)
	v := []rune(val)
	gap := cardInner - 2 - len(k) - len(v)
	if gap < 1 {
		// Truncate value to fit
		maxV := cardInner - 2 - len(k) - 1
		if maxV < 0 {
			maxV = 0
		}
		v = []rune(safeRunes(val, maxV))
		gap = 1
	}
	content := " " + string(k) + strings.Repeat(" ", gap) + string(v) + " "
	return "│" + padRight(content, cardInner) + "│"
}

// cardRowEmpty renders a blank row.
func cardRowEmpty() string {
	return cardRow(strings.Repeat(" ", cardInner))
}

// --- Card data types ---

// QuoteCardData holds data for renderQuoteCardMono.
type QuoteCardData struct {
	FromTicker   string
	ToTicker     string
	AmountIn     string
	AmountOut    string
	AmountInUSD  string
	AmountOutUSD string
	Rate         string
	SpreadUSD    string
	SpreadPct    string
}

// DepositCardData holds data for renderDepositCardMono.
type DepositCardData struct {
	FromTicker string
	ToTicker   string
	AmountIn   string
	AmountOut  string
	Network    string
	Deadline   string // e.g. "59m remaining"
	RefundAddr string
	RecvAddr   string
}

// --- Card renderers (return plain string, no <pre> wrapping) ---

// renderSwapCardMono builds the monospace swap card string.
func renderSwapCardMono(sess *tgSession) string {
	var sb strings.Builder

	// Header
	sb.WriteString(cardTop() + "\n")
	sb.WriteString(cardRow(" Ø USWAP ZERO") + "\n")
	sb.WriteString(cardRow(" Zero fees · Non-custodial") + "\n")
	sb.WriteString(cardMid() + "\n")

	// SEND / RECEIVE token rows
	fromNet := networkDisplayName(sess.FromNet)
	toNet := networkDisplayName(sess.ToNet)

	fromTicker := safeRunes(sess.FromTicker, 8)
	toTicker := safeRunes(sess.ToTicker, 8)
	fromNetS := safeRunes(fromNet, 18)
	toNetS := safeRunes(toNet, 18)

	var sendVal string
	if sess.Amount != "" {
		sendVal = safeRunes(sess.Amount+" "+fromTicker+" / "+fromNetS, 24)
	} else {
		sendVal = safeRunes("─── "+fromTicker+" / "+fromNetS, 24)
	}
	recvVal := safeRunes("─── "+toTicker+" / "+toNetS, 24)

	sb.WriteString(cardRowKV("SEND", sendVal) + "\n")
	sb.WriteString(cardRowKV("RECEIVE", recvVal) + "\n")
	sb.WriteString(cardMid() + "\n")

	// Fields
	amount := sess.Amount
	if amount == "" {
		amount = "─── (not set)"
	}
	sb.WriteString(cardRowKV("AMOUNT", safeRunes(amount, 18)) + "\n")
	sb.WriteString(cardRowKV("SLIPPAGE", sess.Slippage+"%") + "\n")

	refund := "(not set)"
	if sess.RefundAddr != "" {
		refund = truncAddr(sess.RefundAddr) + " \u2713"
	}
	sb.WriteString(cardRowKV("REFUND", safeRunes(refund, 18)) + "\n")

	recv := "(not set)"
	if sess.RecvAddr != "" {
		recv = truncAddr(sess.RecvAddr) + " \u2713"
	}
	sb.WriteString(cardRowKV("RECEIVE ADDR", safeRunes(recv, 16)) + "\n")

	sb.WriteString(cardBot())
	return sb.String()
}

// renderQuoteCardMono builds the monospace quote card string.
func renderQuoteCardMono(p QuoteCardData) string {
	var sb strings.Builder

	sb.WriteString(cardTop() + "\n")
	sb.WriteString(cardRow(" Ø USWAP ZERO \u2014 QUOTE") + "\n")
	sb.WriteString(cardMid() + "\n")

	// SEND section
	fromTicker := safeRunes(p.FromTicker, 8)
	toTicker := safeRunes(p.ToTicker, 8)
	amtIn := safeRunes(trimAmount(p.AmountIn, 8), 16)
	amtOut := safeRunes(trimAmount(p.AmountOut, 8), 16)
	amtInUSD := safeRunes(p.AmountInUSD, 12)
	amtOutUSD := safeRunes(p.AmountOutUSD, 12)

	sb.WriteString(cardRowKV("SEND", amtIn+" "+fromTicker) + "\n")
	if p.AmountInUSD != "" {
		sb.WriteString(cardRowRight("~ "+amtInUSD+" ") + "\n")
	}
	sb.WriteString(cardRowCenter("\u2193") + "\n")

	// RECEIVE section
	sb.WriteString(cardRowKV("RECEIVE", "~ "+amtOut+" "+toTicker) + "\n")
	if p.AmountOutUSD != "" {
		sb.WriteString(cardRowRight("~ "+amtOutUSD+" ") + "\n")
	}
	sb.WriteString(cardMid() + "\n")

	// Rate
	if p.Rate != "" {
		rate := safeRunes(p.Rate, 24)
		sb.WriteString(cardRowKV("RATE", rate) + "\n")
	}
	sb.WriteString(cardMid() + "\n")

	// Fee breakdown
	sb.WriteString(cardRowKV("USWAP FEE", "\u00D8 (none)") + "\n")
	sb.WriteString(cardRowKV("PROTO FEE", "\u00D8 (none)") + "\n")

	if p.SpreadUSD != "" && p.SpreadPct != "" {
		spread := safeRunes("~ $"+p.SpreadUSD+" ("+p.SpreadPct+"%)", 18)
		sb.WriteString(cardRowKV("SPREAD", spread) + "\n")
	} else {
		sb.WriteString(cardRowKV("SPREAD", "\u00D8 (none)") + "\n")
	}
	sb.WriteString(cardMid() + "\n")

	sb.WriteString(cardRowKV("FEES CHARGED", "$0.00") + "\n")
	sb.WriteString(cardBot())
	return sb.String()
}

// renderDepositCardMono builds the monospace order/deposit card string (step 0).
// Note: deposit address is NOT included here — callers add it as a separate <code> block.
func renderDepositCardMono(p DepositCardData) string {
	var sb strings.Builder

	sb.WriteString(cardTop() + "\n")
	sb.WriteString(cardRow(" Ø USWAP ZERO \u2014 ORDER") + "\n")
	sb.WriteString(cardMid() + "\n")

	// Stepper at step 0 (awaiting deposit)
	sb.WriteString(cardRowCenter(stepperRow(0)) + "\n")
	sb.WriteString(cardRowCenter("Await    Proc.    Done") + "\n")
	sb.WriteString(cardMid() + "\n")

	// Swap summary
	fromT := safeRunes(p.FromTicker, 8)
	toT := safeRunes(p.ToTicker, 8)
	amtIn := safeRunes(trimAmount(p.AmountIn, 8), 12)
	amtOut := safeRunes(trimAmount(p.AmountOut, 8), 12)
	sb.WriteString(cardRowKV("SEND", amtIn+" "+fromT) + "\n")
	sb.WriteString(cardRowKV("RECEIVE", "~"+amtOut+" "+toT) + "\n")
	sb.WriteString(cardMid() + "\n")

	// Network + deadline
	network := safeRunes(p.Network, 18)
	sb.WriteString(cardRowKV("NETWORK", network) + "\n")
	if p.Deadline != "" {
		deadline := safeRunes(p.Deadline, 18)
		sb.WriteString(cardRowKV("DEADLINE", deadline) + "\n")
	}

	// Addresses (truncated)
	if p.RefundAddr != "" || p.RecvAddr != "" {
		sb.WriteString(cardMid() + "\n")
		if p.RefundAddr != "" {
			sb.WriteString(cardRowKV("REFUND", safeRunes(truncAddr(p.RefundAddr), 16)) + "\n")
		}
		if p.RecvAddr != "" {
			sb.WriteString(cardRowKV("RECEIVE", safeRunes(truncAddr(p.RecvAddr), 16)) + "\n")
		}
	}

	sb.WriteString(cardBot())
	return sb.String()
}

// stepperRow returns the stepper ASCII art for a given step (0=pending, 1=processing, 2=complete).
func stepperRow(step int) string {
	// Nodes: 0=Await, 1=Process, 2=Done
	nodes := make([]string, 3)
	for i := range nodes {
		switch {
		case i < step:
			nodes[i] = "[\u2713]" // completed
		case i == step:
			nodes[i] = "[\u25CF]" // current (●)
		default:
			nodes[i] = "[\u25CB]" // pending (○)
		}
	}
	return nodes[0] + "\u2500\u2500\u2500\u2500" + nodes[1] + "\u2500\u2500\u2500\u2500" + nodes[2]
}

// renderStatusCardMono builds the monospace status card string.
func renderStatusCardMono(order *OrderData, status *StatusResponse) string {
	var sb strings.Builder

	sb.WriteString(cardTop() + "\n")
	sb.WriteString(cardRow(" Ø USWAP ZERO \u2014 STATUS") + "\n")
	sb.WriteString(cardMid() + "\n")

	var step int
	switch strings.ToUpper(status.Status) {
	case "PENDING_DEPOSIT", "KNOWN_DEPOSIT_TX":
		step = 0
	case "PROCESSING":
		step = 1
	default:
		step = 0
	}

	sb.WriteString(cardRowCenter(stepperRow(step)) + "\n")
	sb.WriteString(cardRowCenter("Await    Proc.    Done") + "\n")
	sb.WriteString(cardMid() + "\n")

	fromTicker := safeRunes(order.FromTicker, 8)
	toTicker := safeRunes(order.ToTicker, 8)
	amtIn := safeRunes(order.AmountIn, 10)
	amtOut := safeRunes(order.AmountOut, 10)

	swapLine := safeRunes(amtIn+" "+fromTicker+" \u2192 "+amtOut+" "+toTicker, cardInner-2)
	sb.WriteString(cardRow(" "+swapLine) + "\n")
	sb.WriteString(cardBot())
	return sb.String()
}

// renderCompletionCardMono builds the monospace completion card string.
func renderCompletionCardMono(order *OrderData, status *StatusResponse) string {
	var sb strings.Builder

	sb.WriteString(cardTop() + "\n")
	sb.WriteString(cardRow(" Ø USWAP ZERO \u2014 COMPLETE \u2713") + "\n")
	sb.WriteString(cardMid() + "\n")

	sb.WriteString(cardRowCenter(stepperRow(3)) + "\n")
	sb.WriteString(cardMid() + "\n")

	fromTicker := safeRunes(order.FromTicker, 8)
	toTicker := safeRunes(order.ToTicker, 8)
	amtIn := safeRunes(order.AmountIn, 14)
	sb.WriteString(cardRowKV("SENT", amtIn+" "+fromTicker) + "\n")

	// Use actual received amount if available
	amtOut := order.AmountOut
	if status.SwapDetails != nil && status.SwapDetails.AmountOutFmt != "" {
		amtOut = status.SwapDetails.AmountOutFmt
	}
	amtOutS := safeRunes(amtOut, 14)
	sb.WriteString(cardRowKV("RECEIVED", amtOutS+" "+toTicker) + "\n")
	sb.WriteString(cardMid() + "\n")

	sb.WriteString(cardRowKV("FEES CHARGED", "\u00D8 (zero)") + "\n")
	sb.WriteString(cardBot())
	return sb.String()
}

// renderRefundCardMono builds the monospace refund card string.
func renderRefundCardMono(order *OrderData, status *StatusResponse) string {
	var sb strings.Builder

	sb.WriteString(cardTop() + "\n")
	sb.WriteString(cardRow(" Ø USWAP ZERO \u2014 REFUNDED") + "\n")
	sb.WriteString(cardMid() + "\n")

	fromTicker := safeRunes(order.FromTicker, 8)
	toTicker := safeRunes(order.ToTicker, 8)
	amtIn := safeRunes(order.AmountIn, 14)

	sb.WriteString(cardRowKV("SENT", amtIn+" "+fromTicker) + "\n")
	sb.WriteString(cardRowKV("SWAP TO", toTicker) + "\n")

	if status.SwapDetails != nil && status.SwapDetails.RefundReason != "" {
		reason := safeRunes(status.SwapDetails.RefundReason, 20)
		sb.WriteString(cardMid() + "\n")
		sb.WriteString(cardRowKV("REASON", reason) + "\n")
	}

	sb.WriteString(cardBot())
	return sb.String()
}

// renderFailedCardMono builds the monospace failed card string.
func renderFailedCardMono(order *OrderData, status *StatusResponse) string {
	var sb strings.Builder

	sb.WriteString(cardTop() + "\n")
	sb.WriteString(cardRow(" Ø USWAP ZERO \u2014 FAILED") + "\n")
	sb.WriteString(cardMid() + "\n")

	fromTicker := safeRunes(order.FromTicker, 8)
	toTicker := safeRunes(order.ToTicker, 8)
	amtIn := safeRunes(order.AmountIn, 14)

	sb.WriteString(cardRowKV("SENT", amtIn+" "+fromTicker) + "\n")
	sb.WriteString(cardRowKV("SWAP TO", toTicker) + "\n")

	if status.SwapDetails != nil && status.SwapDetails.RefundReason != "" {
		reason := safeRunes(status.SwapDetails.RefundReason, 20)
		sb.WriteString(cardMid() + "\n")
		sb.WriteString(cardRowKV("REASON", reason) + "\n")
	}

	sb.WriteString(cardBot())
	return sb.String()
}

// renderAnyStatusCard dispatches to the correct card renderer based on status.
// API status values: PENDING_DEPOSIT, KNOWN_DEPOSIT_TX, INCOMPLETE_DEPOSIT,
// PROCESSING, SUCCESS, REFUNDED, FAILED
func renderAnyStatusCard(order *OrderData, status *StatusResponse) string {
	switch strings.ToUpper(status.Status) {
	case "SUCCESS":
		return renderCompletionCardMono(order, status)
	case "REFUNDED":
		return renderRefundCardMono(order, status)
	case "FAILED", "INCOMPLETE_DEPOSIT":
		return renderFailedCardMono(order, status)
	default:
		return renderStatusCardMono(order, status)
	}
}

// deadlineString returns a human-readable deadline remaining string.
func deadlineString(deadlineRFC3339 string) string {
	if deadlineRFC3339 == "" {
		return "60m remaining"
	}
	dl, err := time.Parse(time.RFC3339, deadlineRFC3339)
	if err != nil {
		return "60m remaining"
	}
	remaining := time.Until(dl)
	if remaining <= 0 {
		return "expired"
	}
	h := int(remaining.Hours())
	m := int(remaining.Minutes()) % 60
	if h > 0 {
		if m > 0 {
			return fmt.Sprintf("%dh %dm remaining", h, m)
		}
		return fmt.Sprintf("%dh remaining", h)
	}
	return fmt.Sprintf("%dm remaining", int(remaining.Minutes()))
}
