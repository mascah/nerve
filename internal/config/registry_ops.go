package config

import (
	"fmt"
	"path/filepath"
)

// FindProject returns the registry entry matching name, or nil if absent.
func (r *GlobalRegistry) FindProject(name string) *ProjectEntry {
	for i := range r.Projects {
		if r.Projects[i].Name == name {
			return &r.Projects[i]
		}
	}
	return nil
}

// FindProjectByPath returns the registry entry whose Path equals (after absolute
// resolution) the given path, or nil if none matches.
func (r *GlobalRegistry) FindProjectByPath(path string) *ProjectEntry {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	for i := range r.Projects {
		entryAbs, err := filepath.Abs(r.Projects[i].Path)
		if err != nil {
			continue
		}
		if entryAbs == abs {
			return &r.Projects[i]
		}
	}
	return nil
}

// AddProject appends entry. Returns an error if a project with the same name already exists.
func (r *GlobalRegistry) AddProject(e ProjectEntry) error {
	if r.FindProject(e.Name) != nil {
		return fmt.Errorf("project %q already registered", e.Name)
	}
	r.Projects = append(r.Projects, e)
	return nil
}

// RemoveProject removes the entry with the given name. Returns true if something was removed.
func (r *GlobalRegistry) RemoveProject(name string) bool {
	for i := range r.Projects {
		if r.Projects[i].Name == name {
			r.Projects = append(r.Projects[:i], r.Projects[i+1:]...)
			return true
		}
	}
	return false
}
