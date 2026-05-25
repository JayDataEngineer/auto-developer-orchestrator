package checkpoint

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Manifest is the on-disk representation of the checkpoint state.
type Manifest struct {
	SessionID string                  `json:"sessionId"`
	Project   string                  `json:"projectDir"`
	Snapshots []*Snapshot             `json:"snapshots"`
	Versions  map[string][]*FileVersion `json:"versions"`
}

// saveVersion writes a file version to disk.
// Stored at {basePath}/files/{relPath}/{hash}@v{version}
func (m *Manager) saveVersion(relPath, hash string, version int, content []byte) error {
	dir := filepath.Join(m.basePath, "files", filepath.Dir(relPath))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	name := filepath.Join(dir, filepath.Base(relPath), fmtVersionName(hash, version))
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return err
	}
	return os.WriteFile(name, content, 0644)
}

// loadVersion reads a file version from disk.
// Caller must NOT hold m.mu — this acquires RLock internally.
func (m *Manager) loadVersion(relPath string, version int) ([]byte, error) {
	m.mu.RLock()
	vs, ok := m.versions[relPath]
	m.mu.RUnlock()
	if !ok {
		return nil, os.ErrNotExist
	}

	var hash string
	for _, fv := range vs {
		if fv.Version == version {
			hash = fv.Hash
			break
		}
	}
	if hash == "" {
		return nil, os.ErrNotExist
	}

	return m.readVersionFile(relPath, hash, version)
}

// loadVersionLocked reads a file version from disk.
// Caller MUST hold at least m.mu.RLock — this does NOT acquire the lock.
func (m *Manager) loadVersionLocked(relPath string, version int) ([]byte, error) {
	vs, ok := m.versions[relPath]
	if !ok {
		return nil, os.ErrNotExist
	}

	var hash string
	for _, fv := range vs {
		if fv.Version == version {
			hash = fv.Hash
			break
		}
	}
	if hash == "" {
		return nil, os.ErrNotExist
	}

	return m.readVersionFile(relPath, hash, version)
}

// readVersionFile reads the on-disk backup file. No locking.
func (m *Manager) readVersionFile(relPath, hash string, version int) ([]byte, error) {
	name := filepath.Join(
		m.basePath, "files", filepath.Dir(relPath),
		filepath.Base(relPath), fmtVersionName(hash, version),
	)
	return os.ReadFile(name)
}

// versionDiskPath returns the full path for a version file.
func (m *Manager) versionDiskPath(relPath string, version int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var hash string
	if vs, ok := m.versions[relPath]; ok {
		for _, fv := range vs {
			if fv.Version == version {
				hash = fv.Hash
				break
			}
		}
	}

	return filepath.Join(
		m.basePath, "files", filepath.Dir(relPath),
		filepath.Base(relPath), fmtVersionName(hash, version),
	)
}

// saveManifest persists the full checkpoint state to manifest.json.
func (m *Manager) saveManifest() error {
	if err := os.MkdirAll(m.basePath, 0755); err != nil {
		return err
	}

	man := &Manifest{
		SessionID: m.sessionID,
		Project:   m.projectDir,
		Snapshots: m.snapshots,
		Versions:  m.versions,
	}

	data, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(m.basePath, "manifest.json"), data, 0644)
}

// loadManifest restores state from manifest.json.
func (m *Manager) loadManifest() error {
	data, err := os.ReadFile(filepath.Join(m.basePath, "manifest.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh session
		}
		return err
	}

	var man Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return err
	}

	m.snapshots = man.Snapshots
	if man.Versions != nil {
		m.versions = man.Versions
	}

	// Rebuild hashCache from latest versions
	for relPath, vs := range m.versions {
		if len(vs) > 0 {
			m.hashCache[relPath] = vs[len(vs)-1].Hash
		}
	}

	return nil
}

func fmtVersionName(hash string, version int) string {
	return hash + "@v" + itoa(version)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 12)
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
