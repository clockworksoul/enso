package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const rebuildLockEnv = "ENSO_RECALL_REBUILD_LOCK_FD"

// Keep the lock file: unlinking it can let two processes lock different inodes.
// The kernel releases flock when the last inherited descriptor closes, even
// after SIGKILL. Thus a dead worker's lock file needs no PID/age heuristic.
func tryRebuildLock(indexPath string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(indexPath+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil
		}
		return nil, err
	}
	return f, nil
}

func waitRebuildLock(ctx context.Context, indexPath string) (*os.File, error) {
	// The detached child receives its already-held lock as descriptor 3. No
	// unlock/relock window exists between scheduling and the actual cold build.
	if os.Getenv(rebuildLockEnv) == "3" {
		os.Unsetenv(rebuildLockEnv)
		return os.NewFile(3, indexPath+".lock"), nil
	}
	for {
		f, err := tryRebuildLock(indexPath)
		if err != nil || f != nil {
			return f, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Caller owns the lock; no active worker can be using these database/WAL files.
func cleanTemporaryIndexes(indexPath string) error {
	entries, err := os.ReadDir(filepath.Dir(indexPath))
	if err != nil {
		return err
	}
	prefix := filepath.Base(indexPath) + ".tmp-"
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			if err := os.RemoveAll(filepath.Join(filepath.Dir(indexPath), entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func startBackgroundRebuild(indexPath, root string) (bool, error) {
	lock, err := tryRebuildLock(indexPath)
	if err != nil || lock == nil {
		return false, err
	}
	defer lock.Close()
	if err := cleanTemporaryIndexes(indexPath); err != nil {
		return false, err
	}
	// A worker may have finished between this caller's staleness check and
	// lock acquisition. Recheck under the lock to avoid a duplicate rebuild.
	fresh, err := indexIsFresh(indexPath, root)
	if err != nil || fresh {
		return false, err
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	cmd := exec.Command(exe, "-root", root, "-index", indexPath, "-rebuild")
	cmd.Env = append(os.Environ(), rebuildLockEnv+"=3")
	cmd.ExtraFiles = []*os.File{lock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// No inherited stdout/stderr pipe: the host can finish as soon as recall
	// returns. Keep worker errors in the derived-cache directory for diagnosis.
	log, err := os.OpenFile(indexPath+".rebuild.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return false, err
	}
	defer log.Close()
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("start worker: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return true, nil
}
