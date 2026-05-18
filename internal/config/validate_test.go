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
