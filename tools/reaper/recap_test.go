package reaper

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// failedRun is the shape of a failed go test ./... log: a test's failure
// printed long before the end, among log lines and passing packages.
const failedRun = `2026/11/13 10:35:11 Relays over WebSocket are taken at /api/v1/relay/live
--- FAIL: TestUploadsResume (3.21s)
    uploads_test.go:88: part 2: got 409, want 204
    --- FAIL: TestUploadsResume/after_a_restart (1.02s)
        uploads_test.go:120: offset 1000, want 2048
2026/11/13 10:36:45 upload 6dbb8f2c: part 1: EOF
ok  	example.org/bam/internal/store	224.212s
panic: runtime error: index out of range [3] with length 3

goroutine 7 [running]:
example.org/bam/internal/labels.fit(...)
FAIL
FAIL	example.org/bam/internal/api	789.769s
ok  	example.org/bam/internal/webui	1.073s
`

func TestRecapRepeatsWhatFailed(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh to run recap.sh with")
	}
	log := filepath.Join(t.TempDir(), "go-test.log")
	if err := os.WriteFile(log, []byte(failedRun), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("sh", "recap.sh", log).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"--- FAIL: TestUploadsResume (3.21s)\n    uploads_test.go:88: part 2: got 409, want 204\n",
		"    --- FAIL: TestUploadsResume/after_a_restart (1.02s)\n        uploads_test.go:120: offset 1000, want 2048\n",
		"panic: runtime error: index out of range [3] with length 3\n\ngoroutine 7 [running]:\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("recap lacks %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"Relays over WebSocket", "internal/store", "upload 6dbb8f2c"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("recap repeats %q:\n%s", unwanted, got)
		}
	}
}
