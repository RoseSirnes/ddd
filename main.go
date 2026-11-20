package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed public/*
var webFS embed.FS

type User struct {
	ID            string `json:"id"`
	Nickname      string `json:"nickname"`
	PasswordHash  string `json:"passwordHash"`
	AvatarImageID string `json:"avatarImageId"`
	Bio           string `json:"bio"`
	Status        string `json:"status"`
	CreatedAt     int64  `json:"createdAt"`
}
type Session struct {
	UserID    string `json:"userId"`
	ExpiresAt int64  `json:"expiresAt"`
}
type FriendRequest struct {
	ID         string `json:"id"`
	SenderID   string `json:"senderId"`
	ReceiverID string `json:"receiverId"`
	Status     string `json:"status"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
}
type Message struct {
	ID          string `json:"id"`
	Channel     string `json:"channel"`
	SenderID    string `json:"senderId"`
	RecipientID string `json:"recipientId"`
	Body        string `json:"body"`
	ImageID     string `json:"imageId"`
	CreatedAt   int64  `json:"createdAt"`
}
type Image struct {
	ID        string `json:"id"`
	OwnerID   string `json:"ownerId"`
	FileName  string `json:"fileName"`
	MIME      string `json:"mime"`
	CreatedAt int64  `json:"createdAt"`
}
type Data struct {
	Users    map[string]User          `json:"users"`
	Sessions map[string]Session       `json:"sessions"`
	Friends  map[string]int64         `json:"friends"`
	Requests map[string]FriendRequest `json:"requests"`
	Messages []Message                `json:"messages"`
	Images   map[string]Image         `json:"images"`
}
type Event struct {
	Kind string
	Data any
}
type App struct {
	mu           sync.Mutex
	db           Data
	dataFile     string
	dataDir      string
	publicServer bool
	subscribers  map[string]map[chan Event]bool
	rate         map[string][]time.Time
}

func newApp(dir string) (*App, error) {
	if err := os.MkdirAll(filepath.Join(dir, "uploads"), 0700); err != nil {
		return nil, err
	}
	a := &App{dataFile: filepath.Join(dir, "ddd-data.json"), dataDir: dir, subscribers: map[string]map[chan Event]bool{}, rate: map[string][]time.Time{}}
	a.db = Data{Users: map[string]User{}, Sessions: map[string]Session{}, Friends: map[string]int64{}, Requests: map[string]FriendRequest{}, Messages: []Message{}, Images: map[string]Image{}}
	raw, err := os.ReadFile(a.dataFile)
	if err == nil {
		if err = json.Unmarshal(raw, &a.db); err != nil {
			return nil, fmt.Errorf("arquivo de dados inválido: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if a.db.Users == nil {
		a.db.Users = map[string]User{}
	}
	if a.db.Sessions == nil {
		a.db.Sessions = map[string]Session{}
	}
	if a.db.Friends == nil {
		a.db.Friends = map[string]int64{}
	}
	if a.db.Requests == nil {
		a.db.Requests = map[string]FriendRequest{}
	}
	if a.db.Images == nil {
		a.db.Images = map[string]Image{}
	}
	return a, nil
}
func (a *App) save() error {
	raw, err := json.Marshal(a.db)
	if err != nil {
		return err
	}
	path := a.dataFile + ".tmp"
	if err = os.WriteFile(path, raw, 0600); err != nil {
		return err
	}
	return os.Rename(path, a.dataFile)
}
func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func uid() string {
	s := randHex(16)
	return s[:8] + "-" + s[8:12] + "-4" + s[13:16] + "-a" + s[17:20] + "-" + s[20:]
}
func sha(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func now() int64          { return time.Now().UnixMilli() }
func pbkdf2(password string, salt []byte, iterations, keyLen int) []byte {
	hLen := 32
	out := make([]byte, 0, keyLen)
	for block := 1; len(out) < keyLen; block++ {
		mac := hmac.New(sha256.New, []byte(password))
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for j := 1; j < iterations; j++ {
			mac = hmac.New(sha256.New, []byte(password))
			mac.Write(u)
			u = mac.Sum(nil)
			for k := 0; k < hLen; k++ {
				t[k] ^= u[k]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}
func hashPassword(password string) string {
	salt, _ := hex.DecodeString(randHex(16))
	hash := pbkdf2(password, salt, 120000, 32)
	return "pbkdf2-sha256:120000:" + hex.EncodeToString(salt) + ":" + hex.EncodeToString(hash)
}
func checkPassword(password, stored string) bool {
	parts := strings.Split(stored, ":")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	rounds, err := strconv.Atoi(parts[1])
	if err != nil || rounds < 100000 || rounds > 300000 {
		return false
	}
	salt, e1 := hex.DecodeString(parts[2])
	want, e2 := hex.DecodeString(parts[3])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(want) != 32 {
		return false
	}
	actual := pbkdf2(password, salt, rounds, 32)
	return hmac.Equal(actual, want)
}
func remoteIP(r *http.Request) string {
	// RemoteAddr has a per-connection TCP port: do not use it for rate-limit keys.
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (a *App) session(r *http.Request) (User, bool) {
	c, e := r.Cookie("ddd_session")
	if e != nil || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(c.Value) {
		return User{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	ss, ok := a.db.Sessions[sha(c.Value)]
	if !ok || ss.ExpiresAt < now() {
		return User{}, false
	}
	u, ok := a.db.Users[ss.UserID]
	return u, ok
}
func setCookie(w http.ResponseWriter, token string, age int, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: "ddd_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age, Secure: secure})
}
func (a *App) newSession(userID string) string {
	token := randHex(32)
	a.db.Sessions[sha(token)] = Session{userID, now() + int64(30*24*time.Hour/time.Millisecond)}
	return token
}
func (a *App) online(id string) bool { return len(a.subscribers[id]) > 0 }
func (a *App) onlineCount() int      { return len(a.subscribers) }
func friendKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}
func (a *App) isFriend(x, y string) bool {
	_, ok := a.db.Friends[friendKey(x, y)]
	return x != "" && y != "" && x != y && ok
}
func (a *App) friends(id string) []string {
	result := []string{}
	for key := range a.db.Friends {
		parts := strings.SplitN(key, "|", 2)
		if parts[0] == id {
			result = append(result, parts[1])
		} else if parts[1] == id {
			result = append(result, parts[0])
		}
	}
	return result
}
func (a *App) tell(id, typ string, data any) {
	for ch := range a.subscribers[id] {
		select {
		case ch <- Event{typ, data}:
		default:
		}
	}
}
func (a *App) presence() {
	for id := range a.subscribers {
		ids := []string{}
		for _, f := range a.friends(id) {
			if a.online(f) {
				ids = append(ids, f)
			}
		}
		a.tell(id, "presence", map[string]any{"friendIds": ids, "onlineCount": a.onlineCount()})
	}
}
func (a *App) limit(key string, max int, window time.Duration) bool {
	ts := a.rate[key]
	t := time.Now()
	fresh := make([]time.Time, 0, len(ts)+1)
	for _, v := range ts {
		if t.Sub(v) < window {
			fresh = append(fresh, v)
		}
	}
	if len(fresh) >= max {
		a.rate[key] = fresh
		return false
	}
	a.rate[key] = append(fresh, t)
	if len(a.rate) > 3000 {
		for k, v := range a.rate {
			if len(v) == 0 || t.Sub(v[len(v)-1]) > time.Hour {
				delete(a.rate, k)
			}
		}
	}
	return true
}
func publicUser(u User) any {
	return map[string]any{"id": u.ID, "nickname": u.Nickname, "avatarImageId": nullStr(u.AvatarImageID), "bio": u.Bio, "status": u.Status, "createdAt": u.CreatedAt}
}
func jsonRes(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func apiErr(w http.ResponseWriter, status int, s string) {
	jsonRes(w, status, map[string]any{"error": s})
}
func readJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		apiErr(w, 415, "Envie os dados em JSON.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16000)
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		apiErr(w, 400, "JSON inválido.")
		return false
	}
	return true
}
func asMap(m Message, u User) map[string]any {
	return map[string]any{"id": m.ID, "channel": m.Channel, "senderId": m.SenderID, "recipientId": nullStr(m.RecipientID), "senderNick": u.Nickname, "senderAvatarImageId": nullStr(u.AvatarImageID), "senderBio": u.Bio, "senderStatus": u.Status, "body": m.Body, "imageId": nullStr(m.ImageID), "createdAt": m.CreatedAt}
}
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (a *App) routes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' blob: data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.Method != "GET" && r.Method != "HEAD" {
		if s := r.Header.Get("Origin"); s != "" {
			host := strings.TrimPrefix(strings.TrimPrefix(s, "http://"), "https://")
			if host != r.Host {
				apiErr(w, 403, "Origem não permitida.")
				return
			}
		}
	}
	p := r.URL.Path
	if p == "/healthz" && r.Method == "GET" {
		jsonRes(w, 200, map[string]any{"status": "ok", "version": appVersion})
		return
	}
	if strings.HasPrefix(p, "/api/") {
		if a.routeServerConfig(w, r) {
			return
		}
		if a.publicServer && strings.HasPrefix(p, "/api/update/") {
			apiErr(w, 403, "Atualizações são gerenciadas pelo aplicativo instalado.")
			return
		}
		if a.maybeProxyAPI(w, r) {
			return
		}
	}
	if !strings.HasPrefix(p, "/api/") {
		names := map[string]string{"/": "index.html", "/app.js": "app.js", "/styles.css": "styles.css", "/favicon.svg": "favicon.svg"}
		name, ok := names[p]
		if !ok {
			apiErr(w, 404, "Página não encontrada.")
			return
		}
		raw, err := webFS.ReadFile("public/" + name)
		if err != nil {
			apiErr(w, 500, "Interface indisponível.")
			return
		}
		switch filepath.Ext(name) {
		case ".html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case ".js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case ".svg":
			w.Header().Set("Content-Type", "image/svg+xml")
		}
		w.Write(raw)
		return
	}
	if p == "/api/me" && r.Method == "GET" {
		u, ok := a.session(r)
		a.mu.Lock()
		cnt := a.onlineCount()
		a.mu.Unlock()
		var v any
		if ok {
			v = publicUser(u)
		}
		jsonRes(w, 200, map[string]any{"user": v, "onlineCount": cnt})
		return
	}
	if p == "/api/auth/register" && r.Method == "POST" {
		a.mu.Lock()
		allowed := a.limit("reg:"+remoteIP(r), 5, time.Hour)
		a.mu.Unlock()
		if !allowed {
			apiErr(w, 429, "Muitas tentativas. Aguarde um pouco.")
			return
		}
		var req struct {
			Nickname string `json:"nickname"`
			Password string `json:"password"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		if !regexp.MustCompile(`^[A-Za-z0-9_.]{3,20}$`).MatchString(req.Nickname) {
			apiErr(w, 400, "O nick precisa ter de 3 a 20 caracteres: letras, números, ponto ou _.")
			return
		}
		if len(req.Password) < 8 || len(req.Password) > 128 {
			apiErr(w, 400, "A senha deve ter entre 8 e 128 caracteres.")
			return
		}
		a.mu.Lock()
		for _, u := range a.db.Users {
			if strings.EqualFold(u.Nickname, req.Nickname) {
				a.mu.Unlock()
				apiErr(w, 409, "Este nick já está em uso.")
				return
			}
		}
		a.mu.Unlock()
		hash := hashPassword(req.Password)
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, u := range a.db.Users {
			if strings.EqualFold(u.Nickname, req.Nickname) {
				apiErr(w, 409, "Este nick já está em uso.")
				return
			}
		}
		id := uid()
		u := User{ID: id, Nickname: req.Nickname, PasswordHash: hash, CreatedAt: now()}
		a.db.Users[id] = u
		token := a.newSession(id)
		if err := a.save(); err != nil {
			log.Print(err)
			apiErr(w, 500, "Não foi possível salvar a conta.")
			return
		}
		setCookie(w, token, 30*86400, a.publicServer)
		jsonRes(w, 201, map[string]any{"user": publicUser(u)})
		return
	}
	if p == "/api/auth/login" && r.Method == "POST" {
		a.mu.Lock()
		allowed := a.limit("login:"+remoteIP(r), 18, 15*time.Minute)
		a.mu.Unlock()
		if !allowed {
			apiErr(w, 429, "Muitas tentativas. Aguarde um pouco.")
			return
		}
		var req struct {
			Nickname string `json:"nickname"`
			Password string `json:"password"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		a.mu.Lock()
		var match User
		for _, u := range a.db.Users {
			if strings.EqualFold(u.Nickname, req.Nickname) {
				match = u
				break
			}
		}
		a.mu.Unlock()
		if match.ID == "" || !checkPassword(req.Password, match.PasswordHash) {
			apiErr(w, 401, "Nick ou senha incorretos.")
			return
		}
		a.mu.Lock()
		token := a.newSession(match.ID)
		if err := a.save(); err != nil {
			log.Print(err)
		}
		a.mu.Unlock()
		setCookie(w, token, 30*86400, a.publicServer)
		jsonRes(w, 200, map[string]any{"user": publicUser(match)})
		return
	}
	if p == "/api/auth/logout" && r.Method == "POST" {
		if cookie, err := r.Cookie("ddd_session"); err == nil {
			a.mu.Lock()
			delete(a.db.Sessions, sha(cookie.Value))
			a.save()
			a.mu.Unlock()
		}
		setCookie(w, "", -1, a.publicServer)
		jsonRes(w, 200, map[string]any{"ok": true})
		return
	}
	u, ok := a.session(r)
	if !ok {
		apiErr(w, 401, "Você precisa entrar na sua conta.")
		return
	}
	// A configuração da fonte de atualizações só é acessível pelo aplicativo local.
	if p == "/api/update/status" && r.Method == "GET" {
		config, err := readUpdateConfig(a.dataDir)
		if err != nil {
			apiErr(w, 500, "Falha ao ler configuração de atualização.")
			return
		}
		jsonRes(w, 200, map[string]any{"version": appVersion, "repo": config.Repo, "enabled": config.Repo != "", "managed": defaultUpdateRepo != ""})
		return
	}
	if p == "/api/update/config" && r.Method == "POST" {
		if defaultUpdateRepo != "" {
			apiErr(w, 403, "Atualizações são configuradas automaticamente pelo DDD.")
			return
		}
		var req struct {
			Repo string `json:"repo"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		repo, err := validateRepo(req.Repo)
		if err != nil {
			apiErr(w, 400, err.Error())
			return
		}
		if err := writeUpdateConfig(a.dataDir, updateConfig{Repo: repo}); err != nil {
			apiErr(w, 500, "Não foi possível salvar a configuração.")
			return
		}
		jsonRes(w, 200, map[string]any{"ok": true, "repo": repo})
		return
	}
	if p == "/api/update/check" && r.Method == "POST" {
		conf, err := readUpdateConfig(a.dataDir)
		if err != nil {
			apiErr(w, 500, "Configuração inválida.")
			return
		}
		if conf.Repo == "" {
			apiErr(w, 400, "Configure o repositório GitHub primeiro.")
			return
		}
		m, err := fetchManifest(conf.Repo)
		if err != nil {
			apiErr(w, 502, "Não foi possível consultar atualizações: "+err.Error())
			return
		}
		jsonRes(w, 200, map[string]any{"version": appVersion, "latestVersion": m.Version, "available": isNewVersion(m.Version, appVersion)})
		return
	}
	if p == "/api/update/apply" && r.Method == "POST" {
		conf, err := readUpdateConfig(a.dataDir)
		if err != nil {
			apiErr(w, 500, "Configuração inválida.")
			return
		}
		if conf.Repo == "" {
			apiErr(w, 400, "Configure o repositório GitHub primeiro.")
			return
		}
		m, err := fetchManifest(conf.Repo)
		if err != nil {
			apiErr(w, 502, "Não foi possível consultar atualizações: "+err.Error())
			return
		}
		if !isNewVersion(m.Version, appVersion) {
			jsonRes(w, 200, map[string]any{"updated": false, "version": appVersion, "message": "Você já está na versão mais recente."})
			return
		}
		exe, err := os.Executable()
		if err != nil {
			apiErr(w, 500, "Não foi possível localizar o executável do DDD.")
			return
		}
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			apiErr(w, 500, "Não foi possível preparar a atualização.")
			return
		}
		probe, err := os.CreateTemp(filepath.Dir(exe), ".ddd-permission-check-")
		if err != nil {
			apiErr(w, 500, "Sem permissão para atualizar nesta pasta.")
			return
		}
		probe.Close()
		os.Remove(probe.Name())
		staged, err := downloadUpdate(m, a.dataDir)
		if err != nil {
			apiErr(w, 502, "Não foi possível baixar a atualização: "+err.Error())
			return
		}
		if err := startUpdateHelper(a.dataDir, exe, staged, m.SHA256); err != nil {
			os.Remove(staged)
			apiErr(w, 500, "Não foi possível iniciar a atualização: "+err.Error())
			return
		}
		jsonRes(w, 202, map[string]any{"updated": true, "latestVersion": m.Version})
		go func() {
			time.Sleep(800 * time.Millisecond)
			os.Exit(0)
		}()
		return
	}
	if strings.HasPrefix(p, "/api/profile/") && r.Method == http.MethodGet {
		id := strings.TrimPrefix(p, "/api/profile/")
		if id == "" || strings.Contains(id, "/") {
			apiErr(w, 404, "Perfil não encontrado.")
			return
		}
		a.mu.Lock()
		person, exists := a.db.Users[id]
		a.mu.Unlock()
		if !exists {
			apiErr(w, 404, "Perfil não encontrado.")
			return
		}
		jsonRes(w, 200, map[string]any{"user": publicUser(person)})
		return
	}
	if p == "/api/profile/details" && r.Method == http.MethodPost {
		var body struct {
			Bio    string `json:"bio"`
			Status string `json:"status"`
		}
		if !readJSON(w, r, &body) {
			return
		}
		body.Bio = strings.TrimSpace(body.Bio)
		body.Status = strings.TrimSpace(body.Status)
		if len([]rune(body.Bio)) > 280 || len([]rune(body.Status)) > 60 {
			apiErr(w, 400, "Bio ou status acima do limite de caracteres.")
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		person := a.db.Users[u.ID]
		oldPerson := person
		person.Bio, person.Status = body.Bio, body.Status
		a.db.Users[u.ID] = person
		if err := a.save(); err != nil {
			a.db.Users[u.ID] = oldPerson
			apiErr(w, 500, "Não foi possível salvar as informações do perfil.")
			return
		}
		for id := range a.subscribers {
			a.tell(id, "profile:changed", map[string]string{"id": u.ID})
		}
		jsonRes(w, 200, map[string]any{"user": publicUser(person)})
		return
	}
	if p == "/api/profile/avatar" && r.Method == "POST" {
		var req struct {
			ImageID string `json:"imageId"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		user := a.db.Users[u.ID]
		if strings.TrimSpace(req.ImageID) == "" {
			user.AvatarImageID = ""
			a.db.Users[u.ID] = user
			_ = a.save()
			jsonRes(w, 200, map[string]any{"user": publicUser(user)})
			return
		}
		img, ok := a.db.Images[req.ImageID]
		if !ok || img.OwnerID != u.ID {
			apiErr(w, 403, "Escolha primeiro uma imagem enviada por você.")
			return
		}
		user.AvatarImageID = img.ID
		a.db.Users[u.ID] = user
		if err := a.save(); err != nil {
			apiErr(w, 500, "Não foi possível salvar sua foto de perfil.")
			return
		}
		jsonRes(w, 200, map[string]any{"user": publicUser(user)})
		return
	}
	if p == "/api/events" && r.Method == "GET" {
		flush, ok := w.(http.Flusher)
		if !ok {
			apiErr(w, 500, "Eventos indisponíveis.")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(200)
		fmt.Fprint(w, ": conectado\n\n")
		flush.Flush()
		ch := make(chan Event, 128)
		a.mu.Lock()
		if a.subscribers[u.ID] == nil {
			a.subscribers[u.ID] = map[chan Event]bool{}
		}
		a.subscribers[u.ID][ch] = true
		a.tell(u.ID, "ready", map[string]any{"nickname": u.Nickname, "onlineCount": a.onlineCount()})
		a.presence()
		a.mu.Unlock()
		defer func() {
			a.mu.Lock()
			delete(a.subscribers[u.ID], ch)
			if len(a.subscribers[u.ID]) == 0 {
				delete(a.subscribers, u.ID)
			}
			a.presence()
			a.mu.Unlock()
		}()
		ping := time.NewTicker(25 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case evt := <-ch:
				b, _ := json.Marshal(evt.Data)
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Kind, b); err != nil {
					return
				}
				flush.Flush()
			case <-ping.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				flush.Flush()
			}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if p == "/api/stats" && r.Method == "GET" {
		jsonRes(w, 200, map[string]any{"registered": len(a.db.Users), "onlineCount": a.onlineCount()})
		return
	}
	if p == "/api/friends" && r.Method == "GET" {
		arr := []any{}
		for _, id := range a.friends(u.ID) {
			v := a.db.Users[id]
			arr = append(arr, map[string]any{"id": id, "nickname": v.Nickname, "avatarImageId": nullStr(v.AvatarImageID), "bio": v.Bio, "status": v.Status, "createdAt": v.CreatedAt, "online": a.online(id)})
		}
		sort.Slice(arr, func(i, j int) bool {
			return strings.ToLower(arr[i].(map[string]any)["nickname"].(string)) < strings.ToLower(arr[j].(map[string]any)["nickname"].(string))
		})
		jsonRes(w, 200, map[string]any{"friends": arr})
		return
	}
	if p == "/api/requests" && r.Method == "GET" {
		incoming := []any{}
		outgoing := []any{}
		for _, q := range a.db.Requests {
			if q.Status != "pending" {
				continue
			}
			if q.ReceiverID == u.ID {
				incoming = append(incoming, map[string]any{"id": q.ID, "userId": q.SenderID, "nickname": a.db.Users[q.SenderID].Nickname, "avatarImageId": nullStr(a.db.Users[q.SenderID].AvatarImageID), "bio": a.db.Users[q.SenderID].Bio, "status": a.db.Users[q.SenderID].Status, "createdAt": q.CreatedAt})
			} else if q.SenderID == u.ID {
				outgoing = append(outgoing, map[string]any{"id": q.ID, "userId": q.ReceiverID, "nickname": a.db.Users[q.ReceiverID].Nickname, "avatarImageId": nullStr(a.db.Users[q.ReceiverID].AvatarImageID), "bio": a.db.Users[q.ReceiverID].Bio, "status": a.db.Users[q.ReceiverID].Status, "createdAt": q.CreatedAt})
			}
		}
		sort.Slice(incoming, func(i, j int) bool {
			return incoming[i].(map[string]any)["createdAt"].(int64) > incoming[j].(map[string]any)["createdAt"].(int64)
		})
		jsonRes(w, 200, map[string]any{"incoming": incoming, "outgoing": outgoing})
		return
	}
	if p == "/api/users" && r.Method == "GET" {
		if !a.limit("search:"+u.ID, 45, time.Minute) {
			apiErr(w, 429, "Muitas buscas. Aguarde.")
			return
		}
		q := strings.ToLower(strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(r.URL.Query().Get("q")), "@")))
		arr := []any{}
		if len(q) >= 2 {
			for _, v := range a.db.Users {
				if v.ID == u.ID || !strings.Contains(strings.ToLower(v.Nickname), q) {
					continue
				}
				status := "none"
				if a.isFriend(u.ID, v.ID) {
					status = "friend"
				} else {
					for _, fr := range a.db.Requests {
						if fr.Status == "pending" {
							if fr.SenderID == u.ID && fr.ReceiverID == v.ID {
								status = "outgoing"
							}
							if fr.ReceiverID == u.ID && fr.SenderID == v.ID {
								status = "incoming"
							}
						}
					}
				}
				arr = append(arr, map[string]any{"id": v.ID, "nickname": v.Nickname, "avatarImageId": nullStr(v.AvatarImageID), "bio": v.Bio, "profileStatus": v.Status, "online": a.online(v.ID), "status": status})
			}
		}
		sort.Slice(arr, func(i, j int) bool {
			return arr[i].(map[string]any)["nickname"].(string) < arr[j].(map[string]any)["nickname"].(string)
		})
		if len(arr) > 15 {
			arr = arr[:15]
		}
		jsonRes(w, 200, map[string]any{"users": arr})
		return
	}
	if p == "/api/requests" && r.Method == "POST" {
		if !a.limit("requests:"+u.ID, 12, time.Hour) {
			apiErr(w, 429, "Muitos pedidos. Aguarde.")
			return
		}
		var req struct {
			UserID string `json:"userId"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		if req.UserID == u.ID || req.UserID == "" {
			apiErr(w, 400, "Escolha outra pessoa.")
			return
		}
		target, found := a.db.Users[req.UserID]
		if !found {
			apiErr(w, 404, "Pessoa não encontrada.")
			return
		}
		if a.isFriend(u.ID, target.ID) {
			apiErr(w, 409, "Vocês já são amigos.")
			return
		}
		for _, pr := range a.db.Requests {
			if pr.Status == "pending" && pr.SenderID == u.ID && pr.ReceiverID == target.ID {
				apiErr(w, 409, "Pedido já enviado.")
				return
			}
			if pr.Status == "pending" && pr.SenderID == target.ID && pr.ReceiverID == u.ID {
				apiErr(w, 409, "Essa pessoa já enviou um pedido. Veja seus convites.")
				return
			}
			if pr.Status == "declined" && pr.SenderID == u.ID && pr.ReceiverID == target.ID && now()-pr.UpdatedAt < 86400000 {
				apiErr(w, 429, "Você poderá enviar outro pedido depois de 24 horas.")
				return
			}
		}
		id := uid()
		a.db.Requests[id] = FriendRequest{ID: id, SenderID: u.ID, ReceiverID: target.ID, Status: "pending", CreatedAt: now(), UpdatedAt: now()}
		a.save()
		a.tell(target.ID, "notification", map[string]any{"kind": "request", "nickname": u.Nickname, "text": u.Nickname + " quer ser seu amigo(a)!"})
		a.tell(u.ID, "friends:changed", map[string]bool{"ok": true})
		a.tell(target.ID, "friends:changed", map[string]bool{"ok": true})
		jsonRes(w, 201, map[string]bool{"ok": true})
		return
	}
	if strings.HasPrefix(p, "/api/requests/") && r.Method == "POST" {
		parts := strings.Split(p, "/")
		if len(parts) != 5 || (parts[4] != "accept" && parts[4] != "decline") {
			apiErr(w, 404, "Rota não encontrada.")
			return
		}
		pr, found := a.db.Requests[parts[3]]
		if !found || pr.Status != "pending" || pr.ReceiverID != u.ID {
			apiErr(w, 404, "Pedido não encontrado.")
			return
		}
		if parts[4] == "accept" {
			a.db.Friends[friendKey(pr.SenderID, pr.ReceiverID)] = now()
			pr.Status = "accepted"
		} else {
			pr.Status = "declined"
		}
		pr.UpdatedAt = now()
		a.db.Requests[pr.ID] = pr
		a.save()
		if pr.Status == "accepted" {
			a.tell(pr.SenderID, "notification", map[string]any{"kind": "accepted", "nickname": u.Nickname, "text": u.Nickname + " aceitou seu pedido de amizade!"})
		}
		a.tell(pr.SenderID, "friends:changed", map[string]bool{"ok": true})
		a.tell(pr.ReceiverID, "friends:changed", map[string]bool{"ok": true})
		a.presence()
		jsonRes(w, 200, map[string]bool{"ok": true})
		return
	}
	if p == "/api/messages" && r.Method == "GET" {
		channel := r.URL.Query().Get("channel")
		if channel == "" {
			channel = "global"
		}
		if channel != "global" && channel != "dm" {
			apiErr(w, 400, "Canal inválido.")
			return
		}
		peer := r.URL.Query().Get("with")
		if channel == "dm" && !a.isFriend(u.ID, peer) {
			apiErr(w, 403, "Vocês precisam ser amigos para conversar.")
			return
		}
		before := int64(1<<63 - 1)
		if s := r.URL.Query().Get("before"); s != "" {
			v, err := strconv.ParseInt(s, 10, 64)
			if err != nil || v < 1 {
				apiErr(w, 400, "Paginação inválida.")
				return
			}
			before = v
		}
		all := []Message{}
		for _, m := range a.db.Messages {
			if m.CreatedAt >= before || m.Channel != channel {
				continue
			}
			if channel == "dm" && !((m.SenderID == u.ID && m.RecipientID == peer) || (m.SenderID == peer && m.RecipientID == u.ID)) {
				continue
			}
			all = append(all, m)
		}
		sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt > all[j].CreatedAt })
		hasMore := len(all) >= 60
		if len(all) > 60 {
			all = all[:60]
		}
		arr := []any{}
		for i := len(all) - 1; i >= 0; i-- {
			arr = append(arr, asMap(all[i], a.db.Users[all[i].SenderID]))
		}
		jsonRes(w, 200, map[string]any{"messages": arr, "hasMore": hasMore})
		return
	}
	if p == "/api/messages" && r.Method == "POST" {
		if !a.limit("msg:"+u.ID, 12, 10*time.Second) {
			apiErr(w, 429, "Muitas mensagens. Aguarde.")
			return
		}
		var req struct {
			Channel     string `json:"channel"`
			RecipientID string `json:"recipientId"`
			Text        string `json:"text"`
			ImageID     string `json:"imageId"`
		}
		if !readJSON(w, r, &req) {
			return
		}
		if req.Channel != "global" && req.Channel != "dm" {
			apiErr(w, 400, "Canal inválido.")
			return
		}
		if req.Channel == "dm" && !a.isFriend(u.ID, req.RecipientID) {
			apiErr(w, 403, "Você só pode mandar mensagens privadas para amigos.")
			return
		}
		clean := strings.TrimSpace(req.Text)
		if len([]rune(req.Text)) > 2000 || clean == "" && req.ImageID == "" {
			apiErr(w, 400, "Mensagem inválida ou maior que 2.000 caracteres.")
			return
		}
		if req.ImageID != "" {
			img, ok := a.db.Images[req.ImageID]
			if !ok || img.OwnerID != u.ID {
				apiErr(w, 403, "Esta imagem não pertence a você.")
				return
			}
			for _, m := range a.db.Messages {
				if m.ImageID == req.ImageID {
					apiErr(w, 409, "Esta imagem já foi enviada.")
					return
				}
			}
		}
		m := Message{ID: uid(), Channel: req.Channel, SenderID: u.ID, RecipientID: req.RecipientID, Body: clean, ImageID: req.ImageID, CreatedAt: now()}
		a.db.Messages = append(a.db.Messages, m)
		a.save()
		serialized := asMap(m, u)
		if req.Channel == "global" {
			for id := range a.subscribers {
				a.tell(id, "message:new", serialized)
			}
		} else {
			a.tell(u.ID, "message:new", serialized)
			a.tell(req.RecipientID, "message:new", serialized)
		}
		jsonRes(w, 201, map[string]any{"message": serialized})
		return
	}
	if p == "/api/images" && r.Method == "POST" {
		if !a.limit("img:"+u.ID, 15, time.Minute) {
			apiErr(w, 429, "Muitos envios. Aguarde.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 5*1024*1024)
		bytes, err := io.ReadAll(r.Body)
		if err != nil {
			apiErr(w, 413, "A imagem deve ter até 5 MB.")
			return
		}
		mime, ext := imageSignature(bytes)
		if mime == "" {
			apiErr(w, 415, "Envie uma imagem PNG, JPG, WEBP ou GIF válida.")
			return
		}
		id := uid()
		filename := id + "." + ext
		if err := os.WriteFile(filepath.Join(a.dataDir, "uploads", filename), bytes, 0600); err != nil {
			apiErr(w, 500, "Não foi possível salvar a imagem.")
			return
		}
		a.db.Images[id] = Image{ID: id, OwnerID: u.ID, FileName: filename, MIME: mime, CreatedAt: now()}
		a.save()
		jsonRes(w, 201, map[string]any{"imageId": id, "url": "/api/images/" + id})
		return
	}
	if strings.HasPrefix(p, "/api/images/") && r.Method == "GET" {
		id := strings.TrimPrefix(p, "/api/images/")
		img, ok := a.db.Images[id]
		if !ok {
			apiErr(w, 404, "Imagem não encontrada.")
			return
		}
		if img.OwnerID != u.ID {
			can := false
			for _, user := range a.db.Users {
				if user.AvatarImageID == id {
					can = true
					break
				}
			}
			if !can {
				for _, msg := range a.db.Messages {
					if msg.ImageID == id && (msg.Channel == "global" || msg.Channel == "dm" && (msg.SenderID == u.ID || msg.RecipientID == u.ID)) {
						can = true
						break
					}
				}
			}
			if !can {
				apiErr(w, 403, "Acesso negado a esta imagem.")
				return
			}
		}
		file, err := os.ReadFile(filepath.Join(a.dataDir, "uploads", img.FileName))
		if err != nil {
			apiErr(w, 404, "Arquivo não encontrado.")
			return
		}
		w.Header().Set("Content-Type", img.MIME)
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Write(file)
		return
	}
	apiErr(w, 404, "Rota não encontrada.")
}
func imageSignature(b []byte) (string, string) {
	if len(b) >= 8 && hex.EncodeToString(b[:8]) == "89504e470d0a1a0a" {
		return "image/png", "png"
	}
	if len(b) >= 3 && b[0] == 255 && b[1] == 216 && b[2] == 255 {
		return "image/jpeg", "jpg"
	}
	if len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a") {
		return "image/gif", "gif"
	}
	if len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return "image/webp", "webp"
	}
	return "", ""
}
func launchDesktop(url, profileDir string) error {
	if runtime.GOOS != "windows" {
		return errors.New("Este modo requer Windows; para testar use -server-only")
	}
	candidates := []string{}
	if p := os.Getenv("PROGRAMFILES(X86)"); p != "" {
		candidates = append(candidates, filepath.Join(p, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	if p := os.Getenv("PROGRAMFILES"); p != "" {
		candidates = append(candidates, filepath.Join(p, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	if p := os.Getenv("LOCALAPPDATA"); p != "" {
		candidates = append(candidates, filepath.Join(p, "Microsoft", "Edge", "Application", "msedge.exe"))
	}
	if found, err := exec.LookPath("msedge.exe"); err == nil {
		candidates = append(candidates, found)
	}
	var edge string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			edge = p
			break
		}
	}
	if edge == "" {
		return errors.New("Microsoft Edge não encontrado. Instale o Edge para abrir a janela do DDD.")
	}
	args := []string{"--no-first-run", "--no-default-browser-check", "--disable-features=msEdgeSidebarV2", "--app=" + url, "--user-data-dir=" + filepath.Join(profileDir, "edge-profile")}
	cmd := exec.Command(edge, args...)
	return cmd.Run()
}
func main() {
	apply := flag.Bool("apply-update", false, "modo interno para instalar atualização")
	target := flag.String("target", "", "destino da instalação (interno)")
	staged := flag.String("new", "", "novo executável (interno)")
	digest := flag.String("sha256", "", "hash SHA256 esperado (interno)")
	serveOnly := flag.Bool("server-only", false, "executa o serviço sem janela (para testes)")
	publicServer := flag.Bool("public-server", false, "executa como servidor central atrás de proxy HTTPS")
	port := flag.Int("port", 0, "porta TCP local; 0 escolhe automática")
	host := flag.String("host", "127.0.0.1", "interface de escuta; use 0.0.0.0 somente em servidor central")
	dir := flag.String("data-dir", "", "pasta onde as contas e mensagens ficam salvas")
	flag.Parse()
	if *apply {
		if err := applyUpdate(*target, *staged, *digest); err != nil {
			showError("Falha ao atualizar DDD: " + err.Error())
		}
		return
	}
	dataDir := *dir
	if dataDir == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			log.Fatal(err)
		}
		dataDir = filepath.Join(config, "DDD")
	}
	if !*serveOnly && !*publicServer {
		updated, err := updateAtStartup(dataDir)
		if err != nil {
			log.Print("Verificação de atualização: ", err)
		}
		if updated {
			return
		} // o helper instala e inicia o novo DDD
	}
	app, err := newApp(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	app.publicServer = *publicServer
	if *publicServer {
		*serveOnly = true
		if *port == 0 {
			if envPort, err := strconv.Atoi(os.Getenv("PORT")); err == nil && envPort > 0 {
				*port = envPort
			} else {
				*port = 8080
			}
		}
		if *host == "127.0.0.1" {
			*host = "0.0.0.0"
		}
	}
	if !*publicServer && *host != "127.0.0.1" && *host != "localhost" {
		log.Fatal("Acesso externo requer -public-server e proxy HTTPS")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(*host, strconv.Itoa(*port)))
	if err != nil {
		log.Fatal(err)
	}
	httpServer := &http.Server{Handler: http.HandlerFunc(app.routes), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Print(err)
		}
	}()
	url := "http://" + listener.Addr().String() + "/"
	log.Println("DDD disponível em", url)
	if *serveOnly {
		select {}
	} // usado somente em diagnósticos e testes automatizados
	if err := launchDesktop(url, dataDir); err != nil {
		showError("DDD: " + err.Error())
		log.Print(err)
	}
	_ = httpServer.Shutdown(context.Background())
}
