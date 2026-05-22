package clone

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mascah/nerve/internal/config"
)

// ----- validateEntry --------------------------------------------------------

func TestValidateEntry_EmptyPath(t *testing.T) {
	err := validateEntry(config.CloneFile{Path: ""})
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestValidateEntry_AbsolutePath(t *testing.T) {
	err := validateEntry(config.CloneFile{Path: "/etc/passwd"})
	if err == nil {
		t.Fatal("expected error for absolute path, got nil")
	}
}

func TestValidateEntry_DotDotTraversal(t *testing.T) {
	cases := []string{
		"../etc/passwd",
		"../secret",
		"../../up",
		"foo/../../bar",
	}
	for _, p := range cases {
		err := validateEntry(config.CloneFile{Path: p})
		if err == nil {
			t.Errorf("expected error for traversal path %q, got nil", p)
		}
	}
}

func TestValidateEntry_ValidPaths(t *testing.T) {
	cases := []string{
		".env",
		"subdir/.env",
		"a/b/c",
		"some-file.txt",
	}
	for _, p := range cases {
		err := validateEntry(config.CloneFile{Path: p})
		if err != nil {
			t.Errorf("unexpected error for valid path %q: %v", p, err)
		}
	}
}

// ----- Run: copy behavior ---------------------------------------------------

func TestRun_CopiesFile(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	content := []byte("hello world")
	writeFile(t, filepath.Join(src, ".env"), content, 0o644)

	entries := []config.CloneFile{{Path: ".env"}}
	res, err := Run(src, dst, entries)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(res.Copied) != 1 || res.Copied[0] != ".env" {
		t.Errorf("expected Copied=[.env], got %v", res.Copied)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("expected no skipped, got %v", res.Skipped)
	}

	got, err := os.ReadFile(filepath.Join(dst, ".env"))
	if err != nil {
		t.Fatalf("read dst file: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q want %q", got, content)
	}
}

func TestRun_SkipsMissingOptional(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	entries := []config.CloneFile{{Path: ".npmrc", Required: false}}
	res, err := Run(src, dst, entries)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != ".npmrc" {
		t.Errorf("expected Skipped=[.npmrc], got %v", res.Skipped)
	}
	if len(res.Copied) != 0 {
		t.Errorf("expected no copied, got %v", res.Copied)
	}
}

func TestRun_ErrorsOnMissingRequired(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	entries := []config.CloneFile{{Path: "must-exist.env", Required: true}}
	res, err := Run(src, dst, entries)
	if err == nil {
		t.Fatal("expected error for missing required entry, got nil")
	}
	if !res.HasErrors() {
		t.Error("expected HasErrors() == true")
	}
	if _, ok := res.Errors["must-exist.env"]; !ok {
		t.Errorf("expected error key 'must-exist.env' in Errors map, got %v", res.Errors)
	}
}

func TestRun_PreservesFilePermissions(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Write a file with executable bits.
	wantMode := os.FileMode(0o755)
	writeFile(t, filepath.Join(src, "script.sh"), []byte("#!/bin/sh\n"), wantMode)

	entries := []config.CloneFile{{Path: "script.sh", Kind: config.CloneKindFile}}
	_, err := Run(src, dst, entries)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "script.sh"))
	if err != nil {
		t.Fatalf("stat dst file: %v", err)
	}
	// Mask with umask-independent bits (owner execute is always visible).
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("expected owner-execute bit, got mode %o", info.Mode().Perm())
	}
}

func TestRun_CreatesParentDirectories(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	nested := filepath.Join(src, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir src nested: %v", err)
	}
	writeFile(t, filepath.Join(nested, "config.json"), []byte("{}"), 0o644)

	entries := []config.CloneFile{{Path: "a/b/config.json"}}
	_, err := Run(src, dst, entries)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "a", "b", "config.json")); err != nil {
		t.Errorf("dst nested file missing: %v", err)
	}
}

func TestRun_CopiesDirectory(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	subdir := filepath.Join(src, "config")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeFile(t, filepath.Join(subdir, "settings.yaml"), []byte("key: val\n"), 0o644)
	writeFile(t, filepath.Join(subdir, "other.yaml"), []byte("a: b\n"), 0o644)

	entries := []config.CloneFile{{Path: "config", Kind: config.CloneKindDirectory}}
	res, err := Run(src, dst, entries)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(res.Copied) != 1 || res.Copied[0] != "config" {
		t.Errorf("expected Copied=[config], got %v", res.Copied)
	}
	for _, name := range []string{"settings.yaml", "other.yaml"} {
		if _, err := os.Stat(filepath.Join(dst, "config", name)); err != nil {
			t.Errorf("dst file %s missing: %v", name, err)
		}
	}
}

func TestRun_RejectsAbsolutePathEntry(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	entries := []config.CloneFile{{Path: "/etc/passwd"}}
	res, err := Run(src, dst, entries)
	if err == nil {
		t.Fatal("expected error for absolute path entry, got nil")
	}
	if !res.HasErrors() {
		t.Error("expected HasErrors() == true")
	}
}

func TestRun_RejectsRelativeRoots(t *testing.T) {
	_, err := Run("relative/src", "/tmp/dst", nil)
	if err == nil {
		t.Fatal("expected error for relative srcRoot, got nil")
	}
	_, err = Run("/tmp/src", "relative/dst", nil)
	if err == nil {
		t.Fatal("expected error for relative dstRoot, got nil")
	}
}

func TestRun_KindMismatchDirForFile(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create a directory at the path, but request kind=file.
	if err := os.MkdirAll(filepath.Join(src, "notafile"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	entries := []config.CloneFile{{Path: "notafile", Kind: config.CloneKindFile}}
	res, err := Run(src, dst, entries)
	if err == nil {
		t.Fatal("expected error for dir-vs-file kind mismatch, got nil")
	}
	if _, ok := res.Errors["notafile"]; !ok {
		t.Errorf("expected error key 'notafile' in Errors map")
	}
}

func TestRun_MultipleEntries_PartialFailure(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	writeFile(t, filepath.Join(src, ".env"), []byte("KEY=value"), 0o644)
	// .npmrc is missing and not required — will be skipped.
	// must-exist is missing and required — will be an error.

	entries := []config.CloneFile{
		{Path: ".env"},
		{Path: ".npmrc", Required: false},
		{Path: "must-exist", Required: true},
	}
	res, err := Run(src, dst, entries)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(res.Copied) != 1 || res.Copied[0] != ".env" {
		t.Errorf("expected Copied=[.env], got %v", res.Copied)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != ".npmrc" {
		t.Errorf("expected Skipped=[.npmrc], got %v", res.Skipped)
	}
	if _, ok := res.Errors["must-exist"]; !ok {
		t.Errorf("expected error for 'must-exist'")
	}
}

func TestRun_AutoInfersKindFromDisk(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// When Kind is unset, Run should infer it from os.Lstat.
	writeFile(t, filepath.Join(src, "inferred.txt"), []byte("data"), 0o644)
	entries := []config.CloneFile{{Path: "inferred.txt"}} // no Kind field
	res, err := Run(src, dst, entries)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(res.Copied) != 1 {
		t.Errorf("expected 1 copied, got %v", res.Copied)
	}
}

// ----- helpers --------------------------------------------------------------

func writeFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("writeFile mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
}
