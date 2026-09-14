package adc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Only the installation supplies this directory. Queue policy pins its content,
// not a model-selected path. Copy and hash the bytes actually mounted so changing
// the configured directory cannot silently change an existing evaluation.
func snapshotContributionRuntime(ctx context.Context, expected string) (string, func(), error) {
	noop := func() {}
	if expected == "" {
		return "", noop, nil
	}
	if len(expected) != 64 || expected != os.Getenv("ADC_CONTRIBUTION_RUNTIME_ID") {
		return "", noop, fmt.Errorf("pinned admission runtime is unavailable")
	}
	destination, err := os.MkdirTemp("", "adc-public-runtime-")
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { _ = os.RemoveAll(destination) }
	actual, err := copyContributionRuntime(ctx, os.Getenv("ADC_CONTRIBUTION_RUNTIME"), destination)
	if err != nil || actual != expected {
		cleanup()
		return "", noop, fmt.Errorf("pinned admission runtime is unavailable or changed")
	}
	return destination, cleanup, nil
}

// This curated tree must contain only redistributable tools/public dependencies.
// No symlinks, devices, sockets or host caches are admitted. The fixed limit is
// independent of candidate input. Root confines even a concurrently changed tree.
func copyContributionRuntime(ctx context.Context, source, destination string) (string, error) {
	info, err := os.Lstat(source)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("runtime must be a real directory")
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return "", err
	}
	defer root.Close()
	hash := sha256.New()
	var total int64
	count := 0
	err = fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 40000 {
			return fmt.Errorf("runtime exceeds 40000 entries")
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("runtime symlinks are forbidden")
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(destination, name), 0700)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("runtime requires regular files")
		}
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil {
			return err
		}
		if !stat.Mode().IsRegular() {
			return fmt.Errorf("runtime requires regular files")
		}
		if stat.Size() < 0 || stat.Size() > (1<<30)-total {
			return fmt.Errorf("runtime exceeds 1 GiB")
		}
		mode := os.FileMode(0400)
		if stat.Mode()&0111 != 0 {
			mode = 0500
		}
		fmt.Fprintf(hash, "%s\x00%d\x00%d\x00", filepath.ToSlash(name), mode, stat.Size())
		out, err := os.OpenFile(filepath.Join(destination, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		n, err := io.CopyN(io.MultiWriter(hash, out), file, stat.Size())
		closeErr := out.Close()
		total += n
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		// Detect growth rather than hashing only the old prefix.
		var extra [1]byte
		if n, err := file.Read(extra[:]); n != 0 || err != io.EOF {
			return fmt.Errorf("runtime changed during snapshot")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ContributionRuntimeID validates and fingerprints a curated installation tree.
// It does not register it, authorize a queue, or execute any contained program.
func ContributionRuntimeID(ctx context.Context, source string) (string, error) {
	destination, err := os.MkdirTemp("", "adc-runtime-check-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(destination)
	return copyContributionRuntime(ctx, source, destination)
}

// Cheap intake check: never spend a review slot after an installed profile is
// withdrawn. Per-command snapshots still verify content, outside the store lock.
func contributionRuntimeAvailable(id string) bool {
	if id == "" {
		return true
	}
	if len(id) != 64 || id != os.Getenv("ADC_CONTRIBUTION_RUNTIME_ID") {
		return false
	}
	if _, err := hex.DecodeString(id); err != nil {
		return false
	}
	info, err := os.Lstat(os.Getenv("ADC_CONTRIBUTION_RUNTIME"))
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}
