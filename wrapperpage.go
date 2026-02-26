package main

import (
	"net/http"
	"strings"
	"time"
)

// WrapperLogsPageData is the template data for /wrapper-logs.
type WrapperLogsPageData struct {
	PageData
	Entries        []WrapperLogRow
	TotalFeeUSD    string
	Resellers      []WrapperResellerStat
	Query          string
	FilterReseller string
	Count          int
	MonitorActive  bool
}

// WrapperResellerStat holds display stats for one reseller.
type WrapperResellerStat struct {
	Name      string
	FeeUSD    string
	VolumeUSD string
	Swaps     string
}

// WrapperLogRow is one row in the log table.
type WrapperLogRow struct {
	Reseller   string
	AmountIn   string
	TokenIn    string
	ChainIn    string
	AmountOut  string
	TokenOut   string
	ChainOut   string
	FeeUSD     string
	Timestamp  string
	Sender     string
	Recipient  string
	NearTxHash string
	NearTxURL  string
}

func handleWrapperLogs(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	filterReseller := r.URL.Query().Get("reseller")

	// Build filter function
	filter := func(e LogEntry) bool {
		if filterReseller != "" && !strings.EqualFold(e.Reseller, filterReseller) {
			return false
		}
		if query != "" {
			q := strings.ToLower(query)
			tx := e.Tx
			if !strings.Contains(strings.ToLower(tx.Recipient), q) &&
				!strings.Contains(strings.ToLower(tx.DepositAddress), q) &&
				!strings.Contains(strings.ToLower(txTokenLabel(tx.OriginAsset)), q) &&
				!strings.Contains(strings.ToLower(txTokenLabel(tx.DestinationAsset)), q) &&
				!strings.Contains(strings.ToLower(e.Reseller), q) {
				hasHash := false
				for _, h := range tx.NearTxHashes {
					if strings.Contains(strings.ToLower(h), q) {
						hasHash = true
						break
					}
				}
				if !hasHash {
					return false
				}
			}
		}
		return true
	}

	entries := monitorLogBuf.snapshot(500, filter)

	var rows []WrapperLogRow
	for _, e := range entries {
		tx := e.Tx
		var nearHash, nearURL string
		if len(tx.NearTxHashes) > 0 {
			nearHash = tx.NearTxHashes[0]
			nearURL = "https://nearblocks.io/txns/" + nearHash
		}
		var sender string
		if len(tx.Senders) > 0 {
			sender = tx.Senders[0]
		}

		rows = append(rows, WrapperLogRow{
			Reseller:   e.Reseller,
			AmountIn:   trimAmount(tx.AmountInFormatted, 6),
			TokenIn:    txTokenLabel(tx.OriginAsset),
			ChainIn:    txChainLabel(tx.OriginAsset),
			AmountOut:  trimAmount(tx.AmountOutFormatted, 6),
			TokenOut:   txTokenLabel(tx.DestinationAsset),
			ChainOut:   txChainLabel(tx.DestinationAsset),
			FeeUSD:     formatUSD(e.FeeUSD),
			Timestamp:  formatLogTime(e.Tx.CreatedAtTimestamp),
			Sender:     sender,
			Recipient:  tx.Recipient,
			NearTxHash: nearHash,
			NearTxURL:  nearURL,
		})
	}

	// Build per-reseller stats
	var resellerStats []WrapperResellerStat
	for _, r := range monitorResellers {
		if s, ok := monitorStats[r.Affiliate]; ok {
			fee, vol, swaps := s.snapshot()
			resellerStats = append(resellerStats, WrapperResellerStat{
				Name:      r.Name,
				FeeUSD:    formatUSD(fee),
				VolumeUSD: formatUSD(vol),
				Swaps:     formatCommas(int64(swaps)),
			})
		}
	}

	data := WrapperLogsPageData{
		PageData:       newPageData("Wrapper Logs"),
		Entries:        rows,
		TotalFeeUSD:    formatUSD(monitorTotalFeeUSD()),
		Resellers:      resellerStats,
		Query:          query,
		FilterReseller: filterReseller,
		Count:          len(rows),
		MonitorActive:  monitorEnabled,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "wrapper_logs.html", data)
}

func formatLogTime(ts int64) string {
	if ts == 0 {
		return "—"
	}
	return time.Unix(ts, 0).UTC().Format("02 Jan 2006 15:04z")
}
