package packregistry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
)

const (
	DefaultRegistriesPath = "~/.gc/registries.toml"
	DefaultVersion        = 1
)

var (
	ErrNotFound     = errors.New("registry not found")
	ErrAlreadyExist = errors.New("registry already exists")
)

type RegistryEntry struct {
	Name    string `toml:"name"`
	Source  string `toml:"source"`
	Type    string `toml:"type"`
	Enabled bool   `toml:"enabled"`
}

type PackEntry struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Source      string `toml:"source"`
	Path        string `toml:"path"`
}

type RegistryFile struct {
	Version    uint64           `toml:"version"`
	Registries []RegistryEntry  `toml:"registries"`
	PackCache  []PackEntry      `toml:"pack_cache,omitempty"`
}

type Registry struct {
	mu     sync.RWMutex
	path   string
	file   RegistryFile
	loaded bool
}

func NewRegistry(path string) *Registry {
	return &Registry{path: path}
}

func defaultRegistriesPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return DefaultRegistriesPath
	}
	return filepath.Join(home, ".gc", "registries.toml")
}

func (r *Registry) ensureLoaded() error {
	r.mu.RLock()
	if r.loaded {
		r.mu.RUnlock()
		return nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded {
		return nil
	}
	path := expandPath(r.path)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		r.file = RegistryFile{Version: DefaultVersion}
		r.loaded = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading registry: %w", err)
	}
	if err := toml.Unmarshal(data, &r.file); err != nil {
		return fmt.Errorf("parsing registry: %w", err)
	}
	if r.file.Version == 0 {
		r.file.Version = DefaultVersion
	}
	r.loaded = true
	return nil
}

func (r *Registry) saveLocked() error {
	path := expandPath(r.path)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating registry dir: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating registry file: %w", err)
	}
	enc := toml.NewEncoder(f)
	enc.Indent = "  "
	if err := enc.Encode(r.file); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("encoding registry: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("syncing registry: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("closing registry: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming registry: %w", err)
	}
	return nil
}

func expandPath(p string) string {
	if len(p) > 0 && p[0] == '~' {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func (r *Registry) List() ([]RegistryEntry, error) {
	if err := r.ensureLoaded(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RegistryEntry, len(r.file.Registries))
	copy(out, r.file.Registries)
	return out, nil
}

func (r *Registry) Add(name, source, typ string) error {
	if err := r.ensureLoaded(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.file.Registries {
		if e.Name == name {
			return fmt.Errorf("%w: %s", ErrAlreadyExist, name)
		}
		if e.Source == source {
			return fmt.Errorf("%w: %s", ErrAlreadyExist, source)
		}
	}
	r.file.Registries = append(r.file.Registries, RegistryEntry{
		Name: name, Source: source, Type: typ, Enabled: true,
	})
	return r.saveLocked()
}

func (r *Registry) Show(name string) (RegistryEntry, error) {
	if err := r.ensureLoaded(); err != nil {
		return RegistryEntry{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.file.Registries {
		if e.Name == name {
			return e, nil
		}
	}
	return RegistryEntry{}, fmt.Errorf("%w: %s", ErrNotFound, name)
}

func (r *Registry) Remove(name string) error {
	if err := r.ensureLoaded(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	filtered := r.file.Registries[:0]
	found := false
	for _, e := range r.file.Registries {
		if e.Name == name {
			found = true
			continue
		}
		filtered = append(filtered, e)
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	r.file.Registries = filtered
	return r.saveLocked()
}

func (r *Registry) Init(skipDefault bool) error {
	if err := r.ensureLoaded(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.file.Registries) > 0 {
		return fmt.Errorf("registry already initialized with %d entries", len(r.file.Registries))
	}
	r.file = RegistryFile{Version: DefaultVersion}
	if !skipDefault {
		r.file.Registries = []RegistryEntry{{
			Name:    "gascity-packs",
			Source:  "https://github.com/specimba/gascity-packs",
			Type:    "git",
			Enabled: true,
		}}
	}
	return r.saveLocked()
}

func (r *Registry) Refresh() ([]PackEntry, error) {
	if err := r.ensureLoaded(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	entries := make([]RegistryEntry, len(r.file.Registries))
	copy(entries, r.file.Registries)
	r.mu.RUnlock()

	var packs []PackEntry
	for _, reg := range entries {
		if !reg.Enabled {
			continue
		}
		p, err := discover(reg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "discovery warning for %s: %v\n", reg.Name, err)
			continue
		}
		packs = append(packs, p...)
	}

	r.mu.Lock()
	r.file.PackCache = packs
	if err := r.saveLocked(); err != nil {
		return nil, err
	}
	return packs, nil
}

func discover(reg RegistryEntry) ([]PackEntry, error) {
	switch reg.Type {
	case "git":
		return discoverGit(reg.Source)
	case "http", "":
		return discoverHTTP(reg.Source)
	default:
		return nil, fmt.Errorf("unsupported registry type %q", reg.Type)
	}
}
