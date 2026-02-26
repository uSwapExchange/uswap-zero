package main

import (
	"sync"
	"time"
)

// Session states
const (
	stateIdle         = 0
	stateSwapCard     = 1
	statePickToken    = 2
	statePickNet      = 3
	stateEnterAmount  = 4
	stateEnterRefund  = 5
	stateEnterRecv    = 6
	statePickSlippage = 7
	stateQuoteConfirm = 8
	stateOrderActive  = 9
)

// tgSession holds the swap state for a single Telegram chat.
type tgSession struct {
	mu sync.Mutex

	State     int
	CardMsgID int // the persistent swap card message ID
	LastTouch time.Time

	// Swap fields
	FromTicker string
	FromNet    string
	ToTicker   string
	ToNet      string
	Amount     string
	RefundAddr string
	RecvAddr   string
	Slippage   string // percentage string: "0.5", "1", "2", "3"

	// Token picker context
	PickSide string // "from" or "to"
	PickPage int

	// Prompt message IDs to clean up
	PromptMsgID int
	ReplyMsgID  int

	// Order tracking
	OrderToken   string
	DepositMsgID int
	OrderMsgIDs  []int // all message IDs related to this swap

	// Quote cache
	DryQuote *DryQuoteResponse
}

// tgSessionStore manages sessions keyed by chat_id.
type tgSessionStore struct {
	mu       sync.Mutex
	sessions map[int64]*tgSession
}

var tgSessions = &tgSessionStore{
	sessions: make(map[int64]*tgSession),
}

// get returns the session for a chat, creating one if needed.
func (s *tgSessionStore) get(chatID int64) *tgSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[chatID]
	if !ok {
		sess = &tgSession{
			Slippage:  "1",
			LastTouch: time.Now(),
		}
		s.sessions[chatID] = sess
	}
	sess.LastTouch = time.Now()
	return sess
}

// reset clears a session back to defaults (keeps chat mapping).
func (sess *tgSession) reset() {
	sess.State = stateIdle
	sess.CardMsgID = 0
	sess.FromTicker = "BTC"
	sess.FromNet = "btc"
	sess.ToTicker = "ETH"
	sess.ToNet = "eth"
	sess.Amount = ""
	sess.RefundAddr = ""
	sess.RecvAddr = ""
	sess.Slippage = "1"
	sess.PickSide = ""
	sess.PickPage = 0
	sess.PromptMsgID = 0
	sess.ReplyMsgID = 0
	sess.OrderToken = ""
	sess.DepositMsgID = 0
	sess.OrderMsgIDs = nil
	sess.DryQuote = nil
}

// trackMsg records a message ID for later cleanup.
func (sess *tgSession) trackMsg(msgID int) {
	if msgID != 0 {
		sess.OrderMsgIDs = append(sess.OrderMsgIDs, msgID)
	}
}

// isComplete returns true when all swap fields are filled.
func (sess *tgSession) isComplete() bool {
	return sess.FromTicker != "" && sess.ToTicker != "" &&
		sess.Amount != "" && sess.RefundAddr != "" && sess.RecvAddr != ""
}

// startCleanup starts a goroutine that removes stale sessions.
func (s *tgSessionStore) startCleanup() {
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			s.mu.Lock()
			now := time.Now()
			for id, sess := range s.sessions {
				if now.Sub(sess.LastTouch) > 2*time.Hour {
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}()
}
