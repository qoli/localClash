package rules

import (
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	PackIndexSchemaVersion = 2
	PackIndexFilename      = "index.gob"
)

type PackIndex struct {
	SchemaVersion int
	GeneratedAt   time.Time
	Caches        map[string]PackCache
	Catalog       PackCatalog
	Refs          map[string]PackRef
}

func BuildPackIndex(caches map[string]PackCache) (PackIndex, error) {
	entries, err := catalogEntriesFromCaches(caches)
	if err != nil {
		return PackIndex{}, err
	}
	index := PackIndex{
		SchemaVersion: PackIndexSchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Caches:        copyPackCaches(caches),
		Catalog:       PackCatalog{Details: map[string]PackDetail{}},
		Refs:          map[string]PackRef{},
	}
	for _, entry := range entries {
		summary := packSummary(entry)
		detail := packDetail(entry)
		ref := packRef(entry)
		index.Catalog.Packs = append(index.Catalog.Packs, summary)
		index.Catalog.Details[PackKey(summary.Source, summary.Pack)] = detail
		index.addPackRef(ref)
	}
	return index, nil
}

func WritePackIndex(path string, caches map[string]PackCache) error {
	index, err := BuildPackIndex(caches)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, PackIndexFilename+".*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	encodeErr := gob.NewEncoder(temp).Encode(index)
	closeErr := temp.Close()
	if encodeErr != nil {
		_ = os.Remove(tempPath)
		return encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return closeErr
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func LoadPackIndex(path string) (*PackIndex, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("pack index not found: run localclash rules adapt")
		}
		return nil, err
	}
	defer file.Close()
	var index PackIndex
	if err := gob.NewDecoder(file).Decode(&index); err != nil {
		return nil, err
	}
	if index.SchemaVersion != PackIndexSchemaVersion {
		return nil, fmt.Errorf("pack index schema version mismatch: expected %d, got %d; run localclash rules adapt", PackIndexSchemaVersion, index.SchemaVersion)
	}
	if len(index.Caches) == 0 || len(index.Catalog.Packs) == 0 {
		return nil, fmt.Errorf("pack index is empty: run localclash rules adapt")
	}
	if index.Catalog.Details == nil {
		return nil, fmt.Errorf("pack index is missing details: run localclash rules adapt")
	}
	if index.Refs == nil {
		return nil, fmt.Errorf("pack index is missing refs: run localclash rules adapt")
	}
	return &index, nil
}

func (index *PackIndex) ResolvePackRef(source, pack string) (PackRef, error) {
	if index == nil {
		return PackRef{}, fmt.Errorf("pack index not found: run localclash rules adapt")
	}
	source = strings.TrimSpace(source)
	pack = strings.TrimSpace(pack)
	if source == "" {
		return PackRef{}, fmt.Errorf("pack source is required")
	}
	if pack == "" {
		return PackRef{}, fmt.Errorf("pack name is required")
	}
	ref, ok := index.Refs[PackKey(source, pack)]
	if !ok {
		ref, ok, err := index.resolveGeoSiteSelectorRef(source, pack)
		if err != nil {
			return PackRef{}, err
		}
		if ok {
			return ref, nil
		}
		return PackRef{}, fmt.Errorf("pack %q/%q not found in pack cache", source, pack)
	}
	return ref, nil
}

func (index *PackIndex) resolveGeoSiteSelectorRef(source, selector string) (PackRef, bool, error) {
	base, ok := splitGeoSiteSelector(selector)
	if !ok {
		return PackRef{}, false, nil
	}
	ref, ok := index.Refs[PackKey(source, base)]
	if !ok {
		return PackRef{}, false, nil
	}
	if !packRefIsGeoSite(ref) {
		return PackRef{}, true, fmt.Errorf("pack %q/%q is a GEOSITE selector, but base pack %q is type %q", source, selector, base, ref.Type)
	}
	ref.Pack = selector
	ref.Name = selector
	ref.Type = PackTypeGeoSite
	ref.RenderStrategy = RenderStrategyGeoSite
	ref.RenderRuleTemplate = fmt.Sprintf("GEOSITE,%s,<target>", selector)
	return ref, true, nil
}

func splitGeoSiteSelector(selector string) (string, bool) {
	selector = strings.TrimSpace(selector)
	base, attr, ok := strings.Cut(selector, "@")
	if !ok || strings.TrimSpace(base) == "" || strings.TrimSpace(attr) == "" {
		return "", false
	}
	return strings.TrimSpace(base), true
}

func packRefIsGeoSite(ref PackRef) bool {
	return ref.Type == PackTypeGeoSite || ref.RenderStrategy == RenderStrategyGeoSite
}

func (index *PackIndex) addPackRef(ref PackRef) {
	if index.Refs == nil {
		index.Refs = map[string]PackRef{}
	}
	index.Refs[PackKey(ref.Source, ref.Pack)] = ref
}

func PackKey(source, pack string) string {
	return strings.TrimSpace(source) + "/" + strings.TrimSpace(pack)
}

func PackIndexPath(dir string) string {
	dir = NormalizeOptions(Options{CacheDir: dir}).CacheDir
	return filepath.Join(dir, PackIndexFilename)
}

func DumpPackIndex(path, format string) ([]byte, error) {
	index, err := LoadPackIndex(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "json":
		return json.MarshalIndent(index, "", "  ")
	case "yaml":
		return yaml.Marshal(index)
	default:
		return nil, fmt.Errorf("unsupported index dump format %q: use json or yaml", format)
	}
}

func copyPackCaches(caches map[string]PackCache) map[string]PackCache {
	out := make(map[string]PackCache, len(caches))
	for source, cache := range caches {
		packs := make([]Pack, len(cache.Packs))
		copy(packs, cache.Packs)
		cache.Packs = packs
		out[source] = cache
	}
	return out
}

func removeLegacyPackCacheYAML(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "._") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func packCachesBySource(caches []PackCache) map[string]PackCache {
	out := make(map[string]PackCache, len(caches))
	for _, cache := range caches {
		out[cache.Source] = cache
	}
	return out
}

func sortedPackCacheSources(caches map[string]PackCache) []string {
	sources := make([]string, 0, len(caches))
	for source := range caches {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return sources
}
