package adc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type githubOutput struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *githubOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if left := (1 << 20) - b.b.Len(); left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		b.b.Write(p)
	}
	return n, nil
}
func (g *githubGateway) remote(owner, repo string) string {
	if g.store.githubFixture != nil && g.store.githubFixture.Remote != nil {
		return g.store.githubFixture.Remote(owner, repo)
	}
	return "https://github.com/" + owner + "/" + repo + ".git"
}

// Only ADC-created bare repositories reach host Git. No worker configuration,
// hooks, helper, environment, remote URL, or credential file is imported.
func (g *githubGateway) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "http.followRedirects=false", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "protocol.file.allow=always", "-C", dir}, args...)...)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "HOME=" + dir, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader", "GIT_CONFIG_VALUE_0=Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+g.token))}
	var output githubOutput
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	text := strings.TrimSpace(output.b.String())
	if err != nil {
		return text, fmt.Errorf("mediated Git failed: %s (%w)", clipped(text, 4000), err)
	}
	return text, nil
}
func (g *githubGateway) bare(ctx context.Context) (string, error) {
	dir, err := os.MkdirTemp(g.store.Dir, "github-")
	if err != nil {
		return "", err
	}
	if _, err = g.git(ctx, dir, "init", "--bare", "--quiet"); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}
func (g *githubGateway) fetch(ctx context.Context, args githubArguments) (any, error) {
	r, err := g.caller(ctx)
	if err != nil {
		return nil, err
	}
	data, _, err := g.request(ctx, "GET", "/repos/"+args.Owner+"/"+args.Repository+"/commits/"+args.Ref, nil)
	if err != nil {
		return nil, err
	}
	var resolved struct{ SHA string }
	if json.Unmarshal(data, &resolved) != nil || !gitObjectID(resolved.SHA) {
		return nil, fmt.Errorf("GitHub did not identify an exact commit")
	}
	dir, err := g.bare(ctx)
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if _, err = g.git(ctx, dir, "fetch", "--no-tags", g.remote(args.Owner, args.Repository), resolved.SHA+":refs/heads/export"); err != nil {
		return nil, err
	}
	bundle := filepath.Join(dir, "export.bundle")
	if _, err = g.git(ctx, dir, "bundle", "create", bundle, "refs/heads/export"); err != nil {
		return nil, err
	}
	g.store.mu.Lock()
	_, err = g.caller(ctx)
	if err == nil {
		actor, _ := ctx.Value(githubActorKey{}).(githubActor)
		var tool GatewayTool
		err = g.store.Get(digest(r.Org+":"+g.connection.ID+":github_fetch"), &tool)
		if err == nil {
			_, err = g.store.authorizeGateway(r.ID, tool, actor.Operation, actor.Arguments)
		}
	}
	g.store.mu.Unlock()
	if err != nil {
		return nil, err
	}
	path, err := githubImportBundle(ctx, r, bundle, resolved.SHA)
	if err != nil {
		return nil, err
	}
	return map[string]string{"Path": path, "Commit": resolved.SHA, "Repository": args.Owner + "/" + args.Repository}, nil
}
