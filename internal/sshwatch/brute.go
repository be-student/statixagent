package sshwatch

import (
	"sync"
	"time"
)

// BruteDetector flags an IP whose failed attempts exceed Threshold within
// Window. Each IP alerts once per quiet period: after firing, it must fall
// silent for a full Window before it can fire again, preventing alert spam
// during an ongoing attack.
type BruteDetector struct {
	Window    time.Duration
	Threshold int

	mu       sync.Mutex
	attempts map[string][]time.Time
	fired    map[string]time.Time
}

// NewBruteDetector returns a detector with the given window and threshold.
func NewBruteDetector(window time.Duration, threshold int) *BruteDetector {
	return &BruteDetector{
		Window: window, Threshold: threshold,
		attempts: map[string][]time.Time{},
		fired:    map[string]time.Time{},
	}
}

// Record registers a failed attempt and reports whether this attempt
// crosses the threshold (i.e. the caller should alert now).
func (b *BruteDetector) Record(ip string, at time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	cutoff := at.Add(-b.Window)
	kept := b.attempts[ip][:0]
	for _, t := range b.attempts[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, at)
	b.attempts[ip] = kept

	if len(kept) < b.Threshold {
		return false
	}
	if last, ok := b.fired[ip]; ok && at.Sub(last) < b.Window {
		b.fired[ip] = at // attack continues; slide the quiet period forward
		return false
	}
	b.fired[ip] = at
	return true
}

// Count returns the attempts currently inside the window for an IP.
func (b *BruteDetector) Count(ip string, now time.Time) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	cutoff := now.Add(-b.Window)
	for _, t := range b.attempts[ip] {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}
