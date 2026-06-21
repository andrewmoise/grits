package gritsd

import (
	"encoding/json"
	"fmt"
	"grits/internal/grits"
	"log"
	"net/http"
	"sync"
	"time"
)

// RateLimitEntry is a per-limit config value, keyed by camelCase name.
type RateLimitEntry struct {
	Max      int64         `json:"max"`
	Duration time.Duration `json:"duration"`
	Cost     int64         `json:"cost,omitempty"`
}

func (e *RateLimitEntry) UnmarshalJSON(data []byte) error {
	return grits.UnmarshalDurationFields(data, e)
}

func (e *RateLimitEntry) MarshalJSON() ([]byte, error) {
	return grits.MarshalDurationFields(e)
}

// RateLimitModuleConfig is the top-level config for the rate limit module.
// Custom JSON methods flatten/parse keys directly (all keys except "module").
type RateLimitModuleConfig struct {
	Limits map[string]RateLimitEntry
}

func (c *RateLimitModuleConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Limits = make(map[string]RateLimitEntry, len(raw))
	for key, val := range raw {
		if key == "module" {
			continue
		}
		var entry RateLimitEntry
		if err := json.Unmarshal(val, &entry); err != nil {
			return fmt.Errorf("rateLimit: invalid entry %q: %w", key, err)
		}
		if entry.Max < 0 {
			return fmt.Errorf("rateLimit: limit %q has invalid max %d", key, entry.Max)
		}
		if entry.Duration <= 0 {
			return fmt.Errorf("rateLimit: limit %q has invalid duration %v", key, entry.Duration)
		}
		c.Limits[key] = entry
	}
	return nil
}

func (c *RateLimitModuleConfig) MarshalJSON() ([]byte, error) {
	raw := make(map[string]RateLimitEntry, len(c.Limits))
	for key, entry := range c.Limits {
		raw[key] = entry
	}
	return json.Marshal(raw)
}

// rateLimitConfig is the internal per-limit state stored in the module.
type rateLimitConfig struct {
	Name     string
	Max      int64
	Duration time.Duration
	Cost     int64
}

// bucketKey identifies a unique (identity, limit) pair.
type bucketKey struct {
	Identity string
	Limit    string
}

// bucketState tracks usage within a sliding window.
type bucketState struct {
	Amount      float64
	LastUpdated time.Time
	mu          sync.Mutex
}

// RateLimitModule enforces configurable rate limits using token-bucket logic.
type RateLimitModule struct {
	Config  *RateLimitModuleConfig
	Server  *Server
	limits  map[string]*rateLimitConfig
	buckets sync.Map
}

func NewRateLimitModule(server *Server, config *RateLimitModuleConfig) (*RateLimitModule, error) {
	limits := make(map[string]*rateLimitConfig, len(config.Limits))
	for name, entry := range config.Limits {
		cost := entry.Cost
		if cost <= 0 {
			cost = 1
		}
		limits[name] = &rateLimitConfig{
			Name:     name,
			Max:      entry.Max,
			Duration: entry.Duration,
			Cost:     cost,
		}
	}

	m := &RateLimitModule{
		Config: config,
		Server: server,
		limits: limits,
	}

	server.AddModuleHook(func(module Module) {
		if httpMod, ok := module.(*HTTPModule); ok {
			httpMod.SetRateLimitModule(m)
			log.Println("rateLimit: registered with HTTP module")
		}
	})
	server.AddModuleHook(func(module Module) {
		if authMod, ok := module.(*AuthModule); ok {
			authMod.SetRateLimitModule(m)
			log.Println("rateLimit: registered with Auth module")
		}
	})

	return m, nil
}

func (*RateLimitModule) GetModuleName() string {
	return "rateLimit"
}

func (*RateLimitModule) GetDependencies() []*Dependency {
	return nil
}

func (m *RateLimitModule) GetConfig() any {
	return m.Config
}

func (m *RateLimitModule) Start() error {
	return nil
}

func (m *RateLimitModule) Stop() error {
	return nil
}

// CheckLimit returns nil if the action is allowed, or an httpError with 429
// status if the rate limit would be exceeded.
func (m *RateLimitModule) CheckLimit(identity string, limitName string, cost int64) *httpError {
	cfg, ok := m.limits[limitName]
	if !ok {
		return nil
	}

	if cost <= 0 {
		cost = cfg.Cost
	}
	costF := float64(cost)
	maxF := float64(cfg.Max)
	rate := maxF / cfg.Duration.Seconds()

	raw, _ := m.buckets.LoadOrStore(bucketKey{Identity: identity, Limit: limitName}, &bucketState{})
	bucket := raw.(*bucketState)

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()
	if !bucket.LastUpdated.IsZero() {
		elapsed := now.Sub(bucket.LastUpdated).Seconds()
		decay := rate * elapsed
		bucket.Amount -= decay
		if bucket.Amount < 0 {
			bucket.Amount = 0
		}
	}

	if bucket.Amount+costF > maxF {
		return &httpError{
			Code:    http.StatusTooManyRequests,
			Message: fmt.Sprintf("rate limit exceeded: %s (max %d per %v)", limitName, cfg.Max, cfg.Duration),
		}
	}

	bucket.Amount += costF
	if bucket.LastUpdated.IsZero() {
		bucket.LastUpdated = now
	} else {
		bucket.LastUpdated = now
	}

	return nil
}

// httpError is a lightweight error with an HTTP status code.
type httpError struct {
	Code    int
	Message string
}

func (e *httpError) Error() string {
	return e.Message
}
