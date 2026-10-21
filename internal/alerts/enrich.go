package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const maxEnrichmentBytes int64 = 64 << 10

// Enricher fetches operator-supplied alert context. The response is kept as a
// separate JSON object so untrusted fields cannot become notification template
// text or override the alert's own fields.
type Enricher struct {
	endpoint string
	client   *http.Client
	ttl      time.Duration
	mu       sync.Mutex
	cache    map[string]cacheEntry
}

type cacheEntry struct {
	value   json.RawMessage
	expires time.Time
}

// NewEnricher creates an enricher. An empty endpoint disables enrichment.
func NewEnricher(endpoint string, timeout, ttl time.Duration) (*Enricher, error) {
	if endpoint == "" {
		return nil, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid ALERT_ENRICHMENT_URL: want an absolute http or https URL")
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Enricher{endpoint: endpoint, client: &http.Client{Timeout: timeout}, ttl: ttl, cache: make(map[string]cacheEntry)}, nil
}

// Fetch returns a bounded JSON object for the contract, or an error. Callers
// should treat errors as a soft failure and deliver the original alert.
func (e *Enricher) Fetch(ctx context.Context, contractID, eventName string) (json.RawMessage, error) {
	if e == nil || e.endpoint == "" {
		return nil, nil
	}
	key := contractID
	e.mu.Lock()
	if cached, ok := e.cache[key]; ok && time.Now().Before(cached.expires) {
		value := append(json.RawMessage(nil), cached.value...)
		e.mu.Unlock()
		return value, nil
	}
	e.mu.Unlock()

	u, err := url.Parse(e.endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("contract_id", contractID)
	q.Set("event_name", eventName)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("enrichment source returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxEnrichmentBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxEnrichmentBytes {
		return nil, fmt.Errorf("enrichment response exceeds %d bytes", maxEnrichmentBytes)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil || object == nil {
		return nil, fmt.Errorf("enrichment response must be a JSON object")
	}
	value, _ := json.Marshal(object)
	e.mu.Lock()
	e.cache[key] = cacheEntry{value: value, expires: time.Now().Add(e.ttl)}
	e.mu.Unlock()
	return value, nil
}
