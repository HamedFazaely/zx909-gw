package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/HamedFazaely/zx909-gw/internal/config"
)

// PaapeliClient checks registration via Paapeli internal API and caches results.
type PaapeliClient struct {
	cfg    config.PaapeliConfig
	http   *http.Client
	mu     sync.Mutex
	token  string
	cache  map[string]cacheEntry
}

type cacheEntry struct {
	registered bool
	expires    time.Time
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string `json:"token"`
	TokenType string `json:"tokenType"`
}

type trackerResponse struct {
	DeviceName string `json:"deviceName"`
	Registered bool   `json:"registered"`
}

// New returns AllowAll when disabled or misconfigured; otherwise a Paapeli client.
func New(cfg config.PaapeliConfig) Registry {
	if !cfg.Enabled {
		return AllowAll{}
	}
	if cfg.BaseURL == "" || cfg.Username == "" {
		slog.Warn("paapeli enabled but base_url/username empty; treating as disabled (allow all)")
		return AllowAll{}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &PaapeliClient{
		cfg:   cfg,
		http:  &http.Client{Timeout: timeout},
		cache: make(map[string]cacheEntry),
	}
}

func (p *PaapeliClient) Enabled() bool { return true }

func (p *PaapeliClient) IsRegistered(ctx context.Context, imei string) bool {
	if imei == "" {
		return false
	}
	if reg, ok := p.cacheGet(imei); ok {
		return reg
	}
	reg, err := p.fetch(ctx, imei, true)
	if err != nil {
		slog.Warn("paapeli registration lookup failed (fail closed)", "imei", imei, "error", err)
		return false
	}
	p.cacheSet(imei, reg)
	return reg
}

func (p *PaapeliClient) cacheGet(imei string) (bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.cache[imei]
	if !ok || time.Now().After(e.expires) {
		return false, false
	}
	return e.registered, true
}

func (p *PaapeliClient) cacheSet(imei string, registered bool) {
	ttl := p.cfg.NegativeTTL
	if registered {
		ttl = p.cfg.PositiveTTL
	}
	if ttl <= 0 {
		if registered {
			ttl = 30 * time.Minute
		} else {
			ttl = 60 * time.Second
		}
	}
	p.mu.Lock()
	p.cache[imei] = cacheEntry{registered: registered, expires: time.Now().Add(ttl)}
	p.mu.Unlock()
}

func (p *PaapeliClient) fetch(ctx context.Context, imei string, allowRetry bool) (bool, error) {
	token, err := p.ensureToken(ctx)
	if err != nil {
		return false, err
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/internal/tracker/" + imei
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode == http.StatusUnauthorized && allowRetry {
		p.invalidateToken()
		return p.fetch(ctx, imei, false)
	}
	if resp.StatusCode == http.StatusNotFound {
		// Not known to Paapeli → unregistered
		return false, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("paapeli tracker HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var out trackerResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return false, fmt.Errorf("paapeli tracker decode: %w", err)
	}
	return out.Registered, nil
}

func (p *PaapeliClient) ensureToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	if p.token != "" {
		t := p.token
		p.mu.Unlock()
		return t, nil
	}
	p.mu.Unlock()
	return p.login(ctx)
}

func (p *PaapeliClient) invalidateToken() {
	p.mu.Lock()
	p.token = ""
	p.mu.Unlock()
}

func (p *PaapeliClient) login(ctx context.Context) (string, error) {
	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/api/v1/aiot/auth/login"
	payload, _ := json.Marshal(loginRequest{
		Username: p.cfg.Username,
		Password: p.cfg.Password,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("paapeli login HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var out loginResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("paapeli login decode: %w", err)
	}
	if out.Token == "" {
		return "", fmt.Errorf("paapeli login: empty token")
	}
	p.mu.Lock()
	p.token = out.Token
	p.mu.Unlock()
	return out.Token, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
