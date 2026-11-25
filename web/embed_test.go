package web

import (
	"io/fs"
	"testing"
)

func TestFSHasIndex(t *testing.T) {
	data, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatalf("index.html is not in the embedded frontend: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("index.html is empty")
	}
}
