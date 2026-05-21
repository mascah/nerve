package config

import (
	"strings"
	"testing"
)

func TestValidateDefaults(t *testing.T) {
	cfg := Defaults()
	if err := Validate(&cfg); err != nil {
		t.Fatalf("defaults should validate, got: %v", err)
	}
}

func TestValidateDuplicateServiceID(t *testing.T) {
	cfg := Defaults()
	cfg.Services = []Service{
		{ID: "django", BasePort: 8000, EnvKey: "A"},
		{ID: "django", BasePort: 8001, EnvKey: "B"},
	}
	err := Validate(&cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate id") {
		t.Errorf("expected duplicate id error, got %v", err)
	}
}

func TestValidateDuplicateEnvKey(t *testing.T) {
	cfg := Defaults()
	cfg.Services = []Service{
		{ID: "django", BasePort: 8000, EnvKey: "PORT"},
		{ID: "postgres", BasePort: 5432, EnvKey: "PORT"},
	}
	err := Validate(&cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate env_key") {
		t.Errorf("expected duplicate env_key error, got %v", err)
	}
}

func TestValidateMultiplePrimaries(t *testing.T) {
	cfg := Defaults()
	cfg.Services = []Service{
		{ID: "a", BasePort: 8000, EnvKey: "A", Primary: true},
		{ID: "b", BasePort: 8001, EnvKey: "B", Primary: true},
	}
	err := Validate(&cfg)
	if err == nil || !strings.Contains(err.Error(), "primary") {
		t.Errorf("expected multiple-primary error, got %v", err)
	}
}

func TestValidateCloneFileEscape(t *testing.T) {
	cfg := Defaults()
	cfg.CloneFiles = []CloneFile{{Path: "../escape", Kind: CloneKindFile}}
	err := Validate(&cfg)
	if err == nil || !strings.Contains(err.Error(), "inside the repo") {
		t.Errorf("expected escape rejection, got %v", err)
	}
}

func TestValidateVarsOK(t *testing.T) {
	cfg := Defaults()
	cfg.Services = []Service{{ID: "django", BasePort: 8000, EnvKey: "DJANGO_PORT", Primary: true}}
	cfg.Vars = []Var{{EnvKey: "WORKTREE_ID", Value: "{{branch}}"}}
	if err := Validate(&cfg); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}

func TestValidateVarMissingValue(t *testing.T) {
	cfg := Defaults()
	cfg.Vars = []Var{{EnvKey: "WORKTREE_ID"}}
	err := Validate(&cfg)
	if err == nil || !strings.Contains(err.Error(), "value is required") {
		t.Errorf("expected value-required error, got %v", err)
	}
}

func TestValidateVarMissingEnvKey(t *testing.T) {
	cfg := Defaults()
	cfg.Vars = []Var{{Value: "x"}}
	err := Validate(&cfg)
	if err == nil || !strings.Contains(err.Error(), "env_key is required") {
		t.Errorf("expected env_key-required error, got %v", err)
	}
}

func TestValidateVarConflictsWithService(t *testing.T) {
	cfg := Defaults()
	cfg.Services = []Service{{ID: "django", BasePort: 8000, EnvKey: "SHARED", Primary: true}}
	cfg.Vars = []Var{{EnvKey: "SHARED", Value: "x"}}
	err := Validate(&cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate env_key") {
		t.Errorf("expected duplicate env_key error (var vs service), got %v", err)
	}
}

func TestValidateVarDuplicate(t *testing.T) {
	cfg := Defaults()
	cfg.Vars = []Var{
		{EnvKey: "WORKTREE_ID", Value: "a"},
		{EnvKey: "WORKTREE_ID", Value: "b"},
	}
	err := Validate(&cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate env_key") {
		t.Errorf("expected duplicate env_key error (var vs var), got %v", err)
	}
}

func TestValidateVersionZeroCoerced(t *testing.T) {
	cfg := Defaults()
	cfg.Version = 0
	if err := Validate(&cfg); err != nil {
		t.Fatalf("version 0 should coerce to current and validate, got: %v", err)
	}
	if cfg.Version != CurrentConfigVersion {
		t.Errorf("version 0 should coerce to %d, got %d", CurrentConfigVersion, cfg.Version)
	}
}

func TestValidateOlderVersionAccepted(t *testing.T) {
	cfg := Defaults()
	// CurrentConfigVersion is 1; any version <= current is forward-compatible.
	cfg.Version = CurrentConfigVersion
	if err := Validate(&cfg); err != nil {
		t.Fatalf("version <= current should be accepted, got: %v", err)
	}
}

func TestValidateFutureVersionRejected(t *testing.T) {
	cfg := Defaults()
	cfg.Version = CurrentConfigVersion + 1
	err := Validate(&cfg)
	if err == nil {
		t.Fatalf("expected rejection of future config version")
	}
	for _, want := range []string{"newer version of nerve", "upgrade nerve"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err.Error(), want)
		}
	}
}
