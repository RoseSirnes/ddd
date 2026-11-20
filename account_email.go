package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"time"
)

// Codes are random 64-bit values. Only their hashes are stored on disk.
// A verified email is required for password reset. Email addresses are never
// exposed by public profiles or friend search.
type EmailChallenge struct {
	UserID    string `json:"userId"`
	Email     string `json:"email"`
	Purpose   string `json:"purpose"`
	Hash      string `json:"hash"`
	ExpiresAt int64  `json:"expiresAt"`
	Attempts  int    `json:"attempts"`
}

type mailSender func(context.Context, string, string, string) error

var emailPattern = regexp.MustCompile(`^[^\s@<>]{1,64}@[A-Za-z0-9][A-Za-z0-9.-]{0,252}\.[A-Za-z]{2,63}$`)
var codePattern = regexp.MustCompile(`^[A-F0-9]{16}$`)

func normalizedEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	if len(email) > 254 || !emailPattern.MatchString(email) || strings.Contains(email, "..") {
		return "", errors.New("endereço de e-mail inválido")
	}
	return email, nil
}

func cleanCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(code), "-", ""), " ", ""))
}

func challengeKey(userID, purpose string) string {
	return userID + ":" + purpose
}

func codeHash(userID, purpose, code string) string {
	return sha("ddd-email-v1:" + userID + ":" + purpose + ":" + code)
}

func (a *App) emailInUseLocked(email, exceptUserID string) bool {
	for _, other := range a.db.Users {
		if other.ID != exceptUserID && other.Email == email {
			return true
		}
	}
	return false
}

func privateUser(u User) map[string]any {
	return map[string]any{
		"id": u.ID, "nickname": u.Nickname, "avatarImageId": nullStr(u.AvatarImageID),
		"bio": u.Bio, "status": u.Status, "createdAt": u.CreatedAt,
		"email": u.Email,
	}
}

func (a *App) issueChallengeLocked(userID, email, purpose string) string {
	code := strings.ToUpper(randHex(8))
	expires := 15 * time.Minute
	a.db.EmailChallenges[challengeKey(userID, purpose)] = EmailChallenge{
		UserID: userID, Email: email, Purpose: purpose,
		Hash:      codeHash(userID, purpose, code),
		ExpiresAt: time.Now().Add(expires).UnixMilli(),
	}
	for k, v := range a.db.EmailChallenges {
		if v.ExpiresAt < now() {
			delete(a.db.EmailChallenges, k)
		}
	}
	return code
}

func (a *App) dropChallenge(userID, purpose, code string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := challengeKey(userID, purpose)
	challenge, ok := a.db.EmailChallenges[key]
	if ok && hmac.Equal([]byte(challenge.Hash), []byte(codeHash(userID, purpose, code))) {
		delete(a.db.EmailChallenges, key)
		_ = a.save()
	}
}

func productionMailerFromEnv() mailSender {
	apiKey, from := strings.TrimSpace(os.Getenv("RESEND_API_KEY")), strings.TrimSpace(os.Getenv("DDD_FROM_EMAIL"))
	if apiKey == "" || from == "" {
		return nil
	}
	if _, err := mail.ParseAddress(from); err != nil {
		log.Print("DDD_FROM_EMAIL inválido; envio de e-mails desabilitado")
		return nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	return func(ctx context.Context, recipient, subject, body string) error {
		payload, err := json.Marshal(map[string]any{
			"from": from, "to": []string{recipient}, "subject": subject, "text": body,
		})
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.resend.com/emails", bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated &&
			resp.StatusCode != http.StatusAccepted {
			return fmt.Errorf("provedor de e-mail retornou HTTP %d", resp.StatusCode)
		}
		return nil
	}
}

func (a *App) sendSecurityEmail(ctx context.Context, to, purpose, code string) error {
	if a.emailSender == nil {
		return errors.New("envio de e-mail indisponível")
	}
	pretty := code[:4] + "-" + code[4:8] + "-" + code[8:12] + "-" + code[12:]
	subject := "DDD — recuperar sua conta"
	body := "Seu código para redefinir a senha do DDD é: " + pretty + "\\n\\nO código vale por 15 minutos. Não compartilhe este código com ninguém.\\nSe você não pediu a recuperação, ignore este e-mail."
	return a.emailSender(ctx, to, subject, body)
}

func (a *App) handlePublicEmailRoutes(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.URL.Path == "/api/auth/email/status" && r.Method == http.MethodGet:
		jsonRes(w, 200, map[string]any{"available": a.emailSender != nil})
		return true
	case r.URL.Path == "/api/auth/forgot" && r.Method == http.MethodPost:
		var req struct {
			Email string `json:"email"`
		}
		if !readJSON(w, r, &req) {
			return true
		}
		email, err := normalizedEmail(req.Email)
		if err != nil {
			apiErr(w, 400, "Informe um e-mail válido.")
			return true
		}
		if a.emailSender == nil {
			apiErr(w, 503, "A recuperação por e-mail ainda não está disponível. O administrador precisa configurar o envio.")
			return true
		}
		a.mu.Lock()
		ip := remoteIP(r)
		if !a.limit("forgot:ip:"+ip, 8, time.Hour) ||
			!a.limit("forgot:email:"+sha(email), 4, time.Hour) {
			a.mu.Unlock()
			apiErr(w, 429, "Muitas tentativas. Tente mais tarde.")
			return true
		}
		var user User
		for _, v := range a.db.Users {
			if v.Email == email {
				user = v
				break
			}
		}
		code := ""
		if user.ID != "" {
			code = a.issueChallengeLocked(user.ID, user.Email, "reset")
			if err := a.save(); err != nil {
				delete(a.db.EmailChallenges, challengeKey(user.ID, "reset"))
				code = ""
				log.Print("Falha ao salvar código de recuperação: ", err)
			}
		}
		a.mu.Unlock()
		if code != "" {
			if err := a.sendSecurityEmail(r.Context(), user.Email, "reset", code); err != nil {
				a.dropChallenge(user.ID, "reset", code)
				log.Print("Não foi possível enviar e-mail de recuperação: ", err)
			}
		}
		// Do not disclose whether an account is associated with this address.
		jsonRes(w, 200, map[string]any{"message": "Se esse e-mail estiver cadastrado no DDD, você receberá um código para recuperar a senha."})
		return true
	case r.URL.Path == "/api/auth/reset" && r.Method == http.MethodPost:
		a.mu.Lock()
		allowed := a.limit("reset:ip:"+remoteIP(r), 12, time.Hour)
		a.mu.Unlock()
		if !allowed {
			apiErr(w, 429, "Muitas tentativas. Tente mais tarde.")
			return true
		}
		var req struct {
			Email    string `json:"email"`
			Code     string `json:"code"`
			Password string `json:"password"`
		}
		if !readJSON(w, r, &req) {
			return true
		}
		email, err := normalizedEmail(req.Email)
		code := cleanCode(req.Code)
		if err != nil || !codePattern.MatchString(code) {
			apiErr(w, 400, "Código inválido ou expirado. Solicite um novo.")
			return true
		}
		if len(req.Password) < 8 || len(req.Password) > 128 {
			apiErr(w, 400, "A senha deve ter entre 8 e 128 caracteres.")
			return true
		}
		// Password derivation is deliberately done outside the DB mutex.
		passwordHash := hashPassword(req.Password)
		a.mu.Lock()
		defer a.mu.Unlock()
		var user User
		for _, v := range a.db.Users {
			if v.Email == email {
				user = v
				break
			}
		}
		key := challengeKey(user.ID, "reset")
		challenge, present := a.db.EmailChallenges[key]
		valid := present && user.ID != "" && challenge.Email == email &&
			challenge.ExpiresAt >= now() && challenge.Attempts < 5 &&
			hmac.Equal([]byte(challenge.Hash), []byte(codeHash(user.ID, "reset", code)))
		if !valid {
			if present {
				challenge.Attempts++
				if challenge.Attempts >= 5 {
					delete(a.db.EmailChallenges, key)
				} else {
					a.db.EmailChallenges[key] = challenge
				}
				_ = a.save()
			}
			apiErr(w, 400, "Código inválido ou expirado. Solicite um novo.")
			return true
		}
		oldHash := user.PasswordHash
		oldSessions := map[string]Session{}
		for key, session := range a.db.Sessions {
			if session.UserID == user.ID {
				oldSessions[key] = session
				delete(a.db.Sessions, key)
			}
		}
		user.PasswordHash = passwordHash
		a.db.Users[user.ID] = user
		delete(a.db.EmailChallenges, key)
		if err := a.save(); err != nil {
			user.PasswordHash = oldHash
			a.db.Users[user.ID] = user
			for k, v := range oldSessions {
				a.db.Sessions[k] = v
			}
			a.db.EmailChallenges[key] = challenge
			apiErr(w, 500, "Não foi possível salvar a nova senha.")
			return true
		}
		jsonRes(w, 200, map[string]any{"message": "Senha redefinida! Entre com seu nick e a nova senha."})
		return true
	}
	return false
}

// O e-mail é salvo imediatamente após confirmar a senha atual.
// A recuperação continua exigindo um código enviado ao endereço cadastrado.
func (a *App) handleAccountEmailRoutes(w http.ResponseWriter, r *http.Request, auth User) bool {
	if (r.URL.Path != "/api/auth/email/update" && r.URL.Path != "/api/auth/email/request") || r.Method != http.MethodPost {
		return false
	}
	var req struct {
		Email string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) { return true }
	email, err := normalizedEmail(req.Email)
	if err != nil { apiErr(w,400,"Informe um e-mail válido."); return true }
	if !checkPassword(req.Password,auth.PasswordHash) {
		apiErr(w,401,"Informe sua senha atual para alterar o e-mail."); return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	user, exists := a.db.Users[auth.ID]
	if !exists || user.PasswordHash != auth.PasswordHash {
		apiErr(w,401,"Entre novamente na sua conta."); return true
	}
	if a.emailInUseLocked(email,user.ID) {
		apiErr(w,409,"Esse e-mail já está cadastrado."); return true
	}
	if user.Email == email {
		jsonRes(w,200,map[string]any{"message":"Esse já é seu e-mail de recuperação.","user":privateUser(user)})
		return true
	}
	if !a.limit("update:email:"+user.ID,8,time.Hour) {
		apiErr(w,429,"Muitas alterações de e-mail. Tente mais tarde."); return true
	}
	old := user
	oldChallenge, hadChallenge := a.db.EmailChallenges[challengeKey(user.ID,"reset")]
	user.Email = email
	a.db.Users[user.ID] = user
	delete(a.db.EmailChallenges,challengeKey(user.ID,"reset"))
	delete(a.db.EmailChallenges,challengeKey(user.ID,"verify"))
	if err := a.save(); err != nil {
		a.db.Users[user.ID] = old
		if hadChallenge { a.db.EmailChallenges[challengeKey(user.ID,"reset")] = oldChallenge }
		apiErr(w,500,"Não foi possível salvar o novo e-mail.")
		return true
	}
	jsonRes(w,200,map[string]any{"message":"E-mail de recuperação salvo!","user":privateUser(user)})
	return true
}
