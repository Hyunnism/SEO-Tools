package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const StaleLockDuration = 2 * time.Hour

// Lock mencoba mengunci akses situs dengan membuat file .lock secara eksklusif.
// Jika lock sudah ada dan usianya > 2 jam, lock lama dianggap stale dan akan diambil alih.
func Lock(dataDir, siteName string) (func(), error) {
	dir := filepath.Join(dataDir, siteName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	lockPath := filepath.Join(dir, ".lock")

	createLock := func() (*os.File, error) {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		_, _ = fmt.Fprintf(f, "%d\n%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
		_ = f.Close()
		return f, nil
	}

	_, err := createLock()
	if err != nil {
		if os.IsExist(err) {
			// Periksa apakah lock stale
			info, statErr := os.Stat(lockPath)
			if statErr == nil && time.Since(info.ModTime()) > StaleLockDuration {
				// Lock sudah berumur > 2 jam, hapus dan coba kunci ulang
				_ = os.Remove(lockPath)
				if _, retryErr := createLock(); retryErr == nil {
					return func() { _ = os.Remove(lockPath) }, nil
				}
			}

			// Baca isi lock untuk pesan error informatif
			content, _ := os.ReadFile(lockPath)
			lines := strings.Split(strings.TrimSpace(string(content)), "\n")
			pidStr := "unknown"
			timeStr := "unknown"
			if len(lines) > 0 && lines[0] != "" {
				pidStr = lines[0]
			}
			if len(lines) > 1 {
				timeStr = lines[1]
			}
			return nil, fmt.Errorf("situs %q sedang dikunci oleh proses lain (PID: %s, sejak: %s)", siteName, pidStr, timeStr)
		}
		return nil, err
	}

	unlock := func() {
		_ = os.Remove(lockPath)
	}

	return unlock, nil
}

// ReadLockPID membaca PID dari file lock jika ada (berguna untuk testing).
func ReadLockPID(lockPath string) (int, error) {
	b, err := os.ReadFile(lockPath)
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) == 0 {
		return 0, fmt.Errorf("lockfile kosong")
	}
	return strconv.Atoi(lines[0])
}

