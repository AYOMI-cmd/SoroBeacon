package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SignatureHeader carries the hex HMAC-SHA256 of the canonical signing
// string (timestamp + "." + raw body), keyed with the channel's current
// active secret. Receivers should recompute and compare.
const SignatureHeader = "X-SoroBeacon-Signature"

// SignaturePreviousHeader carries the same signature computed under
// previous_secret. It is only sent while a secret rotation is in flight so
// a receiver that has not switched keys yet can still verify a delivery.
const SignaturePreviousHeader = "X-SoroBeacon-Signature-Previous"

// TimestampHeader carries the Unix-seconds timestamp the signature is
// computed over. It is deliberately part of the signed string so receivers
// can reject replays: a bare signature over the body alone would let anyone
// who captures one request replay it forever.
const TimestampHeader = "X-SoroBeacon-Timestamp"

// webhookConfig: {"url": "https://example.com/hook", "secret": "shared-secret"}
// PreviousSecret, when set, is used to emit a second signature so a
// receiver can roll over without dropping deliveries mid-rotation.
type webhookConfig struct {
	URL          string `json:"url"`
	Secret       string `json:"secret"`
	PreviousSecret string `json:"previous_secret"`
}

// Webhook POSTs the full alert as JSON to an arbitrary endpoint, signed
// with HMAC-SHA256 so receivers can authenticate the payload.
type Webhook struct {
	cfg webhookConfig
}

// NewWebhook builds a generic webhook notifier from channel config.
func NewWebhook(config json.RawMessage) (Notifier, error) {
	var cfg webhookConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, fmt.Errorf("webhook: invalid config: %w", err)
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("webhook: url is required")
	}
	if cfg.Secret == "" {
		return nil, fmt.Errorf("webhook: secret is required")
	}
	return &Webhook{cfg: cfg}, nil
}

func (w *Webhook) Send(ctx context.Context, a Alert) error {
	body, err := json.Marshal(a)
	if err != nil {
		return err
	}

	// The timestamp is computed once and reused for both signatures; a
	// receiver validates each header against the same signed string.
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	// Sign with all active keys so receivers using any key during rotation
	// can verify the delivery. The first active key is used as the primary.
	activeKeys := keyTable.GetActiveKeys(w.cfg.URL)
	if len(activeKeys) == 0 {
		return fmt.Errorf("webhook: no active signing keys configured")
	}

	// Use the first (oldest) active key as the primary for the main signature
	primarySig := Sign(activeKeys[0].Key, timestamp, body)

	headers := map[string]string{
		TimestampHeader: timestamp,
		SignatureHeader: primarySig,
	}

	// If there are multiple active keys or a previous secret is configured,
	// also send the previous signature so receivers still on the old key
	// can verify the delivery.
	if len(activeKeys) > 1 || w.cfg.PreviousSecret != "" {
		// Determine which key to use for the previous signature
		var prevSig string
		if w.cfg.PreviousSecret != "" {
			prevSig = Sign([]byte(w.cfg.PreviousSecret), timestamp, body)
		} else if len(activeKeys) > 1 {
			// Use the second oldest active key as the previous key
			prevSig = Sign(activeKeys[1].Key, timestamp, body)
		} else {
			prevSig = ""
		}
		if prevSig != "" {
			headers[SignaturePreviousHeader] = prevSig
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.URL, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("webhook: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: delivery failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook: %s body: %s", resp.Status, string(bodyBytes))
	}

	return nil
}