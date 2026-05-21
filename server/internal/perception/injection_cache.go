package perception

import (
	"sync"
	"time"
)

// InjectionCache tracks the last digest emitted to each session so we can
// skip repeating identical context turn after turn. The biggest dogfood
// noise source: ~70% of a turn's bundle was unchanged from the previous
// turn but still re-injected.
//
// Safe for concurrent use; the hook handler may call IfChanged from many
// goroutines.
type InjectionCache struct {
	mu    sync.Mutex
	items map[string]injectionCacheEntry
	ttl   time.Duration
}

type injectionCacheEntry struct {
	digest  Digest
	expires time.Time
}

// NewInjectionCache returns a cache with the given per-entry TTL. Entries
// silently expire so a long-idle session re-emits the full bundle on its
// next turn (we want to re-orient the agent after a break).
func NewInjectionCache(ttl time.Duration) *InjectionCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &InjectionCache{
		items: make(map[string]injectionCacheEntry),
		ttl:   ttl,
	}
}

// IfChanged returns true iff the new digest differs from what was last
// stored for sessionID. Kept for callers that only need a binary
// changed/unchanged answer (PreCompact / SessionStart that seed the
// cache without consuming the delta).
//
// Equivalent to `!c.Diff(sessionID, d).Empty()`. See Diff for the cache
// update semantics.
func (c *InjectionCache) IfChanged(sessionID string, d Digest) bool {
	return !c.Diff(sessionID, d).Empty()
}

// Diff returns which Bundle sections changed since the last cached
// digest for sessionID. First call for a session returns
// AllSectionsDelta (no prior baseline → render everything). When the
// new digest matches the cached one, returns an empty delta and
// callers should suppress emit.
//
// In all cases the cache is updated: on a change, the new digest
// replaces the old one; on no-change, the existing entry's expiry is
// refreshed so a stable-but-active session doesn't fall out.
//
// Forced bypass: pass sessionID="" and the call always returns
// AllSectionsDelta without touching the cache.
func (c *InjectionCache) Diff(sessionID string, d Digest) DigestDelta {
	if sessionID == "" {
		return AllSectionsDelta()
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	prev, ok := c.items[sessionID]
	if ok && now.After(prev.expires) {
		delete(c.items, sessionID)
		ok = false
	}
	if !ok {
		c.items[sessionID] = injectionCacheEntry{digest: d, expires: now.Add(c.ttl)}
		c.opportunisticGC(now)
		return AllSectionsDelta()
	}

	delta := d.DiffFrom(prev.digest)
	if delta.Empty() {
		// Refresh expiry, keep the existing baseline.
		c.items[sessionID] = injectionCacheEntry{digest: prev.digest, expires: now.Add(c.ttl)}
		return delta
	}
	c.items[sessionID] = injectionCacheEntry{digest: d, expires: now.Add(c.ttl)}
	return delta
}

// Forget clears the cached entry for sessionID — useful when a session is
// known to have ended (so the next session with the same id re-injects).
func (c *InjectionCache) Forget(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, sessionID)
}

// opportunisticGC drops expired entries; called inline under the lock.
// Bounded to avoid pathological cost on large maps.
func (c *InjectionCache) opportunisticGC(now time.Time) {
	if len(c.items) < 64 {
		return
	}
	checked := 0
	for k, v := range c.items {
		if now.After(v.expires) {
			delete(c.items, k)
		}
		checked++
		if checked >= 32 {
			break
		}
	}
}
