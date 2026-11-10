package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSameInputSameBytesWhateverTheFileMtime(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bootstrap")
	content := bytes.Repeat([]byte("guard binary "), 1000)
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1794300000, 0)

	read := func() []byte {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	_ = os.Chtimes(p, time.Unix(1, 0), time.Unix(1, 0))
	z1, err := Zip("bootstrap", read(), at)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(p, time.Now(), time.Now())
	z2, _ := Zip("bootstrap", read(), at)
	if !bytes.Equal(z1, z2) {
		t.Fatal("same content and mtime must give identical bytes")
	}
	z3, _ := Zip("bootstrap", read(), at.Add(2*time.Second))
	if bytes.Equal(z1, z3) {
		t.Fatal("the recorded mtime should be part of the bytes")
	}
}

func TestEntryShape(t *testing.T) {
	z, err := Zip("bootstrap", []byte("x"), time.Unix(1794300000, 0))
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(z), int64(len(z)))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.File) != 1 {
		t.Fatalf("want one entry, got %d", len(r.File))
	}
	f := r.File[0]
	if f.Name != "bootstrap" || f.Mode().Perm() != 0o755 || len(f.Extra) != 0 {
		t.Fatalf("entry %q mode %v extra %d bytes", f.Name, f.Mode(), len(f.Extra))
	}
	rc, _ := f.Open()
	b, _ := io.ReadAll(rc)
	if string(b) != "x" {
		t.Fatal("content")
	}
}
