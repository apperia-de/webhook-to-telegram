package swt

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type WebhookClaim struct {
	Event      string `json:"event"`
	Hash       string `json:"hash,omitempty"`
	RetryCount *int   `json:"retry_count,omitempty"`
}

type Claims struct {
	Webhook WebhookClaim `json:"webhook"`
	Iss     string       `json:"iss,omitempty"`
	Sub     string       `json:"sub,omitempty"`
	Exp     int64        `json:"exp,omitempty"`
	Nbf     int64        `json:"nbf,omitempty"`
	Iat     int64        `json:"iat,omitempty"`
	Jti     string       `json:"jti,omitempty"`
}

// Verify verifies the SWT token against the shared secret and checks request body integrity.
func Verify(tokenString string, secret []byte, body []byte, now time.Time) (*Claims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format: must have 3 parts")
	}

	headerSegment := parts[0]
	payloadSegment := parts[1]
	signatureSegment := parts[2]

	headerBytes, err := base64.RawURLEncoding.DecodeString(headerSegment)
	if err != nil {
		return nil, fmt.Errorf("failed to decode header: %w", err)
	}

	var header Header
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	if header.Typ != "SWT" {
		return nil, fmt.Errorf("invalid token type: typ must be SWT, got %q", header.Typ)
	}

	if header.Alg != "HS256" {
		return nil, fmt.Errorf("unsupported signing algorithm: alg must be HS256, got %q", header.Alg)
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(headerSegment + "." + payloadSegment))
	expectedSignature := mac.Sum(nil)

	actualSignature, err := base64.RawURLEncoding.DecodeString(signatureSegment)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signature: %w", err)
	}

	if subtle.ConstantTimeCompare(actualSignature, expectedSignature) != 1 {
		return nil, fmt.Errorf("invalid signature")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadSegment)
	if err != nil {
		return nil, fmt.Errorf("failed to decode payload: %w", err)
	}

	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse claims: %w", err)
	}

	if claims.Exp != 0 && now.Unix() > claims.Exp {
		return nil, fmt.Errorf("token has expired")
	}
	if claims.Nbf != 0 && now.Unix() < claims.Nbf {
		return nil, fmt.Errorf("token is not valid yet (nbf)")
	}

	if claims.Webhook.Event == "" {
		return nil, fmt.Errorf("missing required webhook.event claim")
	}

	if len(body) > 0 {
		if claims.Webhook.Hash == "" {
			return nil, fmt.Errorf("missing required webhook.hash claim for non-empty request body")
		}

		hashParts := strings.Split(claims.Webhook.Hash, ":")
		if len(hashParts) != 2 {
			return nil, fmt.Errorf("invalid hash format: must be algo:value")
		}
		algo, value := hashParts[0], hashParts[1]

		var computedHash []byte
		switch strings.ToLower(algo) {
		case "sha-256":
			h := sha256.Sum256(body)
			computedHash = h[:]
		case "sha-512":
			h := sha512.Sum512(body)
			computedHash = h[:]
		default:
			return nil, fmt.Errorf("unsupported hash algorithm: %q", algo)
		}

		computedHex := fmt.Sprintf("%x", computedHash)
		if subtle.ConstantTimeCompare([]byte(computedHex), []byte(value)) != 1 {
			return nil, fmt.Errorf("payload integrity check failed: hash mismatch")
		}
	} else {
		if claims.Webhook.Hash != "" {
			return nil, fmt.Errorf("webhook.hash claim MUST be omitted for empty request body")
		}
	}

	return &claims, nil
}
