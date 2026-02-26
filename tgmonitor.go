package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// --- Double-border card helpers (W=33, inner=31) ---

func cardTopD() string { return "╔" + strings.Repeat("═", cardInner) + "╗" }
func cardMidD() string { return "╠" + strings.Repeat("═", cardInner) + "╣" }
func cardBotD() string { return "╚" + strings.Repeat("═", cardInner) + "╝" }

func cardRowD(s string) string {
	return "║" + padRight(s, cardInner) + "║"
}

// cardRowDLR renders a row with left and right content, padding in between.
func cardRowDLR(left, right string) string {
	l := []rune(left)
	r := []rune(right)
	space := cardInner - len(l) - len(r)
	if space < 1 {
		space = 1
	}
	return "║" + string(l) + strings.Repeat(" ", space) + string(r) + "║"
}

// renderMonitorCard builds the double-border card for one reseller transaction.
func renderMonitorCard(resellerName string, tx ExplorerTx, feeUSD float64, stats *LiveStats) string {
	// Header: " SWAP.MY        $18.42 PROFIT "
	feeStr := formatUSD(feeUSD) + " PROFIT"
	header := cardRowDLR(" "+resellerName, feeStr+" ")

	// Amounts: " 0.50 ETH  ──►  1,842 USDT "
	inTok := safeRunes(txTokenLabel(tx.OriginAsset), 6)
	outTok := safeRunes(txTokenLabel(tx.DestinationAsset), 6)
	inAmt := safeRunes(trimAmount(tx.AmountInFormatted, 6), 10)
	outAmt := safeRunes(trimAmount(tx.AmountOutFormatted, 6), 10)
	amtRow := cardRowD(" " + inAmt + " " + inTok + "  ──►  " + outAmt + " " + outTok)

	// Chains: " Ethereum        TRON "
	inChain := safeRunes(txChainLabel(tx.OriginAsset), 12)
	outChain := safeRunes(txChainLabel(tx.DestinationAsset), 12)
	chainRow := cardRowDLR(" "+inChain, outChain+" ")

	// Footer: " #5,214  ·  26 Feb  ·  02:41z "
	_, _, swaps := stats.snapshot()
	swapNum := formatCommas(int64(swaps))
	ts := monitorFormatTime(tx.CreatedAtTimestamp)
	footerRow := cardRowD(" #" + safeRunes(swapNum, 7) + "  ·  " + ts)

	var sb strings.Builder
	sb.WriteString(cardTopD() + "\n")
	sb.WriteString(header + "\n")
	sb.WriteString(cardMidD() + "\n")
	sb.WriteString(amtRow + "\n")
	sb.WriteString(chainRow + "\n")
	sb.WriteString(cardMidD() + "\n")
	sb.WriteString(footerRow + "\n")
	sb.WriteString(cardBotD())
	return sb.String()
}

// monitorFormatTime formats a unix timestamp as "26 Feb · 02:41z".
func monitorFormatTime(ts int64) string {
	if ts == 0 {
		return "unknown"
	}
	t := time.Unix(ts, 0).UTC()
	return fmt.Sprintf("%d %s · %02d:%02dz",
		t.Day(), t.Month().String()[:3], t.Hour(), t.Minute())
}

// postMonitorCard posts a fee card + addresses/tx hashes to a Telegram thread.
func postMonitorCard(groupID, threadID int64, resellerName string, tx ExplorerTx, feeUSD float64, stats *LiveStats) {
	card := renderMonitorCard(resellerName, tx, feeUSD, stats)

	// Build the full message: card + addresses + tx hashes
	var sb strings.Builder
	sb.WriteString("<pre>" + card + "</pre>")

	// Sender and recipient
	if len(tx.Senders) > 0 && tx.Senders[0] != "" {
		sb.WriteString("\nFrom: <code>" + tx.Senders[0] + "</code>")
	}
	if tx.Recipient != "" {
		sb.WriteString("\nTo:   <code>" + tx.Recipient + "</code>")
	}

	// Origin chain tx
	if len(tx.OriginChainTxHashes) > 0 && tx.OriginChainTxHashes[0] != "" {
		sb.WriteString("\n\nSRC:  <code>" + tx.OriginChainTxHashes[0] + "</code>")
	}
	// Destination chain tx
	if len(tx.DestinationChainTxHashes) > 0 && tx.DestinationChainTxHashes[0] != "" {
		sb.WriteString("\nDST:  <code>" + tx.DestinationChainTxHashes[0] + "</code>")
	}
	// NEAR tx — hyperlinked
	if len(tx.NearTxHashes) > 0 && tx.NearTxHashes[0] != "" {
		hash := tx.NearTxHashes[0]
		sb.WriteString("\nNEAR: <a href=\"https://nearblocks.io/txns/" + hash + "\">" + hash + "</a>")
	}

	payload := map[string]interface{}{
		"chat_id":           groupID,
		"message_thread_id": threadID,
		"text":              sb.String(),
		"parse_mode":        "HTML",
		"link_preview_options": map[string]bool{"is_disabled": true},
	}
	if _, err := tgRequest("sendMessage", payload); err != nil {
		// Don't log every error during backfill to avoid spam
		_ = err
	}
}

// updateMonitorThreadTitle updates the forum topic title with the current profit total.
// Title format: "$24,210 Profit · Swap.my"
func updateMonitorThreadTitle(groupID, threadID int64, resellerDisplay string, totalFeeUSD float64) {
	title := formatUSD(totalFeeUSD) + " Profit · " + resellerDisplay
	payload := map[string]interface{}{
		"chat_id":           groupID,
		"message_thread_id": threadID,
		"name":              title,
	}
	tgRequest("editForumTopic", payload)
}

// updateMainChatDescription updates the main chat description, replacing $ with the total.
func updateMainChatDescription() {
	if monitorMainChatID == 0 || tgBotToken == "" {
		return
	}

	// Get current description
	result, err := tgRequest("getChat", map[string]interface{}{
		"chat_id": monitorMainChatID,
	})
	if err != nil {
		return
	}

	var chatInfo struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(result, &chatInfo); err != nil || chatInfo.Description == "" {
		return
	}

	if !strings.Contains(chatInfo.Description, "$") {
		return
	}

	total := monitorTotalFeeUSD()
	newDesc := strings.Replace(chatInfo.Description, "$", formatUSD(total), 1)

	tgRequest("setChatDescription", map[string]interface{}{
		"chat_id":     monitorMainChatID,
		"description": newDesc,
	})
}
