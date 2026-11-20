package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The desktop serves its local UI and can route chat APIs to a user-configured
// central HTTPS server. Admin-only endpoints remain local to the executable.
type serverConfig struct {
	URL string `json:"url"`
}

// Injetado no executável pelo processo de publicação, nunca pelo usuário final.
var defaultCentralURL string

func validateServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" || u.Port() != "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("Informe um endereço HTTPS válido, como https://chat.exemplo.com")
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || net.ParseIP(h) != nil ||
		!strings.Contains(h, ".") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".test") {
		return "", errors.New("O servidor precisa ter um domínio público HTTPS")
	}
	return "https://" + h, nil
}
func readServerConfig(dir string) (serverConfig, error) {
	if defaultCentralURL != "" {
		url, err := validateServerURL(defaultCentralURL)
		if err != nil {
			return serverConfig{}, fmt.Errorf("servidor oficial DDD inválido: %w", err)
		}
		return serverConfig{URL: url}, nil
	}
	var c serverConfig
	data, err := os.ReadFile(filepath.Join(dir, "server.json"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	c.URL, err = validateServerURL(c.URL)
	return c, err
}
func writeServerConfig(dir string, c serverConfig) error {
	if defaultCentralURL != "" {
		return errors.New("o servidor oficial do DDD é automático e não pode ser alterado")
	}
	var err error
	c.URL, err = validateServerURL(c.URL)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	temp := filepath.Join(dir, "server.json.tmp")
	if err := os.WriteFile(temp, b, 0600); err != nil {
		return err
	}
	return os.Rename(temp, filepath.Join(dir, "server.json"))
}
func (a *App) routeServerConfig(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/api/server/status" && r.Method == http.MethodGet {
		if a.publicServer {
			jsonRes(w, 200, map[string]any{"mode": "public", "url": "", "connected": true})
			return true
		}
		c, err := readServerConfig(a.dataDir)
		if err != nil {
			apiErr(w, 500, "Falha na configuração do servidor.")
			return true
		}
		mode := "local"
		if c.URL != "" {
			mode = "online"
		}
		jsonRes(w, 200, map[string]any{"mode": mode, "url": c.URL, "connected": c.URL != "", "managed": defaultCentralURL != ""})
		return true
	}
	if r.URL.Path == "/api/server/config" && r.Method == http.MethodPost {
		if a.publicServer {
			apiErr(w, 403, "Configuração indisponível no servidor público.")
			return true
		}
		var req struct {
			URL string `json:"url"`
		}
		if !readJSON(w, r, &req) {
			return true
		}
		c := serverConfig{URL: req.URL}
		if err := writeServerConfig(a.dataDir, c); err != nil {
			apiErr(w, 400, err.Error())
			return true
		}
		jsonRes(w, 200, map[string]any{"ok": true, "url": c.URL})
		return true
	}
	return false
}
func isLocalOnlyAPI(path string) bool {
	return strings.HasPrefix(path, "/api/update/") || strings.HasPrefix(path, "/api/server/")
}
func (a *App) maybeProxyAPI(w http.ResponseWriter, r *http.Request) bool {
	if a.publicServer || isLocalOnlyAPI(r.URL.Path) {
		return false
	}
	c, err := readServerConfig(a.dataDir)
	if err != nil {
		apiErr(w, 500, "Servidor online configurado incorretamente.")
		return true
	}
	if c.URL == "" {
		return false
	}
	target, err := url.Parse(c.URL)
	if err != nil {
		apiErr(w, 500, "Servidor configurado incorretamente.")
		return true
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.Header.Del("Origin") // The local Origin was validated by routes before proxying.
		req.Header.Del("Referer")
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	proxy.Transport = centralTransportFactory()
	proxy.FlushInterval = -1 // Stream chat events immediately.
	proxy.ModifyResponse = func(res *http.Response) error {
		if cookies := res.Cookies(); len(cookies) > 0 {
			res.Header.Del("Set-Cookie")
			for _, cookie := range cookies {
				cookie.Secure = false // The loopback UI is HTTP; upstream is always HTTPS.
				cookie.Domain = ""
				res.Header.Add("Set-Cookie", cookie.String())
			}
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		apiErr(w, 502, fmt.Sprintf("Não foi possível conectar ao servidor DDD (%s). Confira a internet ou o endereço nas configurações.", target.Host))
	}
	proxy.ServeHTTP(w, r)
	return true
}

var centralTransportFactory = func() http.RoundTripper {
	return &http.Transport{TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 25 * time.Second, Proxy: http.ProxyFromEnvironment}
}
