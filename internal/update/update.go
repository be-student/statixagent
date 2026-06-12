// Package update implements MVP §5: check GitHub Releases, download the new
// binary, verify SHA256 + ed25519 signature, swap atomically, restart, and
// roll back if the new version crash-loops. Signature verification is
// mandatory — whoever controls the update source controls the server.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Updater checks for and applies releases of one GitHub repo.
type Updater struct {
	Repo       string // "owner/name"
	Current    string // running version, e.g. "v1.0.0" (or "dev")
	BinaryPath string // the executable to replace
	AssetName  string // release asset to install, e.g. "statix-agent_linux_amd64"

	// PublicKey verifies the checksums file signature. Apply refuses to
	// run without it.
	PublicKey ed25519.PublicKey

	// Restart is invoked after a successful swap (systemctl restart).
	Restart func(ctx context.Context) error

	HTTP    *http.Client
	APIBase string // override for tests; default https://api.github.com
	DLBase  string // override for tests; default = asset browser_download_url as-is
}

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (u *Updater) api() string {
	if u.APIBase != "" {
		return u.APIBase
	}
	return "https://api.github.com"
}

func (u *Updater) client() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// Check returns the latest version tag and whether it is newer than Current.
func (u *Updater) Check(ctx context.Context) (string, bool, error) {
	rel, err := u.latest(ctx)
	if err != nil {
		return "", false, err
	}
	return rel.TagName, NewerThan(rel.TagName, u.Current), nil
}

func (u *Updater) latest(ctx context.Context) (*release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", u.api(), u.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: releases API returned %d", resp.StatusCode)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	return &rel, nil
}

// Apply downloads, verifies, and installs the latest release, then restarts.
func (u *Updater) Apply(ctx context.Context) error {
	if len(u.PublicKey) != ed25519.PublicKeySize {
		return errors.New("update: no signing public key embedded in this build; refusing unverified update")
	}
	rel, err := u.latest(ctx)
	if err != nil {
		return err
	}
	if !NewerThan(rel.TagName, u.Current) {
		return fmt.Errorf("update: %s is not newer than %s", rel.TagName, u.Current)
	}
	assetURL, sumsURL, sigURL := "", "", ""
	for _, a := range rel.Assets {
		switch a.Name {
		case u.AssetName:
			assetURL = a.URL
		case "checksums.txt":
			sumsURL = a.URL
		case "checksums.txt.sig":
			sigURL = a.URL
		}
	}
	if assetURL == "" || sumsURL == "" || sigURL == "" {
		return fmt.Errorf("update: release %s is missing %s, checksums.txt, or checksums.txt.sig", rel.TagName, u.AssetName)
	}

	sums, err := u.fetch(ctx, sumsURL, 1<<20)
	if err != nil {
		return err
	}
	sig, err := u.fetch(ctx, sigURL, 1<<10)
	if err != nil {
		return err
	}
	if !ed25519.Verify(u.PublicKey, sums, sig) {
		return errors.New("update: checksums signature verification FAILED — aborting")
	}
	wantSum, err := sumFor(string(sums), u.AssetName)
	if err != nil {
		return err
	}

	binData, err := u.fetch(ctx, assetURL, 200<<20)
	if err != nil {
		return err
	}
	gotSum := sha256.Sum256(binData)
	if hex.EncodeToString(gotSum[:]) != wantSum {
		return errors.New("update: binary SHA256 mismatch — aborting")
	}

	if err := SwapBinary(u.BinaryPath, binData); err != nil {
		return err
	}
	if u.Restart != nil {
		return u.Restart(ctx)
	}
	return nil
}

func (u *Updater) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: %s returned %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// sumFor extracts the sha256 for a file from "hex  name" formatted lines.
func sumFor(sums, name string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && (f[1] == name || f[1] == "*"+name) {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("update: %s not present in checksums.txt", name)
}

// SwapBinary atomically replaces path with data: the previous binary is
// kept at path.prev for rollback, the new one lands via rename from a temp
// file in the same directory.
func SwapBinary(path string, data []byte) error {
	prev := path + ".prev"
	if cur, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(prev+".tmp", cur, 0o755); err != nil {
			return fmt.Errorf("update: saving rollback copy: %w", err)
		}
		if err := os.Rename(prev+".tmp", prev); err != nil {
			return fmt.Errorf("update: saving rollback copy: %w", err)
		}
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("update: atomic swap: %w", err)
	}
	return nil
}

// NewerThan reports whether version a is strictly newer than b. Versions
// are "v1.2.3" (the v and missing parts are tolerated). Non-numeric
// versions ("dev") compare as older than any numeric version.
func NewerThan(a, b string) bool {
	av, aok := parseVer(a)
	bv, bok := parseVer(b)
	if !aok {
		return false // can't prove a is newer
	}
	if !bok {
		return true // numeric beats "dev"
	}
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return false
}

func parseVer(s string) ([3]int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "-") // ignore prerelease suffix
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) == 0 || parts[0] == "" {
		return v, false
	}
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}
