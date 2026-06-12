package dockermon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const listJSON = `[
  {"Id":"abc123","Names":["/web"],"Image":"nginx:1.25","State":"running","Status":"Up 3 hours"},
  {"Id":"def456","Names":["/worker"],"Image":"app:latest","State":"restarting","Status":"Restarting (1) 5 seconds ago"}
]`

const statsJSON = `{
  "cpu_stats":{"cpu_usage":{"total_usage":400000000},"system_cpu_usage":2000000000,"online_cpus":2},
  "precpu_stats":{"cpu_usage":{"total_usage":200000000},"system_cpu_usage":1000000000},
  "memory_stats":{"usage":314572800,"limit":1073741824,"stats":{"inactive_file":14572800}}
}`

func newTestClient(t *testing.T, restartCounts map[string]int) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, listJSON)
	})
	mux.HandleFunc("/containers/abc123/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"RestartCount":%d}`, restartCounts["abc123"])
	})
	mux.HandleFunc("/containers/def456/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"RestartCount":%d}`, restartCounts["def456"])
	})
	mux.HandleFunc("/containers/abc123/stats", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, statsJSON)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return New(srv.Client(), srv.URL)
}

func TestListContainers(t *testing.T) {
	c := newTestClient(t, map[string]int{"abc123": 0, "def456": 3})
	cts, err := c.ListContainers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cts) != 2 {
		t.Fatalf("containers = %d", len(cts))
	}
	web := cts[0]
	if web.Name != "web" || web.State != "running" || web.Image != "nginx:1.25" {
		t.Errorf("web = %+v", web)
	}
	if cts[1].RestartCount != 3 {
		t.Errorf("worker restarts = %d, want 3", cts[1].RestartCount)
	}
}

func TestStats(t *testing.T) {
	c := newTestClient(t, map[string]int{})
	ct := Container{ID: "abc123"}
	if err := c.Stats(context.Background(), &ct); err != nil {
		t.Fatal(err)
	}
	// delta 200M of 1000M system over 2 cpus = 40%
	if ct.CPUPercent < 39.9 || ct.CPUPercent > 40.1 {
		t.Errorf("cpu = %.2f, want 40", ct.CPUPercent)
	}
	if ct.MemUsage != 314572800-14572800 {
		t.Errorf("mem usage = %d (inactive_file must be subtracted)", ct.MemUsage)
	}
	if ct.MemLimit != 1073741824 {
		t.Errorf("mem limit = %d", ct.MemLimit)
	}
}

func TestRestartLoops(t *testing.T) {
	c := New(nil, "http://unused")
	gen := func(n int) []Container {
		return []Container{{ID: "abc", Name: "web", RestartCount: n}, {ID: "xyz", Name: "db", RestartCount: 0}}
	}
	if loops := c.RestartLoops(gen(2)); len(loops) != 0 {
		t.Errorf("first call must only seed baseline, got %+v", loops)
	}
	if loops := c.RestartLoops(gen(2)); len(loops) != 0 {
		t.Errorf("unchanged counts must not report, got %+v", loops)
	}
	loops := c.RestartLoops(gen(4))
	if len(loops) != 1 || loops[0].Name != "web" {
		t.Errorf("grown count must report: %+v", loops)
	}
	if loops := c.RestartLoops(gen(4)); len(loops) != 0 {
		t.Errorf("stable after growth must not re-report, got %+v", loops)
	}
}
