package main

import (
	"path/filepath"
	"testing"
)

func TestPkgListFileFollowsEffectiveConfig(t *testing.T) {
	dir := t.TempDir()
	envCfg := filepath.Join(dir, "env", "config.yaml")
	flagCfg := filepath.Join(dir, "flag", "config.yaml")

	t.Setenv("WPKGET_CONFIG", envCfg)
	if got, want := pkgListFile(""), filepath.Join(dir, "env", "packages.yaml"); got != want {
		t.Errorf("with WPKGET_CONFIG: got %q, want %q", got, want)
	}
	if got, want := pkgListFile(flagCfg), filepath.Join(dir, "flag", "packages.yaml"); got != want {
		t.Errorf("--config must win over WPKGET_CONFIG: got %q, want %q", got, want)
	}
}
