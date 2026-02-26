package main

import (
	"net/http"
	"net/url"
	"sort"
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
	SortBy         string
	SortDir        string
	Count          int
	MonitorActive  bool
	// Pre-built sort toggle URLs for column headers
	SortFeeURL  string
	SortDateURL string
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
	sortBy := r.URL.Query().Get("sort")   // "fee" or "date"
	sortDir := r.URL.Query().Get("dir")   // "asc" or "desc"

	if sortBy != "fee" && sortBy != "date" {
		sortBy = "date"
	}
	if sortDir != "asc" && sortDir != "desc" {
		sortDir = "desc"
	}

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

	// Sort entries server-side
	sort.SliceStable(entries, func(i, j int) bool {
		var less bool
		if sortBy == "fee" {
			less = entries[i].FeeUSD < entries[j].FeeUSD
		} else {
			less = entries[i].Tx.CreatedAtTimestamp < entries[j].Tx.CreatedAtTimestamp
		}
		if sortDir == "desc" {
			return !less
		}
		return less
	})

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
	for _, res := range monitorResellers {
		if s, ok := monitorStats[res.Affiliate]; ok {
			fee, vol, swaps := s.snapshot()
			resellerStats = append(resellerStats, WrapperResellerStat{
				Name:      res.Name,
				FeeUSD:    formatUSD(fee),
				VolumeUSD: formatUSD(vol),
				Swaps:     formatCommas(int64(swaps)),
			})
		}
	}

	// Build sort toggle URLs — clicking a sorted column reverses direction
	sortFeeURL := sortToggleURL(query, filterReseller, "fee", sortBy, sortDir)
	sortDateURL := sortToggleURL(query, filterReseller, "date", sortBy, sortDir)

	pd := newPageData("Wrapper Logs")
	pd.MetaRefresh = 60
	data := WrapperLogsPageData{
		PageData:       pd,
		Entries:        rows,
		TotalFeeUSD:    formatUSD(monitorTotalFeeUSD()),
		Resellers:      resellerStats,
		Query:          query,
		FilterReseller: filterReseller,
		SortBy:         sortBy,
		SortDir:        sortDir,
		Count:          len(rows),
		MonitorActive:  monitorEnabled,
		SortFeeURL:     sortFeeURL,
		SortDateURL:    sortDateURL,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	templates.ExecuteTemplate(w, "wrapper_logs.html", data)
}

// sortToggleURL builds a /wrapper-logs URL that toggles the sort direction
// for the given column, preserving existing query and reseller filter params.
func sortToggleURL(query, filterReseller, column, currentSort, currentDir string) string {
	dir := "desc"
	if currentSort == column && currentDir == "desc" {
		dir = "asc"
	}
	params := url.Values{}
	if query != "" {
		params.Set("q", query)
	}
	if filterReseller != "" {
		params.Set("reseller", filterReseller)
	}
	params.Set("sort", column)
	params.Set("dir", dir)
	return "/wrapper-logs?" + params.Encode()
}

// sortIndicator returns an arrow for active sort columns.
func sortIndicator(sortBy, column, sortDir string) string {
	if sortBy != column {
		return ""
	}
	if sortDir == "asc" {
		return " ↑"
	}
	return " ↓"
}

func formatLogTime(ts int64) string {
	if ts == 0 {
		return "—"
	}
	return time.Unix(ts, 0).UTC().Format("02 Jan 2006 15:04z")
}

