package artifact

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalStoreSavesScreenshot(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStore(tmpDir, "/artifacts")
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	payload := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	url, err := store.SaveScreenshotBase64(context.Background(), "task_1", payload)
	if err != nil {
		t.Fatalf("save screenshot: %v", err)
	}

	if !strings.HasPrefix(url, "/artifacts/screenshots/task_1-") {
		t.Fatalf("unexpected url %s", url)
	}
	name := filepath.Base(url)
	path := filepath.Join(tmpDir, "screenshots", name)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if string(content) != "png-bytes" {
		t.Fatalf("unexpected content %q", string(content))
	}
}
