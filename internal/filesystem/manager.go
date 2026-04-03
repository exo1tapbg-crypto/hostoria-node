package filesystem

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Manager handles file operations within a server's data directory.
// All paths are sandboxed to the server's root — traversal is prevented.
type Manager struct {
	root string
}

// New creates a Manager for a server's data directory.
func New(dataDir, serverUUID string) *Manager {
	return &Manager{root: filepath.Join(dataDir, serverUUID)}
}

// SafePath resolves a relative path inside the server root and verifies it
// doesn't escape the sandbox. Returns the absolute path or an error.
func (m *Manager) SafePath(relPath string) (string, error) {
	clean := filepath.Clean(filepath.Join(m.root, relPath))
	if !strings.HasPrefix(clean, m.root) {
		return "", fmt.Errorf("path traversal detected: %q escapes server root", relPath)
	}
	return clean, nil
}

// Root returns the server's data directory.
func (m *Manager) Root() string {
	return m.root
}

// EnsureRoot creates the server's data directory if it does not exist.
func (m *Manager) EnsureRoot() error {
	return os.MkdirAll(m.root, 0750)
}

// ListDir returns the contents of the directory at dir (relative to server root).
func (m *Manager) ListDir(dir string) ([]FileInfo, error) {
	abs, err := m.SafePath(dir)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}

	files := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name:     e.Name(),
			IsDir:    e.IsDir(),
			Size:     info.Size(),
			Modified: info.ModTime().Unix(),
			Mode:     info.Mode().String(),
		})
	}
	return files, nil
}

// ReadFile returns the contents of a file at filePath (relative to server root).
func (m *Manager) ReadFile(filePath string) ([]byte, error) {
	abs, err := m.SafePath(filePath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

// WriteFile writes data to filePath (relative to server root), creating parent dirs.
func (m *Manager) WriteFile(filePath string, data []byte) error {
	abs, err := m.SafePath(filePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0750); err != nil {
		return err
	}
	return os.WriteFile(abs, data, 0640)
}

// DeletePaths removes a list of relative paths (files or directories).
func (m *Manager) DeletePaths(paths []string) error {
	for _, p := range paths {
		abs, err := m.SafePath(p)
		if err != nil {
			return err
		}
		if err := os.RemoveAll(abs); err != nil {
			return err
		}
	}
	return nil
}

// Rename renames/moves fromPath to toPath (both relative to server root).
func (m *Manager) Rename(fromPath, toPath string) error {
	absFrom, err := m.SafePath(fromPath)
	if err != nil {
		return err
	}
	absTo, err := m.SafePath(toPath)
	if err != nil {
		return err
	}
	return os.Rename(absFrom, absTo)
}

// MkDir creates a directory at dir (relative to server root).
func (m *Manager) MkDir(dir string) error {
	abs, err := m.SafePath(dir)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, 0750)
}

// CopyToWriter writes file contents to w. Used for download streaming.
func (m *Manager) CopyToWriter(filePath string, w io.Writer) error {
	abs, err := m.SafePath(filePath)
	if err != nil {
		return err
	}
	f, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// FileInfo describes a single file or directory entry.
type FileInfo struct {
	Name     string `json:"name"`
	IsDir    bool   `json:"is_dir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	Mode     string `json:"mode"`
}
