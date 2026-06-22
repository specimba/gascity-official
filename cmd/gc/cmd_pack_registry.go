package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/gastownhall/gascity/internal/packregistry"
	"github.com/spf13/cobra"
)

func newPackRegistryCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "registry",
		Short: "Manage pack registries",
		Long: `Manage pack registries for discovering and caching remote pack sources.

A pack registry is a collection of known pack sources (git repositories
or HTTP endpoints) that provide agent configurations. Use init to bootstrap
the registry config, add/list/show to manage entries, and refresh to
re-discover available packs from all enabled registries.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newPackRegistryInitCmd(stdout, stderr))
	cmd.AddCommand(newPackRegistryAddCmd(stdout, stderr))
	cmd.AddCommand(newPackRegistryListCmd(stdout, stderr))
	cmd.AddCommand(newPackRegistryShowCmd(stdout, stderr))
	cmd.AddCommand(newPackRegistryRemoveCmd(stdout, stderr))
	cmd.AddCommand(newPackRegistryRefreshCmd(stdout, stderr))
	return cmd
}

func registryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.gc/registries.toml"
	}
	return filepath.Join(home, ".gc", "registries.toml")
}

func defaultRegistrySource() string {
	return "https://github.com/specimba/gascity-packs"
}

func newPackRegistryInitCmd(stdout, stderr io.Writer) *cobra.Command {
	var skipDefault bool
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the pack registry config",
		Long: `Initialize ~/.gc/registries.toml with the default gascity-packs registry.

Creates the registry file with the default specimba/gascity-packs source.
Use --skip to create an empty config without seeding any registry.
Use --force to overwrite an existing config.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if doPackRegistryInit(skipDefault, force, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&skipDefault, "skip", false, "create empty config without seeding defaults")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config")
	return cmd
}

func doPackRegistryInit(skipDefault, force bool, stdout, stderr io.Writer) int {
	path := registryPath()
	if !force {
		if _, statErr := os.Stat(path); statErr == nil {
			fmt.Fprintln(stderr, "gc pack registry init: already initialized (use --force to overwrite)") //nolint:errcheck
			return 1
		}
	}
	r := packregistry.NewRegistry(path)
	if err := r.Init(skipDefault); err != nil {
		fmt.Fprintf(stderr, "gc pack registry init: %v\n", err) //nolint:errcheck
		return 1
	}
	if skipDefault {
		fmt.Fprintln(stdout, "Initialized empty registry config.") //nolint:errcheck
		return 0
	}
	fmt.Fprintf(stdout, "Initialized registry with default source: %s\n", defaultRegistrySource()) //nolint:errcheck
	return 0
}

func newPackRegistryAddCmd(stdout, stderr io.Writer) *cobra.Command {
	var regType string
	cmd := &cobra.Command{
		Use:   "add <name> <source>",
		Short: "Add a pack registry source",
		Long: `Add a pack registry to the local config.

name is a human-readable identifier for the registry.
source is the URL or path to the registry (git repo or HTTP JSON index).
type is the registry transport: "git" or "http" (default "git" for URLs
containing "github.com" or "gitlab.com", otherwise "http").`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if doPackRegistryAdd(args[0], args[1], regType, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&regType, "type", "", "registry transport: git or http")
	return cmd
}

func doPackRegistryAdd(name, source, regType string, stdout, stderr io.Writer) int {
	if regType == "" {
		regType = guessType(source)
	}
	r := packregistry.NewRegistry(registryPath())
	if err := r.Add(name, source, regType); err != nil {
		fmt.Fprintf(stderr, "gc pack registry add: %v\n", err) //nolint:errcheck
		return 1
	}
	fmt.Fprintf(stdout, "Added registry %q (%s)\n", name, source) //nolint:errcheck
	return 0
}

func newPackRegistryListCmd(stdout, stderr io.Writer) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured pack registries",
		Long:  `List all registries in ~/.gc/registries.toml with their source and type.`,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if doPackRegistryList(jsonOutput, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON lines")
	return cmd
}

type registryListJSON struct {
	SchemaVersion string                    `json:"schema_version"`
	Path          string                    `json:"registry_path"`
	Registries    []packregistry.RegistryEntry `json:"registries"`
}

func doPackRegistryList(jsonOutput bool, stdout, stderr io.Writer) int {
	r := packregistry.NewRegistry(registryPath())
	entries, err := r.List()
	if err != nil {
		fmt.Fprintf(stderr, "gc pack registry list: %v\n", err) //nolint:errcheck
		return 1
	}
	if jsonOutput {
		if err := writeCLIJSONLine(stdout, registryListJSON{
			SchemaVersion: "1",
			Path:          registryPath(),
			Registries:    entries,
		}); err != nil {
			fmt.Fprintf(stderr, "gc pack registry list: %v\n", err) //nolint:errcheck
			return 1
		}
		return 0
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "No registries configured. Use 'gc pack registry init' or 'gc pack registry add'.") //nolint:errcheck
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSOURCE\tTYPE") //nolint:errcheck
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, e.Source, e.Type) //nolint:errcheck
	}
	tw.Flush() //nolint:errcheck
	return 0
}

func newPackRegistryShowCmd(stdout, stderr io.Writer) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show details of a pack registry",
		Long:  `Display the source, type, and cached pack list for a registry.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if doPackRegistryShow(args[0], jsonOutput, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON lines")
	return cmd
}

func doPackRegistryShow(name string, jsonOutput bool, stdout, stderr io.Writer) int {
	r := packregistry.NewRegistry(registryPath())
	entry, err := r.Show(name)
	if err != nil {
		fmt.Fprintf(stderr, "gc pack registry show: %v\n", err) //nolint:errcheck
		return 1
	}
	if jsonOutput {
		type showJSON struct {
			SchemaVersion string                    `json:"schema_version"`
			Registry      packregistry.RegistryEntry `json:"registry"`
		}
		if err := writeCLIJSONLine(stdout, showJSON{
			SchemaVersion: "1",
			Registry:      entry,
		}); err != nil {
			fmt.Fprintf(stderr, "gc pack registry show: %v\n", err) //nolint:errcheck
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "Name:    %s\n", entry.Name)      //nolint:errcheck
	fmt.Fprintf(stdout, "Source:  %s\n", entry.Source)    //nolint:errcheck
	fmt.Fprintf(stdout, "Type:    %s\n", entry.Type)      //nolint:errcheck
	fmt.Fprintf(stdout, "Enabled: %v\n", entry.Enabled)   //nolint:errcheck
	packs, _ := r.Refresh()
	for _, p := range packs {
		if p.Source == entry.Source {
			fmt.Fprintf(stdout, "\nPack: %s\n", p.Name)               //nolint:errcheck
			fmt.Fprintf(stdout, "  Description: %s\n", p.Description) //nolint:errcheck
		}
	}
	return 0
}

func newPackRegistryRemoveCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a pack registry",
		Long:  `Remove a registry from ~/.gc/registries.toml.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if doPackRegistryRemove(args[0], stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	return cmd
}

func doPackRegistryRemove(name string, stdout, stderr io.Writer) int {
	r := packregistry.NewRegistry(registryPath())
	if err := r.Remove(name); err != nil {
		fmt.Fprintf(stderr, "gc pack registry remove: %v\n", err) //nolint:errcheck
		return 1
	}
	fmt.Fprintf(stdout, "Removed registry %q\n", name) //nolint:errcheck
	return 0
}

func newPackRegistryRefreshCmd(stdout, stderr io.Writer) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Re-discover packs from all enabled registries",
		Long:  `Refresh the local pack cache by fetching metadata from all enabled registries.`,
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if doPackRegistryRefresh(jsonOutput, stdout, stderr) != 0 {
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output discovered packs as JSON lines")
	return cmd
}

func doPackRegistryRefresh(jsonOutput bool, stdout, stderr io.Writer) int {
	r := packregistry.NewRegistry(registryPath())
	packs, err := r.Refresh()
	if err != nil {
		fmt.Fprintf(stderr, "gc pack registry refresh: %v\n", err) //nolint:errcheck
		return 1
	}
	if jsonOutput {
		if err := writeCLIJSONLine(stdout, map[string]interface{}{
			"schema_version": "1",
			"path":           registryPath(),
			"packs":          packs,
		}); err != nil {
			fmt.Fprintf(stderr, "gc pack registry refresh: %v\n", err) //nolint:errcheck
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "Discovered %d pack(s):\n", len(packs)) //nolint:errcheck
	for _, p := range packs {
		fmt.Fprintf(stdout, "  %s\t%s\n", p.Name, p.Description) //nolint:errcheck
	}
	return 0
}

func guessType(source string) string {
	s := strings.ToLower(source)
	if strings.Contains(s, "github.com") || strings.Contains(s, "gitlab.com") || strings.HasSuffix(s, ".git") {
		return "git"
	}
	return "http"
}
