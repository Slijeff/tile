package server

import (
	"os"
	"testing"
)

func TestLoadConfigWritesDefaultsWhenMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	if got := loadConfig(); got != defaultConfig {
		t.Fatalf("loadConfig() = %+v, want defaults %+v", got, defaultConfig)
	}
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config.yaml was not written: %v", err)
	}
}
