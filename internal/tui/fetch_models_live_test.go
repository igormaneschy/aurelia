package tui

import (
	"testing"
	"github.com/igormaneschy/aurelia/internal/ipc"
)

func TestFetchTUIModelCatalog_Live(t *testing.T) {
	path, err := ipc.DefaultSocketPath()
	if err != nil {
		t.Skip(err)
	}
	client := ipc.NewClient(path)
	catalog, err := fetchTUIModelCatalog(client, ipc.ReservedTUIChatID)
	if err != nil {
		t.Skipf("live test requires running daemon: %v", err)
	}
	if catalog.providerCount() < 2 {
		t.Fatalf("providers=%d catalog=%#v", catalog.providerCount(), catalog.byProvider)
	}
}
