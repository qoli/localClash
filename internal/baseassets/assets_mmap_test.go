//go:build linux || darwin

package baseassets

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestExtractAssetsPreservesMappedDatabase(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".runtime", "mihomo", "ASN.mmdb")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	old := bytes.Repeat([]byte("a"), 2*os.Getpagesize())
	if err := os.WriteFile(target, old, 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	mapped, err := syscall.Mmap(int(file.Fd()), 0, len(old), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Munmap(mapped)
	// Fault in the old database, as Mihomo does before an update.
	if !bytes.Equal(mapped, old) {
		t.Fatal("mapped database does not match original")
	}
	archivePath := filepath.Join(t.TempDir(), "assets.tar.gz")
	if err := os.WriteFile(archivePath, testArchive(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := extractTarGz(archivePath, dir); err != nil {
		t.Fatal(err)
	}
	// Reading past the shorter replacement's end must still use the old inode.
	if !bytes.Equal(mapped, old) {
		t.Fatal("asset update changed the running runtime's mapped database")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "asn" {
		t.Fatalf("new database = %q, %v", got, err)
	}
}
