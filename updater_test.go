package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVersionComparison(t *testing.T) {
	for _, v := range []struct {
		a, b string
		want bool
	}{
		{"1.1.0", "1.0.0", true}, {"1.10.0", "1.9.9", true}, {"1.0.2", "1.0.10", false},
		{"1.1.0", "1.1.0", false}, {"maliciosa", "1.0.0", false}, {"1.0.0", "", false},
	} {
		if got := isNewVersion(v.a, v.b); got != v.want {
			t.Errorf("%q > %q got %v", v.a, v.b, got)
		}
	}
}
func TestUpdateConfig(t *testing.T) {
	dir := t.TempDir()
	if _, err := readUpdateConfig(dir); err != nil {
		t.Fatal(err)
	}
	if err := writeUpdateConfig(dir, updateConfig{Repo: "usuario/ddd-chat"}); err != nil {
		t.Fatal(err)
	}
	got, err := readUpdateConfig(dir)
	if err != nil || got.Repo != "usuario/ddd-chat" {
		t.Fatalf("got %v: %v", got, err)
	}
	if err := writeUpdateConfig(dir, updateConfig{Repo: "mal.com/../../fora"}); err == nil {
		t.Fatal("invalid repository accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "updates.json")); err != nil {
		t.Fatal(err)
	}
}
func TestSignatureManifest(t *testing.T) {
	// Assinatura pública de exemplo gerada para o teste, sem armazenar chave privada no código.
	raw := `{"version": "1.1.1", "url": "https://github.com/exemplo/ddd/releases/download/v1.1.1/DDD.exe", "sha256": "0000000000000000000000000000000000000000000000000000000000000000", "signature": "OdzFy7FUhoGnb6S8SiPW9i14BPph6MTAYs9gL61DJ8jgnrpDnj/QSbyyEJqD1XpcXe2bCRfyjnvCjq7iOPv+Bw=="}`
	var m updateManifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if err := verifiedManifest(m); err != nil {
		t.Fatal(err)
	}
	m.URL = "https://github.com/outro/ddd/releases/download/v1.1.1/DDD.exe"
	if err := verifiedManifest(m); err == nil {
		t.Fatal("assinatura alterada foi aceita")
	}
	m.URL = "http://github.com/outro/ddd/releases/download/v1.1.1/DDD.exe"
	if err := verifiedManifest(m); err == nil {
		t.Fatal("HTTP foi aceito")
	}
}
