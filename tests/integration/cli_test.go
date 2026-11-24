package integration

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var (
	binary    string
	mockStore string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "calendarctl-it-")
	if err != nil {
		panic(err)
	}
	binary = filepath.Join(dir, "calendarctl")
	mockStore = filepath.Join(dir, "mock-store.json")
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(filepath.Dir(source)))
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/calendarctl")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type result struct {
	stdout string
	stderr string
	code   int
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"CALENDARCTL_CONFIG="+filepath.Join(t.TempDir(), "missing.yaml"),
		"CALENDARCTL_MOCK_STORE="+mockStore,
	)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return result{stdout: out.String(), stderr: errOut.String(), code: code}
}

func withMock(args ...string) []string {
	return append([]string{"--provider", "mock"}, args...)
}

func resetStore(t *testing.T) {
	t.Helper()
	if err := os.Remove(mockStore); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestJSONCommands(t *testing.T) {
	resetStore(t)
	r := run(t, withMock("calendar", "list", "--json")...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	var cals []map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &cals); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, r.stdout)
	}
	if len(cals) < 3 {
		t.Fatalf("expected seeded calendars, got %d", len(cals))
	}

	r = run(t, withMock("event", "list", "--today", "--json")...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	var events []map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &events); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, r.stdout)
	}
	if len(events) == 0 {
		t.Fatal("expected events")
	}
	if _, ok := events[0]["id"].(string); !ok {
		t.Fatal("event missing id")
	}
}

func TestEventLifecycle(t *testing.T) {
	resetStore(t)
	r := run(t, withMock("event", "create",
		"--title", "Integration Event",
		"--start", "2026-12-01T10:00:00+05:30",
		"--end", "2026-12-01T11:00:00+05:30",
		"--calendar", "Work", "--json")...)
	if r.code != 0 {
		t.Fatalf("create exit %d: %s", r.code, r.stderr)
	}
	var created map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("no id returned")
	}

	// Duplicate detection.
	r = run(t, withMock("event", "create",
		"--title", "Integration Event",
		"--start", "2026-12-01T10:00:00+05:30",
		"--end", "2026-12-01T11:00:00+05:30",
		"--calendar", "Work")...)
	if r.code != 5 {
		t.Fatalf("expected duplicate exit 5, got %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "already exists") {
		t.Fatalf("unexpected stderr: %s", r.stderr)
	}

	// Update.
	r = run(t, withMock("event", "update", id, "--title", "Renamed Event", "--json")...)
	if r.code != 0 {
		t.Fatalf("update exit %d: %s", r.code, r.stderr)
	}

	// Show by prefix.
	r = run(t, withMock("event", "show", id[:5], "--json")...)
	if r.code != 0 {
		t.Fatalf("show exit %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "Renamed Event") {
		t.Fatalf("show output: %s", r.stdout)
	}

	// Delete with confirmation skipped.
	r = run(t, withMock("event", "delete", id, "--yes")...)
	if r.code != 0 {
		t.Fatalf("delete exit %d: %s", r.code, r.stderr)
	}
	r = run(t, withMock("event", "show", id)...)
	if r.code != 3 {
		t.Fatalf("expected not-found exit 3, got %d: %s", r.code, r.stderr)
	}
}

func TestAvailabilityJSON(t *testing.T) {
	resetStore(t)
	r := run(t, withMock("availability", "today", "--json")...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	var av struct {
		Events      []any `json:"events"`
		Busy        []any `json:"busy"`
		Free        []any `json:"free"`
		Suggestions []any `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &av); err != nil {
		t.Fatal(err)
	}
	if len(av.Events) == 0 || len(av.Busy) == 0 || len(av.Free) == 0 || len(av.Suggestions) == 0 {
		t.Fatalf("incomplete availability: %+v", av)
	}
}

func TestExitCodesAndStreams(t *testing.T) {
	resetStore(t)
	r := run(t, withMock("event", "create", "--title", "Bad", "--start", "not-a-date")...)
	if r.code != 8 || r.stdout != "" || !strings.Contains(r.stderr, "error:") {
		t.Fatalf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
	}

	r = run(t, withMock("calendar", "list", "--output", "xml")...)
	if r.code != 8 {
		t.Fatalf("expected invalid input exit 8, got %d", r.code)
	}

	r = run(t, withMock("event", "list", "--today", "--quiet")...)
	if r.code != 0 || r.stdout != "" {
		t.Fatalf("quiet mode: %q", r.stdout)
	}
}

func TestConfigCommands(t *testing.T) {
	resetStore(t)
	r := run(t, "config", "path")
	if r.code != 0 || strings.TrimSpace(r.stdout) == "" {
		t.Fatalf("config path: %q %s", r.stdout, r.stderr)
	}
	r = run(t, "config", "show", "--json")
	if r.code != 0 {
		t.Fatalf("config show: %s", r.stderr)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["version"] == nil {
		t.Fatal("missing version")
	}
}

func TestInit(t *testing.T) {
	resetStore(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	cmd := exec.Command(binary, "--provider", "mock", "init",
		"--calendar", "Work", "--timezone", "UTC", "--config", configPath, "--json")
	cmd.Env = append(os.Environ(), "CALENDARCTL_MOCK_STORE="+mockStore)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("init failed: %v\n%s", err, errOut.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if payload["ready"] != true || payload["defaultCalendar"] != "Work" {
		t.Fatalf("unexpected init result: %+v", payload)
	}
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "defaultCalendar: Work") {
		t.Fatalf("config was not written:\n%s", contents)
	}
}

func TestNoColorAndYAML(t *testing.T) {
	resetStore(t)
	r := run(t, withMock("event", "list", "--today", "--no-color")...)
	if r.code != 0 || strings.Contains(r.stdout, "\x1b[") {
		t.Fatalf("color leaked: %q", r.stdout)
	}
	r = run(t, withMock("event", "list", "--today", "--yaml")...)
	if r.code != 0 || !strings.Contains(r.stdout, "title:") {
		t.Fatalf("yaml output: %q %s", r.stdout, r.stderr)
	}
}
