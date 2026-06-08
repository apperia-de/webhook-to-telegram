package swt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func generateTestToken(claims *Claims, secret []byte, alg, typ string) (string, error) {
	header := Header{
		Alg: alg,
		Typ: typ,
	}
	headerBytes, _ := json.Marshal(header)
	headerSegment := base64.RawURLEncoding.EncodeToString(headerBytes)

	payloadBytes, _ := json.Marshal(claims)
	payloadSegment := base64.RawURLEncoding.EncodeToString(payloadBytes)

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(headerSegment + "." + payloadSegment))
	signatureSegment := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return headerSegment + "." + payloadSegment + "." + signatureSegment, nil
}

func TestVerifySWT(t *testing.T) {
	secret := []byte("secret-key")
	now := time.Now()

	// 1. Success case: empty body
	claims1 := &Claims{
		Webhook: WebhookClaim{
			Event: "ping",
		},
		Exp: now.Add(5 * time.Minute).Unix(),
		Nbf: now.Add(-1 * time.Minute).Unix(),
	}
	token1, err := generateTestToken(claims1, secret, "HS256", "SWT")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	verifiedClaims1, err := Verify(token1, secret, nil, now)
	if err != nil {
		t.Errorf("expected verification success, got error: %v", err)
	}
	if verifiedClaims1.Webhook.Event != "ping" {
		t.Errorf("expected event 'ping', got %q", verifiedClaims1.Webhook.Event)
	}

	// 2. Success case: non-empty body with sha-256 hash
	body := []byte("hello world")
	h := sha256.Sum256(body)
	bodyHash := fmt.Sprintf("sha-256:%x", h[:])

	claims2 := &Claims{
		Webhook: WebhookClaim{
			Event: "payment.completed",
			Hash:  bodyHash,
		},
		Exp: now.Add(5 * time.Minute).Unix(),
	}
	token2, err := generateTestToken(claims2, secret, "HS256", "SWT")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	verifiedClaims2, err := Verify(token2, secret, body, now)
	if err != nil {
		t.Errorf("expected verification success, got error: %v", err)
	}
	if verifiedClaims2.Webhook.Event != "payment.completed" {
		t.Errorf("expected event 'payment.completed', got %q", verifiedClaims2.Webhook.Event)
	}

	// 3. Expired token
	claims3 := &Claims{
		Webhook: WebhookClaim{Event: "ping"},
		Exp:     now.Add(-1 * time.Minute).Unix(),
	}
	token3, _ := generateTestToken(claims3, secret, "HS256", "SWT")
	_, err = Verify(token3, secret, nil, now)
	if err == nil || err.Error() != "token has expired" {
		t.Errorf("expected expired error, got %v", err)
	}

	// 4. Invalid signature
	_, err = Verify(token1, []byte("wrong-secret-key"), nil, now)
	if err == nil || err.Error() != "invalid signature" {
		t.Errorf("expected invalid signature error, got %v", err)
	}

	// 5. Hash mismatch for non-empty body
	_, err = Verify(token2, secret, []byte("different body content"), now)
	if err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("expected hash mismatch error, got %v", err)
	}

	// 6. Typ not SWT
	token6, _ := generateTestToken(claims1, secret, "HS256", "JWT")
	_, err = Verify(token6, secret, nil, now)
	if err == nil || !strings.Contains(err.Error(), "typ must be SWT") {
		t.Errorf("expected typ error, got %v", err)
	}

	// 7. Alg not HS256
	token7, _ := generateTestToken(claims1, secret, "RS256", "SWT")
	_, err = Verify(token7, secret, nil, now)
	if err == nil || !strings.Contains(err.Error(), "alg must be HS256") {
		t.Errorf("expected alg error, got %v", err)
	}

	// 8. Omitted hash claim on non-empty request body
	claims8 := &Claims{
		Webhook: WebhookClaim{
			Event: "payment.completed",
		},
	}
	token8, _ := generateTestToken(claims8, secret, "HS256", "SWT")
	_, err = Verify(token8, secret, body, now)
	if err == nil || !strings.Contains(err.Error(), "missing required webhook.hash") {
		t.Errorf("expected missing hash error, got %v", err)
	}

	// 9. Non-empty hash claim on empty request body
	claims9 := &Claims{
		Webhook: WebhookClaim{
			Event: "payment.completed",
			Hash:  bodyHash,
		},
	}
	token9, _ := generateTestToken(claims9, secret, "HS256", "SWT")
	_, err = Verify(token9, secret, nil, now)
	if err == nil || !strings.Contains(err.Error(), "webhook.hash claim MUST be omitted for empty request body") {
		t.Errorf("expected omitted hash error, got %v", err)
	}
}
