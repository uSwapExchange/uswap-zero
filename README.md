# uSwap Zero

Zero-fee, zero-tracking, open-source crypto swap frontend powered by [NEAR Intents](https://near.org/intents).

**Live:** [zero.uswap.net](https://zero.uswap.net)

## What is this?

A single Go binary (~4000 lines, zero dependencies) that lets you swap 140+ tokens across 29 blockchains — via web or Telegram bot. No account needed. No JavaScript analytics. No cookies. No server-side logging of user data.

uSwap Zero passes the NEAR Intents exchange rate through at cost — no markup, no hidden fees. Every swap is verifiable against the public NEAR Intents API.

## Why?

Most "zero-fee" swap services are resellers. They use the NEAR Intents API, add 1-5% to the rate, and pocket the difference. The user never sees the pre-markup price.

uSwap Zero is different:
- **Zero markup** — the API call passes amounts through untouched
- **Open source** — read every line of code that handles your swap
- **Verifiable deployment** — the running binary's commit hash, build log, and image digest are public at `/verify`
- **No tracking** — no analytics, no cookies, no IP logging, no session storage

See the full analysis at [/case-study](https://zero.uswap.net/case-study).

## Tech Stack

```
Language:     Go 1.23 (stdlib only)
HTTP:         net/http
Templating:   html/template (auto-escapes HTML)
Encryption:   crypto/aes + crypto/cipher (AES-256-GCM)
Static:       embed directive (CSS + icons in binary)
Container:    FROM scratch (empty image + binary + TLS certs)
Dependencies: 0
```

## Run Locally

```bash
git clone https://github.com/uSwapExchange/zero.git
cd zero

# Generate a random encryption key and start the server
ORDER_SECRET=$(openssl rand -hex 32) go run .
```

Open http://localhost:3000.

## Telegram Bot

The bot is optional. When `TG_BOT_TOKEN` and `TG_APP_URL` are set, the server auto-registers a webhook and the bot becomes active. If either is unset, the web interface still works normally.

The bot renders everything as monospace `<pre>` cards — no images, no external services. QR codes for deposit addresses are generated server-side (stdlib only) and sent as photo messages with a dark frame.

Try it: [@uSwapZero_Bot](https://t.me/uSwapZero_Bot)

## Build

```bash
# Binary
go build -o zero .

# Docker
docker build -t zero .
docker run -p 3000:3000 zero
```

Build with deployment metadata:

```bash
go build -ldflags "-s -w \
  -X main.commitHash=$(git rev-parse HEAD) \
  -X main.buildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
  -X main.buildLogURL=https://github.com/uSwapExchange/zero/actions/runs/12345" \
  -o zero .
```

## Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `ORDER_SECRET` | Production | Random on startup | 64-char hex key for AES-256-GCM encryption of order tokens |
| `NEAR_INTENTS_JWT` | No | Empty | JWT from NEAR Intents partners portal (enables 0% protocol fee) |
| `NEAR_INTENTS_API_URL` | No | `https://1click.chaindefuser.com` | NEAR Intents API base URL |
| `PORT` | No | `3000` | HTTP listen port |
| `TG_BOT_TOKEN` | No | — | Telegram bot token from @BotFather — enables the Telegram bot |
| `TG_APP_URL` | No | — | Public base URL of the deployment (e.g. `https://zero.uswap.net`) |
| `TG_WEBHOOK_SECRET` | No | Auto-generated | Secret for verifying Telegram webhook requests |

See `.env.example` for a complete reference.

## Project Structure

```
zero/
├── main.go           # Server, routes, templates, rate limiter
├── handlers.go       # HTTP handlers for all pages
├── nearintents.go    # NEAR Intents 1Click API client
├── tokencache.go     # In-memory token cache (5min TTL)
├── crypto.go         # AES-256-GCM encrypt/decrypt + CSRF tokens
├── qr.go             # QR code SVG generator (hand-rolled, no deps)
├── amount.go         # BigInt amount math (human <-> atomic)
├── tgbot.go          # Telegram bot init, webhook registration
├── tghandler.go      # Telegram update router + command handlers
├── tgorder.go        # Telegram swap flow (quote → confirm → order → status)
├── tgrender.go       # Monospace card renderers (<pre> box-drawing)
├── tgqr.go           # Dark-framed QR PNG generator for deposit step
├── tgsession.go      # Per-user session state
├── tgswapcard.go     # Swap card builder + inline keyboard
├── templates/        # Go html/template files
├── static/style.css  # Single stylesheet
├── static/icons/     # 30 bundled SVG crypto icons
└── Dockerfile        # Multi-stage: golang:1.23-alpine -> FROM scratch
```

## Routes

| Method | Path | Description |
|---|---|---|
| GET | `/` | Swap form with currency selector modal |
| POST | `/quote` | Quote preview with fee breakdown |
| POST | `/swap` | Confirm swap, create order, redirect to `/order/{token}` |
| GET | `/order/{token}` | Order status with deposit address + QR code |
| GET | `/order/{token}/raw` | Raw JSON status from NEAR Intents API |
| GET | `/currencies` | Full searchable token list (140+ tokens, 29 networks) |
| GET | `/how-it-works` | How the swap process works |
| GET | `/case-study` | Analysis of swap service reseller markup practices |
| GET | `/verify` | Deployment metadata, build verification instructions |
| GET | `/source` | Redirect to GitHub repository |
| GET | `/static/*` | Embedded CSS and SVG icons |
| GET | `/icons/gen/{ticker}` | Server-generated fallback icon SVG |

## Privacy Model

**What the server stores:** Nothing. There is no database, no session store, no log files beyond stdout.

**What the server logs to stdout:** Token cache refresh counts. That's it. No IP addresses, no swap amounts, no wallet addresses.

**How orders work:** When you confirm a swap, the server encrypts the order details (deposit address, amounts, correlation ID) into an AES-256-GCM token. This token is part of the URL (`/order/{token}`). The server decrypts it on each page load to fetch status from NEAR Intents. If the server restarts with a different `ORDER_SECRET`, old order links stop working — the data existed only in the URL.

**What the templates load:** Nothing external. No Google Fonts, no CDN resources, no analytics scripts. The only JavaScript is an 8-line inline clipboard helper with a `<noscript>` fallback.

## Verify

Every deployment's commit hash, image digest, and build log are published at `/verify`. To verify independently:

```bash
# Clone and build the same commit
git clone https://github.com/uSwapExchange/zero.git
cd zero
git checkout <commit-from-verify-page>
go build -o zero .

# Or build with Docker (same as production)
docker build -t zero .
```

Check `nearintents.go` for zero fee markup. Check `handlers.go` for zero logging. Check `go.mod` for zero dependencies.

## License

[MIT](LICENSE)
