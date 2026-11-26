package builder

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateTarStreamIncludesNestedPOSIXPaths(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "cmd")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := CreateTarStream(dir)
	if err != nil {
		t.Fatalf("CreateTarStream failed: %v", err)
	}

	tr := tar.NewReader(r)
	files := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read error: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			b, _ := io.ReadAll(tr)
			files[hdr.Name] = string(b)
		}
	}

	if files["Dockerfile"] != "FROM scratch\n" {
		t.Fatalf("missing or corrupt Dockerfile: %q", files["Dockerfile"])
	}
	if files["cmd/main.go"] != "package main\n" {
		t.Fatalf("missing or corrupt cmd/main.go: %q", files["cmd/main.go"])
	}
}
