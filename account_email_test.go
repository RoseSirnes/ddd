package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func authJSON(t *testing.T, app *App, path string, data any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(data)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.9:3333"
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp := httptest.NewRecorder()
	app.routes(resp, req)
	return resp
}

func emailCodeFromBody(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}`).FindString(body)
	if m == "" {
		t.Fatalf("não achou código no e-mail: %q", body)
	}
	return m
}

func TestEmailRegistrationVerificationAndReset(t *testing.T) {
	app, err := newApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var mailBodies []string
	app.emailSender = func(ctx context.Context, to, title, body string) error {
		if to != "pessoa@example.com" {
			t.Errorf("destinatário errado: %q", to)
		}
		mailBodies = append(mailBodies, body)
		return nil
	}
	registered := authJSON(t, app, "/api/auth/register", map[string]string{
		"nickname": "UserTeste", "password": "senha-antiga!123", "email": "PESSOA@EXAMPLE.COM",
	}, nil)
	if registered.Code != http.StatusCreated {
		t.Fatalf("register %d: %s", registered.Code, registered.Body.String())
	}
	cookies := registered.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("cookie de sessão ausente")
	}
	cookie := cookies[0]
	if !strings.Contains(registered.Body.String(), `"verificationSent":true`) {
		t.Fatalf("não notificou envio: %s", registered.Body.String())
	}
	if len(mailBodies) != 1 {
		t.Fatal("e-mail de confirmação não enviado")
	}
	code := emailCodeFromBody(t, mailBodies[0])
	wrong := authJSON(t, app, "/api/auth/email/confirm", map[string]string{"code": "0123-4567-89AB-CDEF"}, cookie)
	if wrong.Code == http.StatusOK {
		t.Fatal("aceitou código errado")
	}
	verify := authJSON(t, app, "/api/auth/email/confirm", map[string]string{"code": code}, cookie)
	if verify.Code != http.StatusOK || !strings.Contains(verify.Body.String(), `"emailVerified":true`) {
		t.Fatalf("verificação %d %s", verify.Code, verify.Body.String())
	}
	// Public profile must never include account email address.
	var userID string
	for id := range app.db.Users {
		userID = id
	}
	profile := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/profile/"+userID, nil)
	request.AddCookie(cookie)
	app.routes(profile, request)
	if strings.Contains(profile.Body.String(), "pessoa@example.com") {
		t.Fatal("e-mail vazou no perfil público")
	}
	// Unknown emails always receive the same response, without disclosure.
	unknown := authJSON(t, app, "/api/auth/forgot", map[string]string{"email": "outro@example.com"}, nil)
	forgot := authJSON(t, app, "/api/auth/forgot", map[string]string{"email": "PESSOA@example.com"}, nil)
	if forgot.Code != 200 || unknown.Body.String() != forgot.Body.String() {
		t.Fatalf("enumeração de conta: unknown %s / known %s", unknown.Body.String(), forgot.Body.String())
	}
	if len(mailBodies) != 2 {
		t.Fatal("e-mail de recuperação não enviado")
	}
	resetCode := emailCodeFromBody(t, mailBodies[1])
	invalid := authJSON(t, app, "/api/auth/reset", map[string]string{
		"email": "pessoa@example.com", "code": "0000000000000000", "password": "novaSenha-2026!",
	}, nil)
	if invalid.Code == 200 {
		t.Fatal("aceitou código inválido")
	}
	reset := authJSON(t, app, "/api/auth/reset", map[string]string{
		"email": "pessoa@example.com", "code": resetCode, "password": "novaSenha-2026!",
	}, nil)
	if reset.Code != 200 {
		t.Fatalf("reset %d %s", reset.Code, reset.Body.String())
	}
	replay := authJSON(t, app, "/api/auth/reset", map[string]string{
		"email": "pessoa@example.com", "code": resetCode, "password": "novaSenha2-2026!",
	}, nil)
	if replay.Code == 200 {
		t.Fatal("reusou código de recuperação")
	}
	me := httptest.NewRecorder()
	request = httptest.NewRequest("GET", "/api/me", nil)
	request.AddCookie(cookie)
	app.routes(me, request)
	if !strings.Contains(me.Body.String(), `"user":null`) {
		t.Fatalf("sessão antiga não foi revogada: %s", me.Body.String())
	}
	oldLogin := authJSON(t, app, "/api/auth/login", map[string]string{"nickname": "UserTeste", "password": "senha-antiga!123"}, nil)
	newLogin := authJSON(t, app, "/api/auth/login", map[string]string{"nickname": "UserTeste", "password": "novaSenha-2026!"}, nil)
	if oldLogin.Code != 401 || newLogin.Code != 200 {
		t.Fatalf("login old %d new %d", oldLogin.Code, newLogin.Code)
	}
	// Persisted verified address survives restart.
	persisted, err := newApp(app.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range persisted.db.Users {
		if u.Email != "pessoa@example.com" || !u.EmailVerified {
			t.Fatal("e-mail não persistido")
		}
	}
}

func TestExistingAccountCanAttachVerifiedEmailAndPreventDuplicates(t *testing.T) {
	app, _ := newApp(t.TempDir())
	var gotMail string
	app.emailSender = func(ctx context.Context, to, subject, body string) error {
		gotMail = body
		return nil
	}
	app.mu.Lock()
	id := uid()
	app.db.Users[id] = User{ID: id, Nickname: "LegacyUser", PasswordHash: hashPassword("oldpassword123"), CreatedAt: now()}
	token := app.newSession(id)
	if err := app.save(); err != nil {
		t.Fatal(err)
	}
	app.mu.Unlock()
	cookie := &http.Cookie{Name: "ddd_session", Value: token}
	noPassword := authJSON(t, app, "/api/auth/email/request", map[string]string{
		"email": "legacy@example.com", "password": "wrongPassword",
	}, cookie)
	if noPassword.Code != 401 {
		t.Fatalf("sem senha atual %d", noPassword.Code)
	}
	resp := authJSON(t, app, "/api/auth/email/request", map[string]string{
		"email": "legacy@example.com", "password": "oldpassword123",
	}, cookie)
	if resp.Code != 200 {
		t.Fatalf("legacy email request %d %s", resp.Code, resp.Body.String())
	}
	confirm := authJSON(t, app, "/api/auth/email/confirm", map[string]string{"code": emailCodeFromBody(t, gotMail)}, cookie)
	if confirm.Code != 200 {
		t.Fatalf("legacy confirm %d %s", confirm.Code, confirm.Body.String())
	}
	duplicate := authJSON(t, app, "/api/auth/register", map[string]string{
		"nickname": "AnotherUser", "email": "LEGACY@example.com", "password": "passAnother2026!",
	}, nil)
	if duplicate.Code != 409 {
		t.Fatalf("e-mail duplicado aceito %d %s", duplicate.Code, duplicate.Body.String())
	}
}

func TestRecoveryDisabledWhenMailerAbsentAndCodeExpires(t *testing.T) {
	app, _ := newApp(t.TempDir())
	app.emailSender = nil
	reg := authJSON(t, app, "/api/auth/register", map[string]string{
		"nickname": "NoMailUser", "password": "testpassword123", "email": "nomail@example.com",
	}, nil)
	if reg.Code != 201 {
		t.Fatalf("registro sem provedor deve continuar funcionando: %d", reg.Code)
	}
	response := authJSON(t, app, "/api/auth/forgot", map[string]string{"email": "nomail@example.com"}, nil)
	if response.Code != 503 {
		t.Fatalf("não pode alegar recuperação ativa sem provedor: %d", response.Code)
	}
	app.mu.Lock()
	for id, u := range app.db.Users {
		u.EmailVerified = true
		app.db.Users[id] = u
		app.db.EmailChallenges[challengeKey(id, "reset")] = EmailChallenge{
			UserID: id, Email: u.Email, Purpose: "reset",
			Hash: codeHash(id, "reset", "0123456789ABCDEF"), ExpiresAt: time.Now().Add(-time.Minute).UnixMilli(),
		}
	}
	app.mu.Unlock()
	expired := authJSON(t, app, "/api/auth/reset", map[string]string{
		"email": "nomail@example.com", "code": "0123-4567-89AB-CDEF", "password": "freshpassword2026",
	}, nil)
	if expired.Code == 200 {
		t.Fatal("código expirado foi aceito")
	}
}
