//go:build linux

package adc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestContributionIsolation(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer server.Close()
	secret := filepath.Join(t.TempDir(), "secret")
	must(t, os.WriteFile(secret, []byte("synthetic-secret"), 0600))
	t.Setenv("SYNTHETIC_SECRET", "synthetic-secret")
	files := map[string]string{"main.py": "print(2 + 2)\n", "hostile.sh": fmt.Sprintf(`set -eu
test -z "${SYNTHETIC_SECRET-}"
test ! -e %q
test ! -e /run/user
test ! -e /var/run/docker.sock
test ! -e /proc/%d/environ
! curl -fsS --max-time 1 %q
! unshare -Ur true
! touch /usr/adc-escape
! touch /source/escape
test "$(python3 main.py)" = 4
printf isolated
`, secret, os.Getpid(), server.URL)}
	r, err := (contributionExecutor{}).Execute(context.Background(), files, workspaceCommand{Command: "bash hostile.sh"})
	must(t, err)
	if r.ExitCode != 0 || !strings.Contains(r.Output, "isolated") || hits.Load() != 0 {
		t.Fatalf("isolation failed: %+v; hits=%d", r, hits.Load())
	}
	// Each call starts cold; malicious persistent hooks or generated files cannot
	// contaminate the next candidate or review command.
	r, err = (contributionExecutor{}).Execute(context.Background(), files, workspaceCommand{Command: "test ! -e /workspace/escape; echo clean"})
	must(t, err)
	if r.ExitCode != 0 {
		t.Fatal(r)
	}
}
func TestContributionTimeout(t *testing.T) {
	start := time.Now()
	r, err := (contributionExecutor{}).Execute(context.Background(), map[string]string{"x": "x"}, workspaceCommand{Command: "sleep 10", TimeoutSeconds: 1})
	if err == nil && r.ExitCode == 0 {
		t.Fatal("timeout succeeded")
	}
	if time.Since(start) > 8*time.Second {
		t.Fatal("timeout failed")
	}
}
func TestContributionFilesRejectAmbiguousPaths(t *testing.T) {
	for _, files := range []map[string]string{{"../x": "x"}, {".git/config": "x"}, {"x": "a", "x/y": "b"}, {"/x": "x"}, {"x/../y": "x"}, {"x\\y": "x"}, {"x": "\x00"}} {
		if validateContributionFiles(files) == nil {
			t.Fatal(files)
		}
	}
}

func TestContributionAggregateResourceLimits(t *testing.T) {
	x := contributionExecutor{}
	files := map[string]string{"fixture": "synthetic limits"}
	r, err := x.Execute(context.Background(), files, workspaceCommand{Command: `python3 -I -c 'import subprocess
children=[]
try:
 for i in range(100): children.append(subprocess.Popen(["/usr/bin/sleep","10"]))
except OSError: pass
finally:
 for child in children: child.kill()
 for child in children: child.wait()
assert 0 < len(children) < 64, len(children)
print("bounded-processes",len(children))'`})
	must(t, err)
	if r.ExitCode != 0 || !strings.Contains(r.Output, "bounded-processes") {
		t.Fatal(r)
	}
	r, err = x.Execute(context.Background(), files, workspaceCommand{Command: "python3 -I -c 'bytearray(800*1024*1024)'", TimeoutSeconds: 10})
	must(t, err)
	if r.ExitCode == 0 {
		t.Fatal("aggregate memory limit not enforced")
	}
	r, err = x.Execute(context.Background(), files, workspaceCommand{Command: "test $(df -B1 --output=size /workspace | tail -1) -eq 268435456; echo storage-bounded"})
	must(t, err)
	if r.ExitCode != 0 {
		t.Fatal(r)
	}
}
