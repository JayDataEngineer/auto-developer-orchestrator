package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// watcher fingerprints skill files at creation time and reports whether any
// have changed since the previous changed() call. Used by Store.WatchAndReload
// to drive polling-based hot-reload without pulling in fsnotify.
type watcher struct {
	dirs    []string
	mu      sync.Mutex
	state   map[string]string
}

func newWatcher(dirs []string) *watcher {
	w := &watcher{
		dirs:  append([]string(nil), dirs...),
		state: make(map[string]string),
	}
	w.snapshot()
	return w
}

// changed re-snapshots every watched dir and returns true if any file's
// fingerprint was added, removed, or modified since the last call.
func (w *watcher) changed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	fresh := w.fingerprint()
	if len(fresh) != len(w.state) {
		w.state = fresh
		return true
	}
	for k, v := range fresh {
		if old, ok := w.state[k]; !ok || old != v {
			w.state = fresh
			return true
		}
	}
	return false
}

func (w *watcher) snapshot() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.state = w.fingerprint()
}

func (w *watcher) fingerprint() map[string]string {
	out := make(map[string]string)
	for _, dir := range w.dirs {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			lower := strings.ToLower(d.Name())
			if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".markdown") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			out[path] = hashFileInfo(path, info.Size(), info.ModTime().UnixNano())
			return nil
		})
	}
	return out
}

// hashFileInfo combines path + size + mtime into a short fingerprint. The
// goal is cheap change detection, not cryptographic integrity — mtime is
// sufficient for "agent edited the skill file and re-ran" cadence.
func hashFileInfo(path string, size int64, mtime int64) string {
	h := sha256.Sum256([]byte(filepath.ToSlash(path) + "|" + itoa(size) + "|" + itoa(mtime)))
	return hex.EncodeToString(h[:8])
}

// itoa avoids strconv import to keep the watcher dependency-light.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
