package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

var orderKey []byte

func initCrypto() {
	secretHex := os.Getenv("ORDER_SECRET")
	if secretHex == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Fatal("failed to generate random key:", err)
		}
		orderKey = b
		log.Println("WARNING: ORDER_SECRET not set — generated random key. Tokens will not survive restart.")
		return
	}
	decoded, err := hex.DecodeString(secretHex)
	if err != nil || len(decoded) < 32 {
		log.Fatal("ORDER_SECRET must be a 64-character hex string (32 bytes)")
	}
	orderKey = decoded[:32]
}

// OrderData holds the swap metadata encrypted into the order token.
type OrderData struct {
	DepositAddr string `json:"d"`
	Memo        string `json:"m,omitempty"`
	FromTicker  string `json:"f"`
	FromNet     string `json:"fn"`
	ToTicker    string `json:"t"`
	ToNet       string `json:"tn"`
	AmountIn    string `json:"ai"`
	AmountOut   string `json:"ao"`
	Deadline    string `json:"dl"`
	CorrID      string `json:"c"`
	RefundAddr  string `json:"ra,omitempty"`
	RecvAddr    string `json:"rca,omitempty"`
}

// encryptOrderData encrypts order data into a base64url token.
// Format: IV (12 bytes) + GCM tag (16 bytes) + ciphertext → base64url
func encryptOrderData(data *OrderData) (string, error) {
	plaintext, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal order data: %w", err)
	}

	block, err := aes.NewCipher(orderKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	iv := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("generate iv: %w", err)
	}

	// Seal appends ciphertext+tag to dst
	sealed := gcm.Seal(nil, iv, plaintext, nil)

	// Pack: IV + sealed (ciphertext + tag)
	packed := make([]byte, 0, len(iv)+len(sealed))
	packed = append(packed, iv...)
	packed = append(packed, sealed...)

	return base64.RawURLEncoding.EncodeToString(packed), nil
}

// decryptOrderData decrypts a base64url token back to order data.
func decryptOrderData(token string) (*OrderData, error) {
	packed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	block, err := aes.NewCipher(orderKey)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(packed) < nonceSize+gcm.Overhead() {
		return nil, fmt.Errorf("token too short")
	}

	iv := packed[:nonceSize]
	ciphertext := packed[nonceSize:]

	plaintext, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	var data OrderData
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	return &data, nil
}

// generateCSRFToken creates a stateless CSRF token using HMAC.
func generateCSRFToken(formID string) string {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 36)
	payload := formID + ":" + ts
	mac := hmac.New(sha256.New, orderKey)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:16]
	return payload + ":" + sig
}

// verifyCSRFToken validates a stateless CSRF token.
func verifyCSRFToken(token, formID string, maxAge time.Duration) bool {
	parts := strings.SplitN(token, ":", 3)
	if len(parts) != 3 {
		return false
	}
	fid, ts, sig := parts[0], parts[1], parts[2]
	if fid != formID {
		return false
	}
	timestamp, err := strconv.ParseInt(ts, 36, 64)
	if err != nil {
		return false
	}
	if time.Since(time.UnixMilli(timestamp)) > maxAge {
		return false
	}
	payload := fid + ":" + ts
	mac := hmac.New(sha256.New, orderKey)
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))[:16]
	return hmac.Equal([]byte(sig), []byte(expected))
}
