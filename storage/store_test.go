package storage

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"testing"
)

func TestCASPath(t *testing.T) {
	root := "peerfs_data"
	key := "my_special_picture.png"

	// 1. Manually calculate expected SHA-1 hash to verify correctness
	hash := sha1.Sum([]byte(key))
	hashStr := hex.EncodeToString(hash[:])

	expectedFirstFolder := hashStr[0:2]
	expectedSecondFolder := hashStr[2:4]
	expectedFilename := hashStr[4:]
	expectedPathname := filepath.Join(root, expectedFirstFolder, expectedSecondFolder)
	expectedFullPath := filepath.Join(expectedPathname, expectedFilename)

	// 2. Call CASPath
	pk := CASPath(root, key)

	// 3. Verify Pathname and Filename
	if pk.Pathname != expectedPathname {
		t.Errorf("Pathname mismatch: expected %q, got %q", expectedPathname, pk.Pathname)
	}

	if pk.Filename != expectedFilename {
		t.Errorf("Filename mismatch: expected %q, got %q", expectedFilename, pk.Filename)
	}

	// 4. Verify FullPath()
	if pk.FullPath() != expectedFullPath {
		t.Errorf("FullPath() mismatch: expected %q, got %q", expectedFullPath, pk.FullPath())
	}
}

func TestCASPathDeterminism(t *testing.T) {
	root := "test_storage"
	key := "consistent_file_key"

	// Calling CASPath with identical inputs must always yield identical results
	pk1 := CASPath(root, key)
	pk2 := CASPath(root, key)

	if pk1.Pathname != pk2.Pathname {
		t.Errorf("Pathname not deterministic: %q vs %q", pk1.Pathname, pk2.Pathname)
	}

	if pk1.Filename != pk2.Filename {
		t.Errorf("Filename not deterministic: %q vs %q", pk1.Filename, pk2.Filename)
	}

	if pk1.FullPath() != pk2.FullPath() {
		t.Errorf("FullPath() not deterministic: %q vs %q", pk1.FullPath(), pk2.FullPath())
	}
}

func TestCASPathDifferentKeys(t *testing.T) {
	root := "test_storage"
	key1 := "file_alpha.txt"
	key2 := "file_beta.txt"

	pk1 := CASPath(root, key1)
	pk2 := CASPath(root, key2)

	// Different keys must produce different sharded paths
	if pk1.FullPath() == pk2.FullPath() {
		t.Errorf("Collision detected: two different keys produced identical paths: %q", pk1.FullPath())
	}
}
