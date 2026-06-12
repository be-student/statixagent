package sshwatch

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// KeyChange describes a detected authorized_keys modification.
type KeyChange struct {
	Path string
	Kind string // "created", "modified", "removed"
}

// KeysWatcher polls a set of authorized_keys files and reports changes by
// content hash, so permission-preserving rewrites with identical content
// stay quiet.
type KeysWatcher struct {
	paths  []string
	hashes map[string]string // path → hex sha256, "" = absent
	seeded bool
}

// NewKeysWatcher watches the given file paths.
func NewKeysWatcher(paths []string) *KeysWatcher {
	return &KeysWatcher{paths: paths, hashes: map[string]string{}}
}

// Poll re-hashes every watched file and returns the changes since the last
// call. The first call seeds the baseline and reports nothing.
func (w *KeysWatcher) Poll() []KeyChange {
	var changes []KeyChange
	for _, p := range w.paths {
		cur := hashFile(p)
		prev := w.hashes[p]
		w.hashes[p] = cur
		if !w.seeded || cur == prev {
			continue
		}
		switch {
		case prev == "" && cur != "":
			changes = append(changes, KeyChange{Path: p, Kind: "created"})
		case prev != "" && cur == "":
			changes = append(changes, KeyChange{Path: p, Kind: "removed"})
		default:
			changes = append(changes, KeyChange{Path: p, Kind: "modified"})
		}
	}
	w.seeded = true
	return changes
}

func hashFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
