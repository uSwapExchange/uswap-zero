package main

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Build metadata — injected via -ldflags at compile time
var (
	commitHash  = "development"
	buildTime   = "unknown"
	buildLogURL = ""
)

//go:embed templates/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

//go:embed data/near_intents_reseller_analysis.json
var analysisJSON []byte

var templates *template.Template

// iconPath returns the URL path for a server-generated token icon.
func iconPath(ticker string) string {
	return "/icons/gen/" + strings.ToUpper(ticker)
}

// Rate limiter — basic in-memory counter per IP prefix
type rateLimiter struct {
	mu       sync.Mutex
	counters map[string]*rateBucket
}

type rateBucket struct {
	count   int
	resetAt time.Time
}

var limiter = &rateLimiter{counters: make(map[string]*rateBucket)}

func (rl *rateLimiter) allow(ip string, limit int, window time.Duration) bool {
	// Use /24 prefix for IPv4
	prefix := ip
	if idx := strings.LastIndex(ip, "."); idx > 0 {
		prefix = ip[:idx]
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, ok := rl.counters[prefix]
	now := time.Now()
	if !ok || now.After(bucket.resetAt) {
		rl.counters[prefix] = &rateBucket{count: 1, resetAt: now.Add(window)}
		return true
	}
	bucket.count++
	return bucket.count <= limit
}

// Clean up expired buckets periodically
func (rl *rateLimiter) startCleanup() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			rl.mu.Lock()
			now := time.Now()
			for k, v := range rl.counters {
				if now.After(v.resetAt) {
					delete(rl.counters, k)
				}
			}
			rl.mu.Unlock()
		}
	}()
}

func initTemplates() {
	funcMap := template.FuncMap{
		"iconPath": iconPath,
		"formatUSD": func(price float64) string {
			return formatUSD(price)
		},
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i
			}
			return s
		},
		"truncAddr": func(addr string) string {
			if len(addr) <= 16 {
				return addr
			}
			return addr[:8] + "..." + addr[len(addr)-6:]
		},
	}

	var err error
	templates, err = template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		log.Fatal("Failed to parse templates:", err)
	}
}

func main() {
	initCrypto()
	initNearIntents()
	initTemplates()
	initCaseStudy()
	startCacheRefresher()
	limiter.startCleanup()

	mux := http.NewServeMux()

	// Static files
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Generated token icons
	mux.HandleFunc("/icons/gen/", handleGenIcon)

	// Pages
	mux.HandleFunc("/", handleSwap)
	mux.HandleFunc("/quote", handleQuote)
	mux.HandleFunc("/swap", handleSwapConfirm)
	mux.HandleFunc("/order/", handleOrder)
	mux.HandleFunc("/currencies", handleCurrencies)
	mux.HandleFunc("/how-it-works", handleHowItWorks)
	mux.HandleFunc("/case-study", handleCaseStudy)
	mux.HandleFunc("/verify", handleVerify)
	mux.HandleFunc("/source", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://github.com/uSwapExchange/zero", http.StatusFound)
	})

	// Telegram bot (optional — disabled if TG_BOT_TOKEN is unset)
	if initTelegramBot() {
		mux.HandleFunc("/tg/webhook/"+tgWebhookSecret, handleTelegramWebhook)
		tgSessions.startCleanup()
		log.Printf("Telegram bot enabled")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Printf("uSwap Zero starting on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}
