package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewerThan(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.2.3", "v1.2.2", true},
		{"v1.2.3", "v1.2.3", false},
		{"v2.0.0", "v1.9.9", true},
		{"v1.2.3", "v1.10.0", false},
		{"v1.10.0", "v1.2.3", true},
		{"1.0.1", "v1.0.0", true},
		{"v1.0.0", "dev", true},
		{"dev", "v1.0.0", false},
		{"v1.2.3-rc1", "v1.2.2", true},
		{"v1.2", "v1.1.9", true},
	}
	for _, tc := range cases {
		if got := NewerThan(tc.a, tc.b); got != tc.want {
			t.Errorf("NewerThan(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSwapBinaryKeepsRollbackCopy(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "agent")
	os.WriteFile(bin, []byte("OLD"), 0o755)

	if err := SwapBinary(bin, []byte("NEW")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(bin); string(got) != "NEW" {
		t.Errorf("binary = %q", got)
	}
	if got, _ := os.ReadFile(bin + ".prev"); string(got) != "OLD" {
		t.Errorf("prev = %q", got)
	}
	// Swapping when no current binary exists works too (fresh install).
	bin2 := filepath.Join(dir, "agent2")
	if err := SwapBinary(bin2, []byte("FIRST")); err != nil {
		t.Fatal(err)
	}
}

// fakeRelease serves a complete signed release over httptest.
func fakeRelease(t *testing.T, tag, assetName string, binary []byte, priv ed25519.PrivateKey, tamper bool) *httptest.Server {
	t.Helper()
	sum := sha256.Sum256(binary)
	sums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName)
	sig := ed25519.Sign(priv, []byte(sums))
	if tamper {
		binary = append([]byte("EVIL"), binary...)
	}
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name":%q,"assets":[
			{"name":%q,"browser_download_url":"%s/dl/bin"},
			{"name":"checksums.txt","browser_download_url":"%s/dl/sums"},
			{"name":"checksums.txt.sig","browser_download_url":"%s/dl/sig"}
		]}`, tag, assetName, srv.URL, srv.URL, srv.URL)
	})
	mux.HandleFunc("/dl/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(binary) })
	mux.HandleFunc("/dl/sums", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(sums)) })
	mux.HandleFunc("/dl/sig", func(w http.ResponseWriter, r *http.Request) { w.Write(sig) })
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestUpdater(t *testing.T, srv *httptest.Server, pub ed25519.PublicKey, bin string) *Updater {
	t.Helper()
	return &Updater{
		Repo: "o/r", Current: "v1.0.0", BinaryPath: bin,
		AssetName: "agent_linux_amd64", PublicKey: pub,
		HTTP: srv.Client(), APIBase: srv.URL,
	}
}

func TestApplyVerifiesAndSwaps(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	newBin := []byte("NEW BINARY v1.1.0")
	srv := fakeRelease(t, "v1.1.0", "agent_linux_amd64", newBin, priv, false)

	bin := filepath.Join(t.TempDir(), "agent")
	os.WriteFile(bin, []byte("OLD BINARY"), 0o755)
	restarted := false
	u := newTestUpdater(t, srv, pub, bin)
	u.Restart = func(ctx context.Context) error { restarted = true; return nil }

	tag, ok, err := u.Check(context.Background())
	if err != nil || !ok || tag != "v1.1.0" {
		t.Fatalf("Check = %q %v %v", tag, ok, err)
	}
	if err := u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(bin); string(got) != string(newBin) {
		t.Errorf("binary not swapped: %q", got)
	}
	if got, _ := os.ReadFile(bin + ".prev"); string(got) != "OLD BINARY" {
		t.Errorf("rollback copy = %q", got)
	}
	if !restarted {
		t.Error("restart hook not invoked")
	}
}

func TestApplyRejectsTamperedBinary(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	srv := fakeRelease(t, "v1.1.0", "agent_linux_amd64", []byte("GOOD"), priv, true)
	bin := filepath.Join(t.TempDir(), "agent")
	os.WriteFile(bin, []byte("OLD"), 0o755)

	u := newTestUpdater(t, srv, pub, bin)
	err := u.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(bin); string(got) != "OLD" {
		t.Error("binary must be untouched after failed verification")
	}
}

func TestApplyRejectsBadSignature(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	_, otherPriv, _ := ed25519.GenerateKey(rand.Reader) // signed by the wrong key
	srv := fakeRelease(t, "v1.1.0", "agent_linux_amd64", []byte("GOOD"), otherPriv, false)
	bin := filepath.Join(t.TempDir(), "agent")
	os.WriteFile(bin, []byte("OLD"), 0o755)

	u := newTestUpdater(t, srv, pub, bin)
	err := u.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "signature verification FAILED") {
		t.Fatalf("err = %v", err)
	}
}

func TestApplyRefusesWithoutKey(t *testing.T) {
	u := &Updater{Repo: "o/r", Current: "v1.0.0"}
	err := u.Apply(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no signing public key") {
		t.Fatalf("err = %v", err)
	}
}

func TestRollbackGuard(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "agent")
	os.WriteFile(bin, []byte("BROKEN-NEW"), 0o755)
	os.WriteFile(bin+".prev", []byte("GOOD-OLD"), 0o755)
	g := RollbackGuard{StatePath: filepath.Join(dir, "starts"), BinaryPath: bin}

	t0 := time.Date(2026, 6, 12, 18, 0, 0, 0, time.UTC)
	if rb, _ := g.Check(t0); rb {
		t.Fatal("first start must not roll back")
	}
	if rb, _ := g.Check(t0.Add(20 * time.Second)); rb {
		t.Fatal("second start must not roll back")
	}
	rb, err := g.Check(t0.Add(40 * time.Second))
	if err != nil || !rb {
		t.Fatalf("third rapid start must roll back: %v %v", rb, err)
	}
	if got, _ := os.ReadFile(bin); string(got) != "GOOD-OLD" {
		t.Errorf("binary after rollback = %q", got)
	}
	if _, err := os.Stat(bin + ".prev"); !os.IsNotExist(err) {
		t.Error(".prev must be consumed to avoid ping-pong")
	}
	// State reset: the next start is fresh, no rollback.
	if rb, _ := g.Check(t0.Add(50 * time.Second)); rb {
		t.Error("post-rollback start must not roll back again")
	}
}

func TestRollbackGuardSpreadOutStarts(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "agent")
	os.WriteFile(bin, []byte("FINE"), 0o755)
	os.WriteFile(bin+".prev", []byte("OLD"), 0o755)
	g := RollbackGuard{StatePath: filepath.Join(dir, "starts"), BinaryPath: bin}

	t0 := time.Date(2026, 6, 12, 18, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if rb, _ := g.Check(t0.Add(time.Duration(i) * time.Hour)); rb {
			t.Fatal("slow restarts must never roll back")
		}
	}
	if got, _ := os.ReadFile(bin); string(got) != "FINE" {
		t.Errorf("binary = %q", got)
	}
}
