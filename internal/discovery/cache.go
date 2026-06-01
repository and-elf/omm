package discovery

import (
	"context"
	"encoding/json"
	"net"
	"sort"
	"sync"
	"time"
)

// Cache holds controller announcements heard recently, so a scan can return
// instantly instead of waiting to catch a broadcast. Entries expire after a
// TTL.
type Cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	ttl     int64
	now     func() int64
}

type cacheEntry struct {
	ann Announcement
	at  int64
}

// NewCache creates an announcement cache. now defaults to time.Now (unix secs).
func NewCache(ttl time.Duration, now func() int64) *Cache {
	if now == nil {
		now = func() int64 { return time.Now().Unix() }
	}
	return &Cache{entries: map[string]cacheEntry{}, ttl: int64(ttl.Seconds()), now: now}
}

// Add records (or refreshes) an announcement.
func (c *Cache) Add(ann Announcement) {
	if ann.API == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[ann.HomeID+"|"+ann.API] = cacheEntry{ann: ann, at: c.now()}
}

// List returns the non-expired announcements, sorted by name then home id.
func (c *Cache) List() []Announcement {
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := c.now() - c.ttl
	var out []Announcement
	for _, e := range c.entries {
		if c.ttl <= 0 || e.at >= cutoff {
			out = append(out, e.ann)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].HomeID < out[j].HomeID
	})
	return out
}

// Listen binds listenAddr and feeds received controller announcements into the
// cache until the context is cancelled. Intended to run for the daemon's
// lifetime so /scan can answer from the cache.
func (c *Cache) Listen(ctx context.Context, listenAddr string) error {
	return ListenUDP(ctx, listenAddr, func(data []byte, _ *net.UDPAddr) {
		var ann Announcement
		if json.Unmarshal(data, &ann) == nil {
			c.Add(ann)
		}
	})
}
