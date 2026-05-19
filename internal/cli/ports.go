package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mascah/nerve/internal/config"
	"github.com/mascah/nerve/internal/gitutil"
	"github.com/mascah/nerve/internal/leases"
	"github.com/mascah/nerve/internal/registry"
)

func newPortsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "ports", Short: "Inspect / manage port registry"}

	listCmd := &cobra.Command{Use: "list", Short: "List allocations", RunE: runPortsList}
	listCmd.Flags().String("project", "", "limit to project (else all)")
	listCmd.Flags().Bool("json", false, "JSON output")

	cleanupCmd := &cobra.Command{Use: "cleanup", Short: "Drop stale allocations", RunE: runPortsCleanup}
	cleanupCmd.Flags().String("project", "", "limit to project (else all)")

	statusCmd := &cobra.Command{Use: "status", Short: "Show pool usage", RunE: runPortsStatus}
	statusCmd.Flags().String("project", "", "limit to project (else all)")
	statusCmd.Flags().Bool("json", false, "JSON output")

	cmd.AddCommand(listCmd, cleanupCmd, statusCmd)
	return cmd
}

func selectProjects(filter string) ([]config.ProjectEntry, error) {
	reg, err := config.LoadGlobalRegistry()
	if err != nil {
		return nil, err
	}
	if filter != "" {
		p := reg.FindProject(filter)
		if p == nil {
			return nil, fmt.Errorf("project %q not registered", filter)
		}
		return []config.ProjectEntry{*p}, nil
	}
	return reg.Projects, nil
}

func runPortsList(cmd *cobra.Command, _ []string) error {
	filter, _ := cmd.Flags().GetString("project")
	projs, err := selectProjects(filter)
	if err != nil {
		return err
	}
	type entry struct {
		Project string `json:"project"`
		Port    string `json:"port"`
		registry.Allocation
	}
	var rows []entry
	for _, p := range projs {
		reg, err := registry.Open(p.Path).Read()
		if err != nil {
			continue
		}
		for port, a := range reg.Allocations {
			rows = append(rows, entry{Project: p.Name, Port: port, Allocation: a})
		}
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		raw, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(raw))
		return nil
	}
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no allocations")
		return nil
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%-20s %-8s %-30s %s\n", "PROJECT", "PORT", "BRANCH", "PATH")
	for _, r := range rows {
		fmt.Fprintf(out, "%-20s %-8s %-30s %s\n", r.Project, r.Port, r.Branch, r.WorktreePath)
	}
	return nil
}

func runPortsCleanup(cmd *cobra.Command, _ []string) error {
	filter, _ := cmd.Flags().GetString("project")
	projs, err := selectProjects(filter)
	if err != nil {
		return err
	}
	totalDropped := 0
	// Per-project: clean stale local allocations AND collect every active
	// worktree path so we can prune the global leases store after.
	var allActive []string
	for _, p := range projs {
		handle := registry.Open(p.Path)
		err := handle.With(func(reg *registry.Registry) error {
			n, err := registry.CleanStale(reg, p.Path)
			if err != nil {
				return err
			}
			totalDropped += n
			if n > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: dropped %d stale\n", p.Name, n)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: %v\n", p.Name, err)
		}
		wts, err := gitutil.ListWorktrees(p.Path)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: list worktrees: %v\n", p.Name, err)
			continue
		}
		for _, w := range wts {
			allActive = append(allActive, w.Path)
		}
	}

	// Prune the global cross-project leases store. We only prune when the user
	// asked for a cleanup across ALL projects (no --project filter), because
	// otherwise we'd drop entries that belong to projects the user didn't ask
	// about and that are still legitimately alive.
	if filter == "" {
		store, err := leases.Open()
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "leases: open: %v\n", err)
		} else if dropped, err := store.Prune(allActive); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "leases: prune: %v\n", err)
		} else if len(dropped) > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "leases: dropped %d orphan(s)\n", len(dropped))
			totalDropped += len(dropped)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "cleanup complete (%d total)\n", totalDropped)
	return nil
}

func runPortsStatus(cmd *cobra.Command, _ []string) error {
	filter, _ := cmd.Flags().GetString("project")
	projs, err := selectProjects(filter)
	if err != nil {
		return err
	}
	type status struct {
		Project string `json:"project"`
		Used    int    `json:"used"`
		Total   int    `json:"total"`
		Start   int    `json:"start"`
		End     int    `json:"end"`
	}
	var rows []status
	for _, p := range projs {
		reg, err := registry.Open(p.Path).Read()
		if err != nil {
			continue
		}
		rows = append(rows, status{
			Project: p.Name,
			Used:    len(reg.Allocations),
			Total:   reg.Pool.Size(),
			Start:   reg.Pool.Start,
			End:     reg.Pool.End,
		})
	}
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		raw, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Fprintln(cmd.OutOrStdout(), string(raw))
		return nil
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%-20s %-8s %s\n", "PROJECT", "USED/TOTAL", "POOL")
	for _, r := range rows {
		fmt.Fprintf(out, "%-20s %d/%-6d  [%d, %d)\n", r.Project, r.Used, r.Total, r.Start, r.End)
	}
	return nil
}
