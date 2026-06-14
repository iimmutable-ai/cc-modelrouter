package proxy

import (
	"errors"
	"net/http"

	"github.com/iimmutable/cc-modelrouter/internal/auth"
	"github.com/iimmutable/cc-modelrouter/internal/logging"
)

// AuthError is returned by the auth interceptor to signal a 401 response.
type AuthError struct {
	StatusCode int
	Message    string
}

func (e *AuthError) Error() string {
	return e.Message
}

// AuthInterceptor validates Bearer tokens and resolves user identity.
// It must be the first RequestInterceptor in the chain.
type AuthInterceptor struct {
	KeyStore *auth.KeyStore
}

// NewAuthInterceptor creates a new auth interceptor.
func NewAuthInterceptor(ks *auth.KeyStore) *AuthInterceptor {
	return &AuthInterceptor{KeyStore: ks}
}

// Authenticate validates the raw token and returns UserInfo.
// Called from ServeHTTP before entering the interceptor chain.
func (ai *AuthInterceptor) Authenticate(rawToken string) (*auth.UserInfo, error) {
	if rawToken == "" {
		return nil, errors.New("missing token")
	}

	info, err := ai.KeyStore.ValidateKey(rawToken)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, errors.New("invalid API key")
	}

	logging.Debugf("[AUTH] Authenticated: key=%s user=%s group=%s profile=%s",
		info.KeyPrefix, info.UserName, info.GroupName, info.Profile)

	return info, nil
}

// WriteAuthError writes a JSON authentication error response.
func WriteAuthError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"` + message + `"}}`))
}
