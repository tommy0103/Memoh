package application

import "sync"

const (
	// animationCacheEntries and animationCacheBytes bound the rendered-frame
	// cache. Only derived frames are cached: an ordinary image is returned as
	// the bytes it already was, so caching it would spend the budget without
	// saving any work.
	animationCacheEntries = 64
	animationCacheBytes   = 16 * 1024 * 1024
)

// animationCache remembers rendered animation frames for the process lifetime.
//
// The same sticker reappears across turns — it stays in a discussion's context
// and is re-prepared on every model call — and rendering it again costs tens of
// milliseconds and a fresh WebAssembly instance each time for a result that is
// byte-identical. Entries are keyed by bot and content hash: content
// addressing already makes the bytes unique, and scoping by bot keeps one
// tenant's media out of another's cache.
//
// The zero value is ready to use.
type animationCache struct {
	mu      sync.Mutex
	entries map[string][]string
	order   []string
	bytes   int
}

func animationCacheKey(botID, contentHash string) string { return botID + "\x00" + contentHash }

func (c *animationCache) get(key string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	frames, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.touch(key)
	// Hand back a copy: a caller is free to keep or mutate what it received.
	return append([]string(nil), frames...), true
}

func (c *animationCache) put(key string, frames []string) {
	size := 0
	for _, frame := range frames {
		size += len(frame)
	}
	if size > animationCacheBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string][]string, animationCacheEntries)
	}
	if _, exists := c.entries[key]; exists {
		c.touch(key)
		return
	}
	for len(c.order) > 0 && (c.bytes+size > animationCacheBytes || len(c.order) >= animationCacheEntries) {
		c.evictOldest()
	}
	c.entries[key] = append([]string(nil), frames...)
	c.order = append(c.order, key)
	c.bytes += size
}

// touch moves a key to the most-recently-used end. Callers hold the lock.
func (c *animationCache) touch(key string) {
	for i, existing := range c.order {
		if existing == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			return
		}
	}
}

// evictOldest drops the least recently used entry. Callers hold the lock.
func (c *animationCache) evictOldest() {
	oldest := c.order[0]
	for _, frame := range c.entries[oldest] {
		c.bytes -= len(frame)
	}
	delete(c.entries, oldest)
	c.order = c.order[1:]
}
