package rules

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadPackIndexMissingHardFails(t *testing.T) {
	_, err := LoadPackIndex(PackIndexPath(t.TempDir()))
	if err == nil || !strings.Contains(err.Error(), "pack index not found: run localclash rules adapt") {
		t.Fatalf("err = %v, want missing pack index hard fail", err)
	}
}

func TestLoadPackIndexSchemaMismatchHardFails(t *testing.T) {
	path := PackIndexPath(t.TempDir())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encodeErr := gob.NewEncoder(file).Encode(PackIndex{
		SchemaVersion: PackIndexSchemaVersion + 1,
		Caches: map[string]PackCache{
			"blackmatrix7": {
				Source: "blackmatrix7",
				Packs:  []Pack{{ID: "OpenAI", Name: "OpenAI", Renderable: true}},
			},
		},
		Catalog: PackCatalog{Packs: []PackSummary{{Source: "blackmatrix7", Pack: "OpenAI"}}, Details: map[string]PackDetail{}},
		Refs:    map[string]PackRef{PackKey("blackmatrix7", "OpenAI"): {Source: "blackmatrix7", Pack: "OpenAI"}},
	})
	closeErr := file.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}

	_, err = LoadPackIndex(path)
	if err == nil ||
		!strings.Contains(err.Error(), "pack index schema version mismatch") ||
		!strings.Contains(err.Error(), "expected 2, got 3") ||
		!strings.Contains(err.Error(), "run localclash rules adapt") {
		t.Fatalf("err = %v, want schema mismatch hard fail", err)
	}
}
