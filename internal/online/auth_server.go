package online

import (
	"crypto/tls"
	"encoding/json"
	"eridian/internal/db"
	"eridian/internal/domain"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const authSessionTTL = time.Hour

// authSession is one issued /login token.
type authSession struct {
	accountID int64
	expiresAt time.Time
}

type AuthServerConfig struct {
	AuthServer domain.ServerConfig `json:"auth_server"`
}

type JWTClaims struct {
	AccountID int64  `json:"account_id"`
	Username  string `json:"username"`
	jwt.RegisteredClaims
}

type AuthServer struct {
	host     string
	port     int
	certFile string
	keyFile  string
	srv      *http.Server

	mu        sync.Mutex
	sessions  map[string]authSession
	secretKey string
}

// NewAuthServer creates a new TLS auth server.
func NewAuthServer(host string, port int, certFile, keyFile string) *AuthServer {
	return &AuthServer{
		host:     host,
		port:     port,
		certFile: certFile,
		keyFile:  keyFile,
		sessions: map[string]authSession{},
	}
}

func (s *AuthServer) Serve() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/account", s.handleAccount)
	mux.HandleFunc("/logout", s.handleLogout)

	file, er := os.Open("config/server.json")
	if er != nil {
		log.Printf("[AUTH] failed to open config/server.json: %v", er)
		return er
	}
	defer file.Close()

	var cfg domain.ServerConfig
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		log.Printf("[AUTH] failed to decode config/server.json: %v", err)
		return err
	}

	s.secretKey = cfg.AuthServer.SecretKey

	s.srv = &http.Server{
		Addr:      fmt.Sprintf("%s:%d", s.host, s.port),
		Handler:   mux,
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}

	log.Printf("[AUTH] HTTPS listening on %s", s.srv.Addr)
	err := s.srv.ListenAndServeTLS(s.certFile, s.keyFile)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Shutdown gracefully closes the auth server.
func (s *AuthServer) Shutdown() {
	if s.srv != nil {
		_ = s.srv.Close()
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func (s *AuthServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Username == "" {
		writeError(w, http.StatusBadRequest, "username required")
		return
	}

	if db.IsIPBanned(r.RemoteAddr) {
		writeError(w, http.StatusForbidden, "ip banned")
		return
	}
	account, err := db.GetAccountByUsername(body.Username)
	if err != nil {
		log.Printf("[AUTH] login failed: unknown user=%s", body.Username)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if !db.CheckPassword(account, body.Password) {
		log.Printf("[AUTH] login failed: bad password user=%s", body.Username)
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if account.IsBanned {
		writeError(w, http.StatusForbidden, "account banned")
		return
	}
	if !account.CanPlay {
		writeError(w, http.StatusForbidden, account.CanPlayRejectMessage())
		return
	}

	token, err := s.generateJWTToken(account)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token generation failed")
		return
	}
	expiresAt := time.Now().Add(authSessionTTL)
	s.mu.Lock()
	s.sessions[token] = authSession{accountID: account.AccountID, expiresAt: expiresAt}
	s.mu.Unlock()

	db.UpdateAccountToken(account.AccountID, token) // Update the token in the database

	log.Printf("[AUTH] login ok: user=%s accountID=%d", body.Username, account.AccountID)
	writeJSON(w, http.StatusOK, map[string]any{
		"account_id": account.AccountID,
		"username":   account.Username,
		"token":      token,
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

func (s *AuthServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	token := bearerToken(r)
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}


func (s *AuthServer) handleAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	token := bearerToken(r)
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return
	}

	s.mu.Lock()
	sess, ok := s.sessions[token]
	if ok && time.Now().After(sess.expiresAt) {
		delete(s.sessions, token)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		writeError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	account, err := db.GetAccountByID(sess.accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "account lookup failed")
		return
	}
	characters, err := db.GetCharactersByAccount(sess.accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "character lookup failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"account":    account,
		"characters": characters,
	})
}

// bearerToken extracts the token from "Authorization: Bearer <token>".
func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return ""
}

// JTWtoken
func (s *AuthServer) generateJWTToken(account *domain.Account) (string, error) {
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &JWTClaims{
		AccountID: account.AccountID,
		Username:  account.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.secretKey))
}
