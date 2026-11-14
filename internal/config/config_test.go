package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesDeviceIDAndSaves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")

	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store.Data.DeviceID == "" {
		t.Fatal("expected generated device id")
	}
	if len(store.Data.DeviceID) != 32 {
		t.Fatalf("device id length = %d, want 32", len(store.Data.DeviceID))
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not written: %v", err)
	}

	// The device id is stable across loads.
	again, err := Load(path)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if again.Data.DeviceID != store.Data.DeviceID {
		t.Fatalf("device id changed: %q != %q", again.Data.DeviceID, store.Data.DeviceID)
	}
}

func TestSaveAndClearToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	store.Data.Token = "token-123"
	store.Data.LoginMethod = "sms"
	store.Data.LastMobile = "13800001234"
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if loaded.Data.Token != "token-123" || loaded.Data.LoginMethod != "sms" || loaded.Data.LastMobile != "13800001234" {
		t.Fatalf("unexpected reloaded config: %+v", loaded.Data)
	}

	if err := loaded.ClearToken(); err != nil {
		t.Fatalf("ClearToken: %v", err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload after clear: %v", err)
	}
	if reloaded.Data.Token != "" {
		t.Fatalf("token not cleared: %q", reloaded.Data.Token)
	}
	if reloaded.Data.LastMobile != "13800001234" {
		t.Fatalf("preferences lost after clearing token: %+v", reloaded.Data)
	}
}

func TestLoadCorruptConfigBacksUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for corrupt config")
	}
	if store == nil {
		t.Fatal("expected a usable store alongside the warning")
	}
	if store.Data.DeviceID == "" {
		t.Fatal("expected device id to be regenerated")
	}
	backup, readErr := os.ReadFile(path + ".bad")
	if readErr != nil {
		t.Fatalf("backup not created: %v", readErr)
	}
	if string(backup) != "{not json" {
		t.Fatalf("backup content = %q", string(backup))
	}
}

func TestDefaultPathHonoursEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YHCHAT_CONFIG_DIR", dir)
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if path != filepath.Join(dir, "config.json") {
		t.Fatalf("path = %q, want %q", path, filepath.Join(dir, "config.json"))
	}
}

func TestLoadHonoursEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YHCHAT_CONFIG_DIR", dir)
	store, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if store.Path != filepath.Join(dir, "config.json") {
		t.Fatalf("path = %q", store.Path)
	}
}

func TestSaveIsAtomicJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	store.Data.Token = "abc"
	if err := store.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Config
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("saved config is not valid JSON: %v", err)
	}
	if decoded.Token != "abc" || decoded.DeviceID == "" {
		t.Fatalf("unexpected saved config: %+v", decoded)
	}
	// No temp files left behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "config.json" {
			t.Fatalf("unexpected leftover file: %s", e.Name())
		}
	}
}

func TestDefaultPathUsesHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YHCHAT_CONFIG_DIR", "")
	previous := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { userHomeDir = previous })

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join(home, ".yhchat-tui", "config.json")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestLoadMigratesLegacyConfig(t *testing.T) {
	home := t.TempDir()
	legacyDir := t.TempDir()
	t.Setenv("YHCHAT_CONFIG_DIR", "")

	previousHome := userHomeDir
	previousConfig := userConfigDir
	userHomeDir = func() (string, error) { return home, nil }
	userConfigDir = func() (string, error) { return legacyDir, nil }
	t.Cleanup(func() {
		userHomeDir = previousHome
		userConfigDir = previousConfig
	})

	legacyPath := filepath.Join(legacyDir, AppName, "config.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := Config{Token: "legacy-token", DeviceID: "legacy-device", LastEmail: "a@b.com"}
	raw, _ := json.Marshal(legacy)
	if err := os.WriteFile(legacyPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantPath := filepath.Join(home, ".yhchat-tui", "config.json")
	if store.Path != wantPath {
		t.Fatalf("path = %q, want %q", store.Path, wantPath)
	}
	if store.Data.Token != "legacy-token" || store.Data.DeviceID != "legacy-device" || store.Data.LastEmail != "a@b.com" {
		t.Fatalf("legacy config not migrated: %+v", store.Data)
	}
	// The migrated file must exist at the new location.
	migrated, err := Load(wantPath)
	if err != nil {
		t.Fatalf("reload migrated: %v", err)
	}
	if migrated.Data.Token != "legacy-token" {
		t.Fatalf("migrated token = %q", migrated.Data.Token)
	}
}
