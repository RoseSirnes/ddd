package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const appVersion = "1.5.0"

// Valores definidos somente pela publicação oficial. O usuário não os configura.
var defaultUpdateRepo string

const maxUpdateSize int64 = 100 << 20 // 100 MiB

var githubRepoPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,38}/[a-zA-Z0-9_.-]{1,100}$`)
var semverPattern = regexp.MustCompile(`^([0-9]+)\.([0-9]+)\.([0-9]+)$`)

type updateManifest struct {
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

type updateConfig struct {
	Repo string `json:"repo"`
}

func parseVersion(v string) ([3]int64, error) {
	var out [3]int64
	m := semverPattern.FindStringSubmatch(v)
	if m == nil {
		return out, errors.New("versão inválida")
	}
	for i := 0; i < 3; i++ {
		var n int64
		for _, c := range m[i+1] {
			n = n*10 + int64(c-'0')
			if n > 999999 {
				return out, errors.New("versão grande demais")
			}
		}
		out[i] = n
	}
	return out, nil
}
func isNewVersion(v, current string) bool {
	newV, e1 := parseVersion(v)
	oldV, e2 := parseVersion(current)
	if e1 != nil || e2 != nil {
		return false
	}
	for i := 0; i < 3; i++ {
		if newV[i] != oldV[i] {
			return newV[i] > oldV[i]
		}
	}
	return false
}
func validateRepo(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	if !githubRepoPattern.MatchString(repo) || strings.Contains(repo, "..") || strings.HasSuffix(repo, ".") {
		return "", errors.New("Use um repositório GitHub no formato usuario/repositorio.")
	}
	return repo, nil
}
func readUpdateConfig(dir string) (updateConfig, error) {
	if defaultUpdateRepo != "" {
		repo, err := validateRepo(defaultUpdateRepo)
		if err != nil {
			return updateConfig{}, fmt.Errorf("fonte oficial de atualizações inválida: %w", err)
		}
		return updateConfig{Repo: repo}, nil
	}
	var c updateConfig
	raw, err := os.ReadFile(filepath.Join(dir, "updates.json"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return updateConfig{}, err
	}
	if c.Repo != "" {
		c.Repo, err = validateRepo(c.Repo)
	}
	return c, err
}
func writeUpdateConfig(dir string, c updateConfig) error {
	if defaultUpdateRepo != "" {
		return errors.New("a fonte de atualizações do DDD é automática e não pode ser alterada")
	}
	if c.Repo != "" {
		if _, err := validateRepo(c.Repo); err != nil {
			return err
		}
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := filepath.Join(dir, "updates.json.tmp")
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "updates.json"))
}
func manifestURL(repo string) string {
	return "https://github.com/" + repo + "/releases/latest/download/ddd-update.json"
}
func verifiedManifest(m updateManifest) error {
	return verifyManifestWithPublicKey(m, updatePublicKey)
}

func verifyManifestWithPublicKey(m updateManifest, keyBase64 string) error {
	if _, err := parseVersion(m.Version); err != nil {
		return err
	}
	if len(m.SHA256) != 64 {
		return errors.New("hash da atualização inválido")
	}
	hash, err := hex.DecodeString(m.SHA256)
	if err != nil || len(hash) != sha256.Size {
		return errors.New("hash da atualização inválido")
	}
	target, err := url.Parse(m.URL)
	if err != nil || target.Scheme != "https" || target.Host != "github.com" || !strings.HasSuffix(strings.ToLower(target.Path), "/ddd.exe") {
		return errors.New("endereço do arquivo não é um download HTTPS do GitHub")
	}
	pub, err := base64.StdEncoding.DecodeString(keyBase64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("chave pública inválida")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("assinatura inválida")
	}
	payload := []byte(m.Version + "\n" + m.URL + "\n" + strings.ToLower(m.SHA256))
	if !ed25519.Verify(ed25519.PublicKey(pub), payload, sig) {
		return errors.New("atualização rejeitada: assinatura de segurança não confere")
	}
	return nil
}
func updateHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("redirecionamento inseguro")
		}
		if len(via) > 10 {
			return errors.New("excesso de redirecionamentos")
		}
		return nil
	}}
}
func fetchManifest(repo string) (updateManifest, error) {
	var m updateManifest
	req, err := http.NewRequest("GET", manifestURL(repo), nil)
	if err != nil {
		return m, err
	}
	req.Header.Set("User-Agent", "DDD-Updater/"+appVersion)
	res, err := updateHTTPClient(12 * time.Second).Do(req)
	if err != nil {
		return m, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return m, fmt.Errorf("não foi possível consultar o GitHub (HTTP %d)", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 16*1024+1))
	if err != nil {
		return m, err
	}
	if len(data) > 16*1024 {
		return m, errors.New("manifesto grande demais")
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("manifesto inválido: %w", err)
	}
	if err := verifiedManifest(m); err != nil {
		return m, err
	}
	return m, nil
}
func verifyFile(path, digest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, maxUpdateSize+1))
	if err != nil {
		return err
	}
	if n > maxUpdateSize {
		return errors.New("executável maior que o limite")
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(digest) {
		return errors.New("arquivo recebido não corresponde ao hash assinado")
	}
	return nil
}
func downloadUpdate(m updateManifest, dir string) (string, error) {
	folder := filepath.Join(dir, "updater")
	if err := os.MkdirAll(folder, 0700); err != nil {
		return "", err
	}
	dest, err := os.CreateTemp(folder, "ddd-update-*.exe")
	if err != nil {
		return "", err
	}
	name := dest.Name()
	defer func() { dest.Close() }()
	cleanup := func(e error) (string, error) { os.Remove(name); return "", e }
	req, _ := http.NewRequest("GET", m.URL, nil)
	req.Header.Set("User-Agent", "DDD-Updater/"+appVersion)
	res, err := updateHTTPClient(120 * time.Second).Do(req)
	if err != nil {
		return cleanup(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return cleanup(fmt.Errorf("não foi possível baixar a atualização (HTTP %d)", res.StatusCode))
	}
	if res.ContentLength > maxUpdateSize {
		return cleanup(errors.New("atualização grande demais"))
	}
	n, err := io.Copy(dest, io.LimitReader(res.Body, maxUpdateSize+1))
	if err != nil {
		return cleanup(err)
	}
	if n > maxUpdateSize {
		return cleanup(errors.New("atualização grande demais"))
	}
	if err := dest.Close(); err != nil {
		return cleanup(err)
	}
	f, err := os.Open(name)
	if err != nil {
		return cleanup(err)
	}
	header := make([]byte, 2)
	_, err = io.ReadFull(f, header)
	f.Close()
	if err != nil || string(header) != "MZ" {
		return cleanup(errors.New("download não é um executável Windows"))
	}
	if err := verifyFile(name, m.SHA256); err != nil {
		return cleanup(err)
	}
	return name, nil
}
func copyFile(src, dest string) error {
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, err = io.Copy(to, from)
	closeErr := to.Close()
	if err != nil {
		os.Remove(dest)
		return err
	}
	if closeErr != nil {
		os.Remove(dest)
		return closeErr
	}
	return nil
}
func startUpdateHelper(dir, exe, staged, digest string) error {
	helper := filepath.Join(dir, "updater", fmt.Sprintf("DDD-helper-%d.exe", time.Now().UnixNano()))
	if err := copyFile(exe, helper); err != nil {
		return err
	}
	cmd := exec.Command(helper, "-apply-update", "-target", exe, "-new", staged, "-sha256", digest)
	// O helper é uma cópia separada do programa. Ele aguarda o DDD encerrar.
	if err := configureHelperProcess(cmd); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		os.Remove(helper)
		return err
	}
	return cmd.Process.Release()
}
func applyUpdate(target, staged, digest string) error {
	if runtime.GOOS != "windows" {
		return errors.New("o instalador só funciona no Windows")
	}
	if err := verifyFile(staged, digest); err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(target), ".exe") {
		return errors.New("caminho de instalação inválido")
	}
	tmp := target + ".update-new"
	backup := target + ".update-backup"
	// Copiar para a mesma pasta elimina problemas de movimentação entre discos.
	os.Remove(tmp)
	if err := copyFile(staged, tmp); err != nil {
		return err
	}
	if err := verifyFile(tmp, digest); err != nil {
		os.Remove(tmp)
		return err
	}
	// Windows não permite renomear um executável enquanto ele está em uso.
	var swapErr error
	for i := 0; i < 240; i++ {
		if _, err := os.Stat(target); err != nil {
			swapErr = err
			break
		}
		os.Remove(backup)
		swapErr = os.Rename(target, backup)
		if swapErr == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if swapErr != nil {
		os.Remove(tmp)
		return fmt.Errorf("DDD não encerrou ou pasta sem permissão: %w", swapErr)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Rename(backup, target)
		os.Remove(tmp)
		return err
	}
	// Manter o backup até o próximo lançamento; contas ficam em AppData e não mudam.
	cmd := exec.Command(target)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(target)
		_ = os.Rename(backup, target)
		return err
	}
	return cmd.Process.Release()
}
func updateAtStartup(dataDir string) (bool, error) {
	if runtime.GOOS != "windows" {
		return false, nil
	}
	conf, err := readUpdateConfig(dataDir)
	if err != nil {
		return false, err
	}
	if conf.Repo == "" {
		return false, nil
	}
	m, err := fetchManifest(conf.Repo)
	if err != nil {
		return false, err
	}
	if !isNewVersion(m.Version, appVersion) {
		return false, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return false, err
	}
	// Verifica se a pasta permite atualização antes de baixar.
	probe, err := os.CreateTemp(filepath.Dir(exe), ".ddd-permission-check-")
	if err != nil {
		return false, fmt.Errorf("sem permissão para atualizar nesta pasta: %w", err)
	}
	probe.Close()
	os.Remove(probe.Name())
	staged, err := downloadUpdate(m, dataDir)
	if err != nil {
		return false, err
	}
	if err := startUpdateHelper(dataDir, exe, staged, m.SHA256); err != nil {
		os.Remove(staged)
		return false, err
	}
	return true, nil
}
