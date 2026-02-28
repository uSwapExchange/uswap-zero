package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
)

const subscriberPath = "data/subscribers.txt"
const unsubscriberPath = "data/unsubscribers.txt"

// subscriberStore tracks unique chat IDs that have interacted with the bot.
// Opted-out users are stored as SHA-256 hashes so we can check without
// retaining their actual ID.
type subscriberStore struct {
	mu     sync.Mutex
	ids    map[int64]bool
	unsubs map[string]bool // hashes of opted-out chat IDs
}

var subscribers = &subscriberStore{
	ids:    make(map[int64]bool),
	unsubs: make(map[string]bool),
}

// hashChatID returns a salted SHA-256 hex digest for a chat ID.
func hashChatID(chatID int64) string {
	h := sha256.Sum256([]byte("uswap-forget:" + strconv.FormatInt(chatID, 10)))
	return hex.EncodeToString(h[:])
}

// load reads existing subscribers and unsubscriber hashes from disk.
func (s *subscriberStore) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load subscribers
	if f, err := os.Open(subscriberPath); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if id, err := strconv.ParseInt(line, 10, 64); err == nil {
				s.ids[id] = true
			}
		}
		f.Close()
	}

	// Load unsubscriber hashes
	if f, err := os.Open(unsubscriberPath); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				s.unsubs[line] = true
			}
		}
		f.Close()
	}

	log.Printf("Loaded %d subscribers, %d forgotten", len(s.ids), len(s.unsubs))
}

// track records a chat ID. Skips if already known or previously opted out.
func (s *subscriberStore) track(chatID int64) {
	s.mu.Lock()

	if s.ids[chatID] {
		s.mu.Unlock()
		return
	}

	// Check if user previously opted out
	if s.unsubs[hashChatID(chatID)] {
		s.mu.Unlock()
		return
	}

	s.ids[chatID] = true

	os.MkdirAll("data", 0755)
	f, err := os.OpenFile(subscriberPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("subscriber write error: %v", err)
		s.mu.Unlock()
		return
	}
	fmt.Fprintf(f, "%d\n", chatID)
	f.Close()

	s.mu.Unlock()

	// Notify new subscriber (outside lock)
	tgSendMessage(chatID, "<i>You'll receive occasional important updates. /forget to opt out.</i>", nil)
}

// forget removes a chat ID and stores its hash so it stays opted out.
func (s *subscriberStore) forget(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove from active subscribers
	delete(s.ids, chatID)

	// Rewrite subscribers file without this ID
	os.MkdirAll("data", 0755)
	f, err := os.Create(subscriberPath)
	if err != nil {
		log.Printf("subscriber rewrite error: %v", err)
		return
	}
	for id := range s.ids {
		fmt.Fprintf(f, "%d\n", id)
	}
	f.Close()

	// Add hash to unsubscribers
	hash := hashChatID(chatID)
	s.unsubs[hash] = true
	uf, err := os.OpenFile(unsubscriberPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("unsubscriber write error: %v", err)
		return
	}
	fmt.Fprintf(uf, "%s\n", hash)
	uf.Close()
}

// resubscribe removes the opt-out hash and re-adds the chat ID.
func (s *subscriberStore) resubscribe(chatID int64) {
	s.mu.Lock()

	// Remove opt-out hash
	hash := hashChatID(chatID)
	delete(s.unsubs, hash)

	// Rewrite unsubscribers file
	os.MkdirAll("data", 0755)
	f, err := os.Create(unsubscriberPath)
	if err != nil {
		log.Printf("unsubscriber rewrite error: %v", err)
		s.mu.Unlock()
		return
	}
	for h := range s.unsubs {
		fmt.Fprintf(f, "%s\n", h)
	}
	f.Close()

	// Add to subscribers if not already there
	if !s.ids[chatID] {
		s.ids[chatID] = true
		sf, err := os.OpenFile(subscriberPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			log.Printf("subscriber write error: %v", err)
			s.mu.Unlock()
			return
		}
		fmt.Fprintf(sf, "%d\n", chatID)
		sf.Close()
	}

	s.mu.Unlock()
}

// count returns the number of active subscribers.
func (s *subscriberStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ids)
}
