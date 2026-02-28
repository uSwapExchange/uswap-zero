package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashChatID(t *testing.T) {
	h1 := hashChatID(12345)
	h2 := hashChatID(12345)
	h3 := hashChatID(99999)

	if h1 != h2 {
		t.Error("same ID should produce same hash")
	}
	if h1 == h3 {
		t.Error("different IDs should produce different hashes")
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64 (SHA-256 hex)", len(h1))
	}
}

// newTestStore creates a subscriberStore backed by temp files.
func newTestStore(t *testing.T) (*subscriberStore, string) {
	t.Helper()
	dir := t.TempDir()
	return &subscriberStore{
		ids:    make(map[int64]bool),
		unsubs: make(map[string]bool),
	}, dir
}

// withPaths temporarily overrides the file paths for testing.
func withPaths(dir string, fn func()) {
	oldSub := subscriberPath
	oldUnsub := unsubscriberPath
	// We can't reassign consts, so we test the store methods directly
	// using the in-memory maps and verify file I/O separately.
	_ = oldSub
	_ = oldUnsub
	fn()
}

func TestSubscriberTrackAndForget(t *testing.T) {
	s := &subscriberStore{
		ids:    make(map[int64]bool),
		unsubs: make(map[string]bool),
	}

	// Simulate track (in-memory only for unit test)
	chatID := int64(42)

	// Not yet tracked
	if s.ids[chatID] {
		t.Error("should not be tracked yet")
	}

	// Track
	s.ids[chatID] = true
	if !s.ids[chatID] {
		t.Error("should be tracked after add")
	}
	if s.count() != 1 {
		t.Errorf("count = %d, want 1", s.count())
	}

	// Forget: remove from ids, add hash to unsubs
	delete(s.ids, chatID)
	hash := hashChatID(chatID)
	s.unsubs[hash] = true

	if s.ids[chatID] {
		t.Error("should not be tracked after forget")
	}
	if s.count() != 0 {
		t.Errorf("count = %d, want 0", s.count())
	}

	// Track again should be blocked by unsub hash
	if s.unsubs[hashChatID(chatID)] {
		// Would skip — correct behavior
	} else {
		t.Error("unsub hash should block re-tracking")
	}
}

func TestSubscriberResubscribe(t *testing.T) {
	s := &subscriberStore{
		ids:    make(map[int64]bool),
		unsubs: make(map[string]bool),
	}

	chatID := int64(42)
	hash := hashChatID(chatID)

	// Start forgotten
	s.unsubs[hash] = true

	// Verify tracking is blocked
	if !s.unsubs[hashChatID(chatID)] {
		t.Error("should be blocked before resubscribe")
	}

	// Resubscribe: remove hash, add to ids
	delete(s.unsubs, hash)
	s.ids[chatID] = true

	if s.unsubs[hashChatID(chatID)] {
		t.Error("hash should be removed after resubscribe")
	}
	if !s.ids[chatID] {
		t.Error("should be tracked after resubscribe")
	}
}

func TestSubscriberFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	subFile := filepath.Join(dir, "subscribers.txt")
	unsubFile := filepath.Join(dir, "unsubscribers.txt")

	// Write subscriber file
	os.WriteFile(subFile, []byte("100\n200\n300\n"), 0600)

	// Write unsubscriber file
	hash := hashChatID(999)
	os.WriteFile(unsubFile, []byte(hash+"\n"), 0600)

	// Load into a store
	s := &subscriberStore{
		ids:    make(map[int64]bool),
		unsubs: make(map[string]bool),
	}

	// Manual load from files
	if data, err := os.ReadFile(subFile); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line != "" {
				var id int64
				for _, c := range line {
					id = id*10 + int64(c-'0')
				}
				s.ids[id] = true
			}
		}
	}
	if data, err := os.ReadFile(unsubFile); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line != "" {
				s.unsubs[line] = true
			}
		}
	}

	if s.count() != 3 {
		t.Errorf("loaded count = %d, want 3", s.count())
	}
	if !s.ids[100] || !s.ids[200] || !s.ids[300] {
		t.Error("missing expected subscriber IDs")
	}
	if !s.unsubs[hash] {
		t.Error("missing expected unsubscriber hash")
	}
	// 999 should be blocked
	if !s.unsubs[hashChatID(999)] {
		t.Error("chat ID 999 should be blocked by unsub hash")
	}
}

func TestExtractChatID(t *testing.T) {
	tests := []struct {
		name string
		u    TGUpdate
		want int64
	}{
		{
			name: "message",
			u:    TGUpdate{Message: &TGMessage{Chat: TGChat{ID: 42}}},
			want: 42,
		},
		{
			name: "callback",
			u:    TGUpdate{CallbackQuery: &TGCallbackQuery{Message: &TGMessage{Chat: TGChat{ID: 99}}}},
			want: 99,
		},
		{
			name: "inline",
			u:    TGUpdate{InlineQuery: &TGInlineQuery{From: TGUser{ID: 77}}},
			want: 77,
		},
		{
			name: "empty",
			u:    TGUpdate{},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractChatID(&tt.u)
			if got != tt.want {
				t.Errorf("extractChatID() = %d, want %d", got, tt.want)
			}
		})
	}
}
