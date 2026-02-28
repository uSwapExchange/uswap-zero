package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
)

const subscriberPath = "data/subscribers.txt"

// subscriberStore tracks unique chat IDs that have interacted with the bot.
type subscriberStore struct {
	mu   sync.Mutex
	ids  map[int64]bool
	file *os.File
}

var subscribers = &subscriberStore{
	ids: make(map[int64]bool),
}

// loadSubscribers reads existing chat IDs from disk.
func (s *subscriberStore) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(subscriberPath)
	if err != nil {
		return // file doesn't exist yet — that's fine
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if id, err := strconv.ParseInt(line, 10, 64); err == nil {
			s.ids[id] = true
		}
	}
	log.Printf("Loaded %d subscribers", len(s.ids))
}

// track records a chat ID. No-op if already known.
func (s *subscriberStore) track(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ids[chatID] {
		return
	}
	s.ids[chatID] = true

	// Ensure data/ directory exists
	os.MkdirAll("data", 0755)

	f, err := os.OpenFile(subscriberPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		log.Printf("subscriber write error: %v", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%d\n", chatID)
}

// count returns the number of known subscribers.
func (s *subscriberStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ids)
}
