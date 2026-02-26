package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// monitorReseller describes one tracked reseller.
type monitorReseller struct {
	Name      string // "SWAP.MY", "EAGLESWAP", "LIZARDSWAP"
	Affiliate string
	ThreadID  int64
}

// LiveStats holds running totals for a reseller (mutex-protected).
type LiveStats struct {
	mu        sync.RWMutex
	FeeUSD    float64
	VolumeUSD float64
	SwapCount int
}

func (s *LiveStats) add(feeUSD, volumeUSD float64) {
	s.mu.Lock()
	s.FeeUSD += feeUSD
	s.VolumeUSD += volumeUSD
	s.SwapCount++
	s.mu.Unlock()
}

func (s *LiveStats) snapshot() (feeUSD, volumeUSD float64, swaps int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.FeeUSD, s.VolumeUSD, s.SwapCount
}

// LogEntry is one transaction in the in-memory ring buffer.
type LogEntry struct {
	Reseller  string
	Affiliate string
	Tx        ExplorerTx
	FeeUSD    float64
	PostedAt  time.Time
}

const logRingSize = 2000

type ringBuffer struct {
	mu      sync.RWMutex
	entries []LogEntry
}

func (rb *ringBuffer) add(e LogEntry) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.entries = append([]LogEntry{e}, rb.entries...)
	if len(rb.entries) > logRingSize {
		rb.entries = rb.entries[:logRingSize]
	}
}

func (rb *ringBuffer) snapshot(limit int, filter func(LogEntry) bool) []LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	var result []LogEntry
	for _, e := range rb.entries {
		if filter == nil || filter(e) {
			result = append(result, e)
		}
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

// monitorCursor persists the pagination position per affiliate.
type monitorCursor struct {
	LastAddr string `json:"lastAddr"`
	LastMemo string `json:"lastMemo"`
}

type cursorFile struct {
	Cursors map[string]monitorCursor `json:"cursors"`
}

// Global monitor state.
var (
	monitorResellers  []monitorReseller
	monitorStats      = map[string]*LiveStats{} // keyed by affiliate
	monitorStatsMu    sync.RWMutex
	monitorLogBuf     ringBuffer
	monitorCursorPath = "data/monitor_state.json"
	monitorMainChatID int64
	monitorEnabled    bool

	serverStartTime = time.Now()
	requestCounter  int64
)

// initMonitor reads env vars and starts the polling goroutine.
// Returns true if monitor is enabled.
func initMonitor() bool {
	groupID := envInt64("TG_MONITOR_GROUP_ID")
	if groupID == 0 {
		return false
	}

	monitorMainChatID = envInt64("TG_MAIN_CHAT_ID")

	var raw rawAnalysis
	if err := json.Unmarshal(analysisJSON, &raw); err != nil {
		log.Printf("monitor: parse analysis JSON: %v", err)
		return false
	}

	monitorResellers = []monitorReseller{
		{Name: "SWAP.MY", Affiliate: "swapmybuddy.near", ThreadID: envInt64("TG_SWAPMY_THREAD_ID")},
		{Name: "EAGLESWAP", Affiliate: "Gcj5A3a5mF2BEPm4LujddTit7tTR8pNmUKXkcuzM4dC1", ThreadID: envInt64("TG_EAGLESWAP_THREAD_ID")},
		{Name: "LIZARDSWAP", Affiliate: "trustswap.near", ThreadID: envInt64("TG_LIZARDSWAP_THREAD_ID")},
	}

	// Seed live stats from static JSON so totals are correct from startup.
	monitorStats["swapmybuddy.near"] = &LiveStats{FeeUSD: raw.SwapMy.TotalRevenueUSD, VolumeUSD: raw.SwapMy.TotalVolumeUSD, SwapCount: raw.SwapMy.TotalSwaps}
	monitorStats["Gcj5A3a5mF2BEPm4LujddTit7tTR8pNmUKXkcuzM4dC1"] = &LiveStats{FeeUSD: raw.EagleSwap.TotalRevenueUSD, VolumeUSD: raw.EagleSwap.TotalVolumeUSD, SwapCount: raw.EagleSwap.TotalSwaps}
	monitorStats["trustswap.near"] = &LiveStats{FeeUSD: raw.LizardSwap.TotalRevenueUSD, VolumeUSD: raw.LizardSwap.TotalVolumeUSD, SwapCount: raw.LizardSwap.TotalSwaps}

	initExplorerRateLimiter()
	monitorEnabled = true
	go runMonitor(groupID)
	return true
}

func runMonitor(groupID int64) {
	cursors := loadCursors()
	for i, r := range monitorResellers {
		time.Sleep(time.Duration(i) * 6 * time.Second)
		go runResellerPoller(groupID, r, cursors)
	}
}

func runResellerPoller(groupID int64, r monitorReseller, cursors cursorFile) {
	cursor := cursors.Cursors[r.Affiliate]
	titleCounter := 0
	log.Printf("monitor: poller started for %s", r.Name)

	for {
		txs, err := fetchExplorerTxs(r.Affiliate, cursor.LastAddr, cursor.LastMemo, 100)
		if err != nil {
			log.Printf("monitor: fetch %s: %v", r.Name, err)
			time.Sleep(30 * time.Second)
			continue
		}

		for _, tx := range txs {
			fee := txFeeUSD(tx)
			inUsd, _ := strconv.ParseFloat(strings.TrimSpace(tx.AmountInUsd), 64)

			monitorLogBuf.add(LogEntry{
				Reseller:  r.Name,
				Affiliate: r.Affiliate,
				Tx:        tx,
				FeeUSD:    fee,
				PostedAt:  time.Now(),
			})

			monitorStats[r.Affiliate].add(fee, inUsd)

			if r.ThreadID != 0 && tgBotToken != "" {
				postMonitorCard(groupID, r.ThreadID, r.Name, tx, fee, monitorStats[r.Affiliate])
				time.Sleep(200 * time.Millisecond)
			}

			cursor.LastAddr = tx.DepositAddress
			cursor.LastMemo = tx.DepositMemo
			titleCounter++
		}

		if len(txs) > 0 {
			saveCursor(r.Affiliate, cursor)
			if titleCounter >= 10 {
				if r.ThreadID != 0 && tgBotToken != "" {
					fee, _, _ := monitorStats[r.Affiliate].snapshot()
					updateMonitorThreadTitle(groupID, r.ThreadID, r.Name, fee)
				}
				titleCounter = 0
			}
			updateMainChatDescription()
		}

		time.Sleep(15 * time.Second)
	}
}

func loadCursors() cursorFile {
	var cf cursorFile
	cf.Cursors = make(map[string]monitorCursor)
	data, err := os.ReadFile(monitorCursorPath)
	if err != nil {
		return cf
	}
	json.Unmarshal(data, &cf)
	return cf
}

func saveCursor(affiliate string, cursor monitorCursor) {
	cf := loadCursors()
	cf.Cursors[affiliate] = cursor
	data, _ := json.Marshal(cf)
	os.WriteFile(monitorCursorPath, data, 0600)
}

// monitorTotalFeeUSD returns the sum of fees across all tracked resellers.
func monitorTotalFeeUSD() float64 {
	if !monitorEnabled {
		return 0
	}
	var total float64
	for _, s := range monitorStats {
		f, _, _ := s.snapshot()
		total += f
	}
	return total
}

// envInt64 reads an env var as int64; returns 0 if unset or invalid.
func envInt64(key string) int64 {
	v := os.Getenv(key)
	if v == "" {
		return 0
	}
	var n int64
	fmt.Sscanf(v, "%d", &n)
	return n
}

// incrementRequests is called by the HTTP middleware to track request count.
func incrementRequests() {
	atomic.AddInt64(&requestCounter, 1)
}
