package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
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
	// Chave gerada em tempo de teste; nunca guardar a chave de produção nos testes.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil { t.Fatal(err) }
	key := base64.StdEncoding.EncodeToString(pub)
	m := updateManifest{
		Version: "1.1.1",
		URL: "https://github.com/exemplo/ddd/releases/download/v1.1.1/DDD.exe",
		SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
	}
	payload := []byte(m.Version + "\n" + m.URL + "\n" + m.SHA256)
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))
	if err := verifyManifestWithPublicKey(m, key); err != nil { t.Fatal(err) }
	if err := verifiedManifest(m); err == nil { t.Fatal("assinatura de outra chave aceita") }
	m.URL = "https://github.com/outro/ddd/releases/download/v1.1.1/DDD.exe"
	if err := verifyManifestWithPublicKey(m, key); err == nil { t.Fatal("assinatura alterada foi aceita") }
	m.URL = "http://github.com/outro/ddd/releases/download/v1.1.1/DDD.exe"
	if err := verifyManifestWithPublicKey(m, key); err == nil { t.Fatal("HTTP foi aceito") }
}
