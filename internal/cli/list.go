package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/hookstatus"
	"github.com/mascah/nerve/internal/registry"
)

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [<project>]",
		Short: "List worktrees and allocated ports",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runList,
	}
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

type listEntry struct {
	Project       string         `json:"project"`
	Branch        string         `json:"branch"`
	Path          string         `json:"path"`
	IsMain        bool           `json:"is_main"`
	PrimaryPort   int            `json:"primary_port,omitempty"`
	Offset        int            `json:"offset,omitempty"`
	PortByService map[string]int `json:"ports,omitempty"`
	// HookState reflects backgrounded post_create hooks: "running", "ok", or
	// "failed". Empty for synchronous projects and worktrees with no status.
	HookState string `json:"hook_state,omitempty"`
}

func runList(cmd *cobra.Command, args []string) error {
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return err
	}
	var projects []config.ProjectEntry
	if len(args) == 1 {
		p := reg.FindProject(args[0])
		if p == nil {
			return fmt.Errorf("project %q not registered", args[0])
		}
		projects = []config.ProjectEntry{*p}
	} else {
		projects = reg.Projects
	}

	var entries []listEntry
	for _, p := range projects {
		wts, err := gitutil.ListWorktrees(p.Path)
		if err != nil {
			// Skip project entries pointing at directories that aren't valid repos.
			continue
		}
		cfg, _ := loadProjectConfigOrLightweight(p.Path)
		var allocByPath map[string]registry.Allocation
		if cfg != nil && cfg.IsConfigured() {
			h := registry.Open(p.Path)
			if r, err := h.Read(); err == nil {
				allocByPath = make(map[string]registry.Allocation, len(r.Allocations))
				for _, a := range r.Allocations {
					allocByPath[a.WorktreePath] = a
				}
			}
		}
		for _, w := range wts {
			e := listEntry{Project: p.Name, Branch: w.Branch, Path: w.Path, IsMain: w.Path == p.Path}
			if a, ok := allocByPath[w.Path]; ok && cfg != nil {
				e.Offset = a.Offset
				ports := map[string]int{}
				for i := range cfg.Services {
					svc := &cfg.Services[i]
					ports[svc.ID] = svc.BasePort + cfg.Project.PortOffset + a.Offset
				}
				e.PortByService = ports
				if primary := cfg.PrimaryService(); primary != nil {
					e.PrimaryPort = primary.BasePort + cfg.Project.PortOffset + a.Offset
				}
			}
			if !e.IsMain && cfg != nil {
				if slug := config.Slugify(w.Branch); slug != "" {
					if st, found, _ := hookstatus.Read(p.Path, slug); found {
						e.HookState = string(st.State)
					}
				}
			}
			entries = append(entries, e)
		}
	}

	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		raw, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(raw))
		return nil
	}

	if len(entries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no worktrees")
		return nil
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%-20s %-30s %-7s %-9s %s\n", "PROJECT", "BRANCH", "PORT", "HOOKS", "PATH")
	for _, e := range entries {
		port := "-"
		if e.PrimaryPort > 0 {
			port = fmt.Sprintf("%d", e.PrimaryPort)
		}
		branch := e.Branch
		if e.IsMain {
			branch += " (main)"
		}
		hooks := e.HookState
		if hooks == "" {
			hooks = "-"
		}
		fmt.Fprintf(out, "%-20s %-30s %-7s %-9s %s\n", e.Project, branch, port, hooks, e.Path)
	}
	return nil
}
