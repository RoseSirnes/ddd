package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedDeploymentOverridesOldLocalSettings(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.json"), []byte(`{"url":"https://outro.exemplo.com"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "updates.json"), []byte(`{"repo":"outro/outro"}`), 0600); err != nil {
		t.Fatal(err)
	}
	oldServer, oldUpdate := defaultCentralURL, defaultUpdateRepo
	t.Cleanup(func() { defaultCentralURL = oldServer; defaultUpdateRepo = oldUpdate })
	defaultCentralURL = "https://chat.exemplo.com"
	defaultUpdateRepo = "dono/ddd"
	cfg, err := readServerConfig(dir)
	if err != nil || cfg.URL != defaultCentralURL {
		t.Fatalf("servidor automático: %#v %v", cfg, err)
	}
	upd, err := readUpdateConfig(dir)
	if err != nil || upd.Repo != defaultUpdateRepo {
		t.Fatalf("repo automático: %#v %v", upd, err)
	}
	if err := writeServerConfig(dir, serverConfig{}); err == nil {
		t.Fatal("configuração gerenciada não pode ser alterada")
	}
	if err := writeUpdateConfig(dir, updateConfig{}); err == nil {
		t.Fatal("repo gerenciado não pode ser alterado")
	}
}

func TestInvalidManagedServerFailsInsteadOfFallingBackToLocal(t *testing.T) {
	old := defaultCentralURL
	t.Cleanup(func() { defaultCentralURL = old })
	defaultCentralURL = "http://not-secure.example.org"
	_, err := readServerConfig(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "inválido") {
		t.Fatalf("esperava falhar com URL inválida: %v", err)
	}
}
