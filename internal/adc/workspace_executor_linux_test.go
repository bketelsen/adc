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
	"testing"
	"time"
)

func executorFixture(t *testing.T) workspaceExecutor {
	t.Helper()
	if _, err := os.Stat("/usr/bin/bwrap"); err != nil {
		t.Skip("Linux isolation qualification requires bubblewrap")
	}
	x := workspaceExecutor{Workspace: t.TempDir(), Home: t.TempDir()}
	r, err := x.Execute(context.Background(), workspaceCommand{Command: "true"})
	must(t, err)
	if r.ExitCode != 0 {
		t.Fatalf("isolation setup failed (no fallback): %s", r.Output)
	}
	return x
}

func TestWorkspaceIsolationAndUsefulWork(t *testing.T) {
	x := executorFixture(t)
	secret := filepath.Join(t.TempDir(), "synthetic-credential")
	must(t, os.WriteFile(secret, []byte("ADC-SYNTHETIC-SECRET"), 0600))
	must(t, os.Symlink(secret, filepath.Join(x.Workspace, "escape")))
	t.Setenv("ADC_SYNTHETIC_SECRET", "ADC-SYNTHETIC-SECRET")
	t.Setenv("BASH_ENV", secret)
	script := fmt.Sprintf(`set -eux
test -z "${ADC_SYNTHETIC_SECRET-}"
test -z "${BASH_ENV-}"
test ! -r %q
test ! -r escape
test ! -e /proc/%d/environ
test ! -e /run/user
test ! -e /var/run/docker.sock
test ! -e /etc/shadow
test ! -e /home/bjk
test ! -e /proc/1/root%s
test ! -e /proc/self/fd/3
test ! -e /proc/self/fd/4
! touch /usr/adc-should-not-write 2>/dev/null
! unshare -Ur true 2>/dev/null
printf 'print(2 + 2)\n' > sum.py
test "$(python3 sum.py)" = 4
git init -q .
git -c user.name=Fixture -c user.email=fixture@example.test add sum.py
git -c user.name=Fixture -c user.email=fixture@example.test commit -qm fixture
test "$(git show HEAD:sum.py)" = 'print(2 + 2)'
printf 'useful-and-isolated'
`, secret, os.Getpid(), secret)
	r, err := x.Execute(context.Background(), workspaceCommand{Command: script})
	must(t, err)
	if r.ExitCode != 0 || !strings.Contains(r.Output, "useful-and-isolated") {
		t.Fatalf("isolation/useful work failed: %+v", r)
	}
	b, err := os.ReadFile(secret)
	must(t, err)
	if string(b) != "ADC-SYNTHETIC-SECRET" {
		t.Fatal("host secret changed")
	}
}

func TestWorkspaceNetworkingDoesNotBypassADCAuthentication(t *testing.T) {
	x := executorFixture(t)
	s, e, _, _ := fixture(t)
	_, err := s.db.Exec(`INSERT INTO users VALUES('owner','Fixture','owner','unused')`)
	must(t, err)
	server := httptest.NewServer(NewWeb(s, e, false).Handler())
	defer server.Close()
	fixtureHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("outbound-works")) }))
	defer fixtureHTTP.Close()
	script := fmt.Sprintf(`set -eu
test "$(curl -fsS %q)" = outbound-works
test "$(curl -s -o /tmp/body -w '%%{http_code}' %q)" = 303
test "$(curl -s -o /tmp/body -w '%%{http_code}' -d 'org=org&action=approve' %q)" = 303
printf 'network-and-auth-ok'
`, fixtureHTTP.URL, server.URL+"/?org=org", server.URL+"/decision?org=org")
	r, err := x.Execute(context.Background(), workspaceCommand{Command: script})
	must(t, err)
	if r.ExitCode != 0 || !strings.Contains(r.Output, "network-and-auth-ok") {
		t.Fatalf("network/auth: %+v", r)
	}
}

func TestWorkspaceCancellationAndOutputLimit(t *testing.T) {
	x := executorFixture(t)
	r, err := x.Execute(context.Background(), workspaceCommand{Command: "python3 -c 'print(\"x\"*2000000)'"})
	must(t, err)
	if !r.Truncated || len(r.Output) != 1<<20 {
		t.Fatalf("unbounded output: %d", len(r.Output))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = x.Execute(ctx, workspaceCommand{Command: "sleep 1; touch /workspace/survived", TimeoutSeconds: 5})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("cancellation: %v", err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(x.Workspace, "survived")); !os.IsNotExist(err) {
		t.Fatal("descendant survived cancellation")
	}
}

func TestWorkspaceRejectsReplacedMountAndKeepsExitStatus(t *testing.T) {
	x := executorFixture(t)
	r, err := x.Execute(context.Background(), workspaceCommand{Command: "exit 23"})
	must(t, err)
	if r.ExitCode != 23 {
		t.Fatal(r)
	}
	link := filepath.Join(t.TempDir(), "replaced")
	must(t, os.Symlink(x.Workspace, link))
	x.Workspace = link
	if _, err := x.Execute(context.Background(), workspaceCommand{Command: "true"}); err == nil {
		t.Fatal("accepted symlink mount")
	}
}

func TestPrivateStateCannotOverlapRuntimeMounts(t *testing.T) {
	if err := checkPrivateStateMounts(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/usr", "/usr/bin", "/etc", "/"} {
		if checkPrivateStateMounts(path) == nil {
			t.Fatalf("private state at %s accepted", path)
		}
	}
	link := filepath.Join(t.TempDir(), "relocated-state")
	must(t, os.Symlink("/usr/bin", link))
	if checkPrivateStateMounts(link) == nil {
		t.Fatal("relocated state exposed")
	}
}
