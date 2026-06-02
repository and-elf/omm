package discovery

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
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
	return ListenUDP(ctx, listenAddr, func(data []byte, src *net.UDPAddr) {
		var ann Announcement
		if json.Unmarshal(data, &ann) != nil {
			return
		}
		if src != nil {
			// Controllers announce their bind address, which is often the
			// unspecified 0.0.0.0; fill the real host from the packet source so
			// joiners get a dialable URL without per-device configuration.
			ann.API = fixAPIHost(ann.API, src.IP)
		}
		c.Add(ann)
	})
}

// fixAPIHost replaces an unspecified host (0.0.0.0 / :: / empty) in the
// announced API URL with src, preserving the scheme, port and path. A URL that
// already names a concrete host is returned unchanged.
func fixAPIHost(api string, src net.IP) string {
	if api == "" || src == nil {
		return api
	}
	u, err := url.Parse(api)
	if err != nil {
		return api
	}
	switch u.Hostname() {
	case "", "0.0.0.0", "::":
		if port := u.Port(); port != "" {
			u.Host = net.JoinHostPort(src.String(), port)
		} else {
			u.Host = src.String()
		}
		return u.String()
	default:
		return api
	}
}
