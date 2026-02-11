package artifact

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store interface {
	SaveScreenshotBase64(ctx context.Context, taskID, payload string) (string, error)
}

type LocalStore struct {
	rootDir string
	baseURL string
}

func NewLocalStore(rootDir, baseURL string) (*LocalStore, error) {
	root := strings.TrimSpace(rootDir)
	if root == "" {
		return nil, errors.New("artifact root dir is required")
	}
	if err := os.MkdirAll(filepath.Join(root, "screenshots"), 0o755); err != nil {
		return nil, fmt.Errorf("create artifact directories: %w", err)
	}

	prefix := strings.TrimSpace(baseURL)
	if prefix == "" {
		prefix = "/artifacts"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimSuffix(prefix, "/")

	return &LocalStore{rootDir: root, baseURL: prefix}, nil
}

func (s *LocalStore) SaveScreenshotBase64(ctx context.Context, taskID, payload string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if strings.TrimSpace(taskID) == "" {
		return "", errors.New("task id is required")
	}
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" {
		return "", errors.New("payload is required")
	}

	decoded, err := decodeBase64(trimmed)
	if err != nil {
		return "", err
	}

	name := fmt.Sprintf("%s-%d.png", sanitizeTaskID(taskID), time.Now().UTC().UnixNano())
	relative := filepath.ToSlash(filepath.Join("screenshots", name))
	path := filepath.Join(s.rootDir, relative)
	tmpPath := path + ".tmp"

	if err := os.WriteFile(tmpPath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write artifact tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("commit artifact: %w", err)
	}

	return s.baseURL + "/" + relative, nil
}

func RootDirFromEnv(value string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return filepath.Join(os.TempDir(), "browseruse-artifacts")
}

func decodeBase64(payload string) ([]byte, error) {
	if strings.HasPrefix(payload, "data:") {
		parts := strings.SplitN(payload, ",", 2)
		if len(parts) != 2 {
			return nil, errors.New("invalid data url payload")
		}
		payload = parts[1]
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, fmt.Errorf("decode base64 payload: %w", err)
	}
	if len(decoded) == 0 {
		return nil, errors.New("decoded payload is empty")
	}
	return decoded, nil
}

func sanitizeTaskID(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	taskID = strings.ReplaceAll(taskID, "/", "_")
	taskID = strings.ReplaceAll(taskID, "..", "_")
	if taskID == "" {
		return "task"
	}
	return taskID
}
