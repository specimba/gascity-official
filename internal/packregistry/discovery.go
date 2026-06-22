package packregistry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

func discoverHTTP(source string) ([]PackEntry, error) {
	resp, err := http.Get(source)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, source)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var idx struct {
		Packs []PackEntry `json:"packs"`
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing JSON index: %w", err)
	}
	for i := range idx.Packs {
		if idx.Packs[i].Source == "" {
			idx.Packs[i].Source = source
		}
	}
	return idx.Packs, nil
}

func discoverGit(source string) ([]PackEntry, error) {
	tmp, err := os.MkdirTemp("", "packregistry-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	repoDir := filepath.Join(tmp, "repo")
	if err := run("git", "clone", "--depth=1", "--filter=blob:none", "--sparse", source, repoDir); err != nil {
		return nil, err
	}
	if err := runInDir(repoDir, "git", "sparse-checkout", "set", "."); err != nil {
		return nil, err
	}
	var packs []PackEntry
	root := filepath.Join(repoDir, ".")
	if _, err := os.Stat(filepath.Join(root, "pack.toml")); err == nil {
		if p, err := readPackTOML(filepath.Join(root, "pack.toml"), source, ""); err == nil {
			packs = append(packs, *p)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		packPath := filepath.Join(root, e.Name())
		if _, err := os.Stat(filepath.Join(packPath, "pack.toml")); err != nil {
			continue
		}
		p, err := readPackTOML(filepath.Join(packPath, "pack.toml"), source, e.Name())
		if err != nil {
			continue
		}
		packs = append(packs, *p)
	}
	return packs, nil
}

func readPackTOML(path, source, name string) (*PackEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta struct {
		Name        string `toml:"name"`
		Description string `toml:"description"`
	}
	if err := toml.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if n := filepath.Base(filepath.Dir(path)); n != "" && n != "." {
		name = n
	}
	if meta.Name == "" {
		meta.Name = name
	}
	return &PackEntry{
		Name:        meta.Name,
		Description: meta.Description,
		Source:      source,
		Path:        filepath.Base(filepath.Dir(path)),
	}, nil
}

func run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

func runInDir(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, bytes.TrimSpace(out))
	}
	return nil
}
