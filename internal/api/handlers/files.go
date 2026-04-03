package handlers

import (
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/hostoria/hostoria-node/internal/config"
	"github.com/hostoria/hostoria-node/internal/filesystem"
	"github.com/hostoria/hostoria-node/internal/server"
)

// FilesDeps bundles dependencies for file handlers.
type FilesDeps struct {
	Servers *server.Manager
}

// ListFiles — GET /api/servers/{uuid}/files?directory=/path
func ListFiles(deps *FilesDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fm, ok := getFileManager(w, r, deps)
		if !ok {
			return
		}
		dir := r.URL.Query().Get("directory")
		if dir == "" {
			dir = "/"
		}
		entries, err := fm.ListDir(dir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "listing directory: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": entries})
	}
}

// ReadFile — GET /api/servers/{uuid}/files/contents?file=/path
func ReadFile(deps *FilesDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fm, ok := getFileManager(w, r, deps)
		if !ok {
			return
		}
		path := r.URL.Query().Get("file")
		if path == "" {
			writeError(w, http.StatusBadRequest, "file query parameter required")
			return
		}
		data, err := fm.ReadFile(path)
		if err != nil {
			writeError(w, http.StatusNotFound, "reading file: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"data": string(data)})
	}
}

// WriteFile — PUT /api/servers/{uuid}/files/write
// Body: {"file": "/path", "content": "..."}
func WriteFile(deps *FilesDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fm, ok := getFileManager(w, r, deps)
		if !ok {
			return
		}
		var body struct {
			File    string `json:"file"`
			Content string `json:"content"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		if body.File == "" {
			writeError(w, http.StatusBadRequest, "file field required")
			return
		}
		if err := fm.WriteFile(body.File, []byte(body.Content)); err != nil {
			writeError(w, http.StatusInternalServerError, "writing file: "+err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// DeleteFiles — POST /api/servers/{uuid}/files/delete
// Body: {"root": "/", "files": ["/path1", "/path2"]}
func DeleteFiles(deps *FilesDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fm, ok := getFileManager(w, r, deps)
		if !ok {
			return
		}
		var body struct {
			Root  string   `json:"root"`
			Files []string `json:"files"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		// Combine root + each file into a single path
		paths := make([]string, 0, len(body.Files))
		for _, f := range body.Files {
			paths = append(paths, filepath.Join(body.Root, f))
		}
		if err := fm.DeletePaths(paths); err != nil {
			writeError(w, http.StatusInternalServerError, "deleting files: "+err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// RenameFile — PUT /api/servers/{uuid}/files/rename
// Body: {"root": "/", "files": [{"from": "old", "to": "new"}]}
func RenameFile(deps *FilesDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fm, ok := getFileManager(w, r, deps)
		if !ok {
			return
		}
		var body struct {
			Root  string `json:"root"`
			Files []struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"files"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		for _, f := range body.Files {
			from := filepath.Join(body.Root, f.From)
			to := filepath.Join(body.Root, f.To)
			if err := fm.Rename(from, to); err != nil {
				writeError(w, http.StatusInternalServerError, "renaming file: "+err.Error())
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// CreateFolder — POST /api/servers/{uuid}/files/create-folder
// Body: {"root": "/", "name": "newfolder"}
func CreateFolder(deps *FilesDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fm, ok := getFileManager(w, r, deps)
		if !ok {
			return
		}
		var body struct {
			Root string `json:"root"`
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
			return
		}
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "name field required")
			return
		}
		dir := filepath.Join(body.Root, body.Name)
		if err := fm.MkDir(dir); err != nil {
			writeError(w, http.StatusInternalServerError, "creating folder: "+err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// DownloadFile — GET /api/servers/{uuid}/files/download?file=/path
// Streams the file directly in the response.
func DownloadFile(deps *FilesDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fm, ok := getFileManager(w, r, deps)
		if !ok {
			return
		}
		path := r.URL.Query().Get("file")
		if path == "" {
			writeError(w, http.StatusBadRequest, "file query parameter required")
			return
		}
		// Set download headers
		filename := filepath.Base(path)
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
		w.Header().Set("Content-Type", "application/octet-stream")
		if err := fm.CopyToWriter(path, w); err != nil {
			// Headers already written — can't send JSON error
			return
		}
	}
}

// --- helpers ---

func getFileManager(w http.ResponseWriter, r *http.Request, deps *FilesDeps) (*filesystem.Manager, bool) {
	uuid := chi.URLParam(r, "uuid")
	srv := deps.Servers.Get(uuid)
	if srv == nil {
		writeError(w, http.StatusNotFound, "server not found")
		return nil, false
	}
	cfg := config.Get()
	return filesystem.New(cfg.System.Data, uuid), true
}
