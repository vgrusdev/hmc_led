package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

// AuthMiddleware handles Basic Authentication
type AuthMiddleware struct {
	username string
	password string
	realm    string
	mu       sync.RWMutex
}

// NewAuthMiddleware creates a new authentication middleware
func NewAuthMiddleware(config *viper.Viper) *AuthMiddleware {

	realm := config.GetString("server_auth_realm")
	user := config.GetString("server_user")
	passwd := config.GetString("server_passwd")
	if realm == "" || user == "" || passwd == "" {
		return nil
	}
	auth := &AuthMiddleware{
		realm:    realm,
		username: user,
		password: passwd,
	}
	log.Infof("Srv basic auth setup. realm: %s", realm)
	return auth
}

// Middleware returns the authentication middleware handler
func (a *AuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip authentication for certain paths
		if r.URL.Path == "/health" || r.URL.Path == "/status" {
			next.ServeHTTP(w, r)
			return
		}
		if !a.authenticate(r) {
			log.Warnf("Srv Unauthorized request from %s", r.RemoteAddr)
			a.askForCredentials(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authenticate checks the credentials
func (a *AuthMiddleware) authenticate(r *http.Request) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}
	authParts := strings.SplitN(authHeader, " ", 2)
	if len(authParts) != 2 || authParts[0] != "Basic" {
		return false
	}
	payload, err := base64.StdEncoding.DecodeString(authParts[1])
	if err != nil {
		return false
	}
	credentials := strings.SplitN(string(payload), ":", 2)
	if len(credentials) != 2 {
		return false
	}
	return credentials[0] == a.username && credentials[1] == a.password
}

// askForCredentials prompts for authentication
func (a *AuthMiddleware) askForCredentials(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Basic realm="%s"`, a.realm))
	http.Error(w, "Unauthorized", http.StatusUnauthorized)
}
