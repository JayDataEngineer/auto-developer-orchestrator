package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// CLIHandler handles safe CLI command execution
type CLIHandler struct {
	logger       *zap.Logger
	allowedCmds  map[string]bool
	projectRoot  string
}

// NewCLIHandler creates a new CLIHandler
func NewCLIHandler(logger *zap.Logger, projectRoot string) *CLIHandler {
	return &CLIHandler{
		logger:      logger,
		projectRoot: projectRoot,
		allowedCmds: map[string]bool{
			"ls":   true,
			"cat":  true,
			"pwd":  true,
			"whoami": true,
			"date": true,
			"uname": true,
		},
	}
}

// CLIRequest represents a CLI command request
type CLIRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Dir     string   `json:"dir,omitempty"`
}

// CLIResponse represents a CLI command response
type CLIResponse struct {
	Success bool     `json:"success"`
	Output  string   `json:"output,omitempty"`
	Error   string   `json:"error,omitempty"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// ExecuteCommand executes a safe CLI command
func (h *CLIHandler) ExecuteCommand(w http.ResponseWriter, r *http.Request) {
	var req CLIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate command is allowed
	if !h.allowedCmds[req.Command] {
		h.logger.Warn("Blocked disallowed command", zap.String("command", req.Command))
		http.Error(w, "Command not allowed", http.StatusForbidden)
		return
	}

	// Sanitize arguments
	sanitizedArgs := h.sanitizeArgs(req.Args)

	// Determine working directory
	workDir := h.projectRoot
	if req.Dir != "" {
		// Prevent directory traversal attacks
		cleanDir := filepath.Clean(req.Dir)
		if strings.HasPrefix(cleanDir, "..") {
			http.Error(w, "Invalid directory", http.StatusBadRequest)
			return
		}
		workDir = cleanDir
	}

	h.logger.Info("Executing command",
		zap.String("command", req.Command),
		zap.Strings("args", sanitizedArgs),
		zap.String("dir", workDir))

	// Execute command
	cmd := exec.Command(req.Command, sanitizedArgs...)
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()

	var response CLIResponse
	response.Command = req.Command
	response.Args = sanitizedArgs

	if err != nil {
		h.logger.Error("Command failed",
			zap.String("command", req.Command),
			zap.Error(err),
			zap.String("output", string(output)))
		response.Success = false
		response.Error = err.Error()
		response.Output = string(output)
		w.WriteHeader(http.StatusBadRequest)
	} else {
		h.logger.Info("Command succeeded",
			zap.String("command", req.Command),
			zap.String("output", string(output)))
		response.Success = true
		response.Output = string(output)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(&response)
}

// ListAllowedCommands returns list of allowed commands
func (h *CLIHandler) ListAllowedCommands(w http.ResponseWriter, r *http.Request) {
	commands := make([]string, 0, len(h.allowedCmds))
	for cmd := range h.allowedCmds {
		commands = append(commands, cmd)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"commands":   commands,
		"projectDir": h.projectRoot,
	})
}

// sanitizeArgs removes dangerous characters from arguments
func (h *CLIHandler) sanitizeArgs(args []string) []string {
	dangerous := []string{";", "|", "&", "$", "`", "(", ")", "{", "}", "[", "]", "<", ">", "!", "\\", "\n", "\r", "'", "\""}
	sanitized := make([]string, 0, len(args))

	for _, arg := range args {
		clean := arg
		for _, char := range dangerous {
			clean = strings.ReplaceAll(clean, char, "")
		}
		if clean != "" {
			sanitized = append(sanitized, clean)
		}
	}

	return sanitized
}

// ReadFile reads a file (safe cat alternative)
func (h *CLIHandler) ReadFile(w http.ResponseWriter, r *http.Request) {
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, "Missing 'path' parameter", http.StatusBadRequest)
		return
	}

	// Prevent directory traversal
	cleanPath := filepath.Clean(filePath)
	if strings.HasPrefix(cleanPath, "..") || strings.HasPrefix(cleanPath, "/") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(h.projectRoot, cleanPath)

	// Check file exists and is within project root
	if !strings.HasPrefix(fullPath, h.projectRoot) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"path":    cleanPath,
		"content": string(content),
	})
}

// ListDirectory lists directory contents (safe ls alternative)
func (h *CLIHandler) ListDirectory(w http.ResponseWriter, r *http.Request) {
	dirPath := r.URL.Query().Get("path")
	if dirPath == "" {
		dirPath = "."
	}

	// Prevent directory traversal
	cleanPath := filepath.Clean(dirPath)
	if strings.HasPrefix(cleanPath, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(h.projectRoot, cleanPath)

	// Check directory is within project root
	if !strings.HasPrefix(fullPath, h.projectRoot) {
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, "Failed to list directory", http.StatusInternalServerError)
		return
	}

	type Entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size,omitempty"`
	}

	result := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		size := int64(0)
		if err == nil {
			size = info.Size()
		}
		result = append(result, Entry{
			Name:  entry.Name(),
			IsDir: entry.IsDir(),
			Size:  size,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"path":    cleanPath,
		"entries": result,
	})
}
