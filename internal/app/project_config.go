package app

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/TheFranconianCoder/tachograph/internal/lifecycle"
)

// ProjectConfigFile is the repository-local configuration in the main
// worktree. It applies to every worktree of the project and is additive to the
// user's global config.
const ProjectConfigFile = ".tachograph.yaml"

const (
	projectConfigMaxCopies      = 50
	projectConfigMaxPatternSize = 200
)

// ProjectConfig is the YAML document in ProjectConfigFile. Only the copy list
// is defined today; future settings extend this struct.
type ProjectConfig struct {
	Copy []ProjectCopy `yaml:"copy"`
}

// ProjectCopy is one file or glob copied from the main worktree. Optional
// defaults to true, so a missing local file does not fail the setup.
type ProjectCopy struct {
	Path      string `yaml:"path"`
	Optional  *bool  `yaml:"optional,omitempty"`
	Overwrite bool   `yaml:"overwrite,omitempty"`
}

// UnmarshalYAML accepts both a scalar and a mapping:
//
//   - .env.local
//   - path: .env.local
//     overwrite: true
func (c *ProjectCopy) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		return node.Decode(&c.Path)
	}
	type plain ProjectCopy
	var value plain
	if err := node.Decode(&value); err != nil {
		return err
	}
	// KnownFields does not reach into a custom unmarshaler; reject typos here.
	for index := 0; index+1 < len(node.Content); index += 2 {
		switch node.Content[index].Value {
		case "path", "optional", "overwrite":
		default:
			return fmt.Errorf("unknown field %q", node.Content[index].Value)
		}
	}
	*c = ProjectCopy(value)
	return nil
}

// ProjectConfigPath returns the config location for a main worktree, or "" when
// the project has no main worktree (bare repository).
func ProjectConfigPath(mainWorktree string) string {
	if mainWorktree == "" {
		return ""
	}
	return filepath.Join(mainWorktree, ProjectConfigFile)
}

// LoadProjectConfig reads the repository-local configuration. A missing file is
// not an error: the global config still applies.
func LoadProjectConfig(mainWorktree string) (ProjectConfig, error) {
	path := ProjectConfigPath(mainWorktree)
	if path == "" {
		return ProjectConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ProjectConfig{}, nil
	}
	if err != nil {
		return ProjectConfig{}, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg ProjectConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	// Unknown fields are an error: a typo or a pasted global-config block must
	// not silently configure nothing.
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return ProjectConfig{}, nil
		}
		return ProjectConfig{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return ProjectConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c ProjectConfig) validate() error {
	if len(c.Copy) > projectConfigMaxCopies {
		return fmt.Errorf("at most %d copy entries are allowed", projectConfigMaxCopies)
	}
	seen := make(map[string]bool, len(c.Copy))
	for index, entry := range c.Copy {
		path := strings.TrimSpace(entry.Path)
		if path == "" {
			return fmt.Errorf("copy[%d]: path is required", index)
		}
		if len(path) > projectConfigMaxPatternSize {
			return fmt.Errorf("copy[%d]: path is longer than %d characters", index, projectConfigMaxPatternSize)
		}
		cleaned, err := lifecycle.CleanCopyPattern(path)
		if err != nil {
			return fmt.Errorf("copy[%d]: %w", index, err)
		}
		key := filepath.ToSlash(cleaned)
		if seen[key] {
			return fmt.Errorf("copy[%d]: duplicate path %q", index, path)
		}
		seen[key] = true
	}
	return nil
}

// Actions converts the project copies to lifecycle actions. Callers normally
// use loadProjectActions, which validates the config first.
func (c ProjectConfig) Actions() ([]LifecycleAction, error) {
	actions := make([]LifecycleAction, 0, len(c.Copy))
	for index, entry := range c.Copy {
		cleaned, err := lifecycle.CleanCopyPattern(entry.Path)
		if err != nil {
			return nil, fmt.Errorf("copy[%d]: %w", index, err)
		}
		actions = append(actions, LifecycleAction{Copy: &CopyAction{
			Path:      cleaned,
			Optional:  entry.optional(),
			Overwrite: entry.Overwrite,
		}})
	}
	return actions, nil
}

func (c ProjectCopy) optional() bool {
	if c.Optional == nil {
		return true
	}
	return *c.Optional
}

// loadProjectActions loads and converts the repository-local config.
func loadProjectActions(mainWorktree string) ([]LifecycleAction, error) {
	cfg, err := LoadProjectConfig(mainWorktree)
	if err != nil {
		return nil, err
	}
	return cfg.Actions()
}

// mergeLifecycleActions appends the project actions to the global ones. A
// project entry replaces a global entry with the same path, so a repository can
// refine the optional or overwrite flag of a global copy.
func mergeLifecycleActions(global, project []LifecycleAction) []LifecycleAction {
	merged := make([]LifecycleAction, 0, len(global)+len(project))
	index := make(map[string]int, len(global))
	for _, action := range global {
		if key := copyActionKey(action); key != "" {
			index[key] = len(merged)
		}
		merged = append(merged, action)
	}
	for _, action := range project {
		key := copyActionKey(action)
		if position, ok := index[key]; ok && key != "" {
			merged[position] = action
			continue
		}
		if key != "" {
			index[key] = len(merged)
		}
		merged = append(merged, action)
	}
	return merged
}

func copyActionKey(action LifecycleAction) string {
	if action.Copy == nil {
		return ""
	}
	path := strings.TrimSpace(action.Copy.Path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}
