package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	sessionCookie = "cpc_session"
	stateCookie   = "cpc_oauth"
	sessionLength = 30 * 24 * time.Hour
)

type auth struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	secret   []byte
	allowed  []string
	secure   bool
	devUser  string
}

type ctxKey struct{}

func newAuth(ctx context.Context, issuer, clientID, baseURL, secret string, allowed []string, secure bool) (*auth, error) {
	if len(secret) < 32 {
		return nil, errors.New("SESSION_SECRET must be at least 32 characters")
	}
	a := &auth{secret: []byte(secret), allowed: allowed, secure: secure}
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc provider %s: %w", issuer, err)
	}
	a.verifier = provider.Verifier(&oidc.Config{ClientID: clientID})
	a.oauth = oauth2.Config{
		ClientID:    clientID,
		Endpoint:    provider.Endpoint(),
		RedirectURL: strings.TrimSuffix(baseURL, "/") + "/auth/callback",
		Scopes:      []string{oidc.ScopeOpenID, "email", "profile"},
	}
	return a, nil
}

func (a *auth) sign(value string) string {
	m := hmac.New(sha256.New, a.secret)
	m.Write([]byte(value))
	return value + "|" + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *auth) unsign(signed string) (string, bool) {
	i := strings.LastIndex(signed, "|")
	if i < 0 {
		return "", false
	}
	value := signed[:i]
	return value, hmac.Equal([]byte(a.sign(value)), []byte(signed))
}

func (a *auth) user(r *http.Request) string {
	if a.devUser != "" {
		return a.devUser
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return ""
	}
	value, ok := a.unsign(string(raw))
	if !ok {
		return ""
	}
	email, expires, ok := strings.Cut(value, "|")
	if !ok {
		return ""
	}
	exp, err := strconv.ParseInt(expires, 10, 64)
	if err != nil || time.Now().Unix() > exp || !a.isAllowed(email) {
		return ""
	}
	return email
}

func (a *auth) isAllowed(email string) bool {
	return slices.Contains(a.allowed, strings.ToLower(email))
}

func (a *auth) require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email := a.user(r)
		if email == "" {
			http.Redirect(w, r, "/auth/login?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, email)))
	})
}

func currentUser(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}

func randomString() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (a *auth) login(w http.ResponseWriter, r *http.Request) {
	if a.devUser != "" {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	state, verifier := randomString(), oauth2.GenerateVerifier()
	next := r.URL.Query().Get("next")
	if !strings.HasPrefix(next, "/admin") {
		next = "/admin"
	}
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: base64.RawURLEncoding.EncodeToString([]byte(a.sign(state + "|" + verifier + "|" + next))),
		Path: "/auth/", MaxAge: 600, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (a *auth) callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(stateCookie)
	if err != nil {
		http.Error(w, "login expired, try again", http.StatusBadRequest)
		return
	}
	raw, _ := base64.RawURLEncoding.DecodeString(c.Value)
	value, ok := a.unsign(string(raw))
	parts := strings.SplitN(value, "|", 3)
	if !ok || len(parts) != 3 || parts[0] != r.URL.Query().Get("state") {
		http.Error(w, "login state mismatch, try again", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Path: "/auth/", MaxAge: -1})

	tok, err := a.oauth.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(parts[1]))
	if err != nil {
		log.Printf("oauth exchange: %v", err)
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idTok, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		log.Printf("id token: %v", err)
		http.Error(w, "login failed", http.StatusBadGateway)
		return
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if err := idTok.Claims(&claims); err != nil || !a.isAllowed(claims.Email) {
		log.Printf("login refused for %q", claims.Email)
		http.Error(w, "This account is not allowed to use the attendance pages.", http.StatusForbidden)
		return
	}
	exp := time.Now().Add(sessionLength)
	session := a.sign(strings.ToLower(claims.Email) + "|" + strconv.FormatInt(exp.Unix(), 10))
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: base64.RawURLEncoding.EncodeToString([]byte(session)),
		Path: "/", Expires: exp, HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteLaxMode,
	})
	log.Printf("login %s", claims.Email)
	http.Redirect(w, r, parts[2], http.StatusSeeOther)
}

func (a *auth) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
