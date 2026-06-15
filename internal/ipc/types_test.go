package ipc

import (
	"strings"
	"testing"
)

func TestDefaultSocketPath(t *testing.T) {
	path, err := DefaultSocketPath()
	if err != nil {
		t.Fatalf("DefaultSocketPath() returned error: %v", err)
	}

	if !strings.HasSuffix(path, "/.aurelia/aurelia.sock") {
		t.Errorf("expected path to end with /.aurelia/aurelia.sock, got %q", path)
	}

	if !strings.HasPrefix(path, "/") {
		t.Errorf("expected absolute path, got %q", path)
	}
}
