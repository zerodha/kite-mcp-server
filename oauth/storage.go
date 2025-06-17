package oauth

import (
	"context"
	"sync"
	"time"

	"github.com/ory/fosite"
	"github.com/ory/x/errorsx"
)

// InMemoryStore is a simple in-memory storage for Fosite.
// WARNING: Do not use this in production. It is for demonstration purposes only.
type InMemoryStore struct {
	sync.RWMutex
	clients                map[string]fosite.Client
	authorizeCodes         map[string]fosite.Requester
	accessTokens           map[string]fosite.Requester
	refreshTokens          map[string]fosite.Requester
	pkceRequests           map[string]fosite.Requester
	accessTokenRequestIDs  map[string]string
	refreshTokenRequestIDs map[string]string
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		clients:                make(map[string]fosite.Client),
		authorizeCodes:         make(map[string]fosite.Requester),
		accessTokens:           make(map[string]fosite.Requester),
		refreshTokens:          make(map[string]fosite.Requester),
		pkceRequests:           make(map[string]fosite.Requester),
		accessTokenRequestIDs:  make(map[string]string),
		refreshTokenRequestIDs: make(map[string]string),
	}
}

// AddClient is a helper to add clients to the store. This is called by our manual registration handler.
func (s *InMemoryStore) AddClient(client fosite.Client) {
	s.Lock()
	defer s.Unlock()
	s.clients[client.GetID()] = client
}

// GetClient loads the client by its ID.
func (s *InMemoryStore) GetClient(_ context.Context, id string) (fosite.Client, error) {
	s.RLock()
	defer s.RUnlock()
	c, ok := s.clients[id]
	if !ok {
		return nil, errorsx.WithStack(fosite.ErrNotFound.WithDebugf("Client with id %s does not exist", id))
	}
	return c, nil
}

// ClientAssertionJWTValid is a no-op for this example.
func (s *InMemoryStore) ClientAssertionJWTValid(ctx context.Context, jti string) error {
	return nil
}

// SetClientAssertionJWT is a no-op for this example.
func (s *InMemoryStore) SetClientAssertionJWT(ctx context.Context, jti string, exp time.Time) error {
	return nil
}

// Implement rfc7591.ClientRegistrationManager
func (s *InMemoryStore) CreateClient(ctx context.Context, client fosite.Client) error {
	s.AddClient(client)
	return nil
}

func (s *InMemoryStore) UpdateClient(ctx context.Context, client fosite.Client) error {
	s.AddClient(client)
	return nil
}

func (s *InMemoryStore) DeleteClient(ctx context.Context, id string) error {
	s.Lock()
	defer s.Unlock()
	delete(s.clients, id)
	return nil
}

func (s *InMemoryStore) GetClients(ctx context.Context, limit, offset int) (map[string]fosite.Client, error) {
	s.RLock()
	defer s.RUnlock()
	return s.clients, nil
}

func (s *InMemoryStore) CreateAuthorizeCodeSession(_ context.Context, signature string, requester fosite.Requester) error {
	s.Lock()
	defer s.Unlock()
	s.authorizeCodes[signature] = requester
	return nil
}

func (s *InMemoryStore) GetAuthorizeCodeSession(_ context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	s.RLock()
	defer s.RUnlock()
	req, ok := s.authorizeCodes[signature]
	if !ok {
		return nil, errorsx.WithStack(fosite.ErrNotFound)
	}
	return req, nil
}

func (s *InMemoryStore) InvalidateAuthorizeCodeSession(_ context.Context, signature string) error {
	s.Lock()
	defer s.Unlock()
	delete(s.authorizeCodes, signature)
	return nil
}

func (s *InMemoryStore) CreateAccessTokenSession(_ context.Context, signature string, requester fosite.Requester) error {
	s.Lock()
	defer s.Unlock()
	s.accessTokens[signature] = requester
	s.accessTokenRequestIDs[requester.GetID()] = signature
	return nil
}

func (s *InMemoryStore) GetAccessTokenSession(_ context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	s.RLock()
	defer s.RUnlock()
	req, ok := s.accessTokens[signature]
	if !ok {
		return nil, errorsx.WithStack(fosite.ErrNotFound)
	}
	return req, nil
}

func (s *InMemoryStore) DeleteAccessTokenSession(_ context.Context, signature string) error {
	s.Lock()
	defer s.Unlock()
	delete(s.accessTokens, signature)
	return nil
}

// CreateRefreshTokenSession now correctly matches the oauth2.RefreshTokenStorage interface.
func (s *InMemoryStore) CreateRefreshTokenSession(_ context.Context, signature string, accessSignature string, requester fosite.Requester) error {
	s.Lock()
	defer s.Unlock()
	s.refreshTokens[signature] = requester
	s.refreshTokenRequestIDs[requester.GetID()] = signature
	return nil
}

func (s *InMemoryStore) GetRefreshTokenSession(_ context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	s.RLock()
	defer s.RUnlock()
	req, ok := s.refreshTokens[signature]
	if !ok {
		return nil, errorsx.WithStack(fosite.ErrNotFound)
	}
	return req, nil
}

func (s *InMemoryStore) DeleteRefreshTokenSession(_ context.Context, signature string) error {
	s.Lock()
	defer s.Unlock()
	delete(s.refreshTokens, signature)
	return nil
}

func (s *InMemoryStore) RevokeRefreshToken(ctx context.Context, requestID string) error {
	s.Lock()
	defer s.Unlock()
	if signature, found := s.refreshTokenRequestIDs[requestID]; found {
		delete(s.refreshTokens, signature)
		delete(s.refreshTokenRequestIDs, requestID)
	}
	return nil
}

func (s *InMemoryStore) RevokeAccessToken(ctx context.Context, requestID string) error {
	s.Lock()
	defer s.Unlock()
	if signature, found := s.accessTokenRequestIDs[requestID]; found {
		delete(s.accessTokens, signature)
		delete(s.accessTokenRequestIDs, requestID)
	}
	return nil
}

func (s *InMemoryStore) CreatePKCERequestSession(_ context.Context, signature string, requester fosite.Requester) error {
	s.Lock()
	defer s.Unlock()
	s.pkceRequests[signature] = requester
	return nil
}

func (s *InMemoryStore) GetPKCERequestSession(_ context.Context, signature string, _ fosite.Session) (fosite.Requester, error) {
	s.RLock()
	defer s.RUnlock()
	req, ok := s.pkceRequests[signature]
	if !ok {
		return nil, errorsx.WithStack(fosite.ErrNotFound)
	}
	return req, nil
}

func (s *InMemoryStore) DeletePKCERequestSession(_ context.Context, signature string) error {
	s.Lock()
	defer s.Unlock()
	delete(s.pkceRequests, signature)
	return nil
}

func (s *InMemoryStore) Authenticate(ctx context.Context, name string, secret string) (string, error) {
	// This is a placeholder for username/password authentication, not used by our flows but required by some interfaces.
	return "", fosite.ErrNotFound
}

func (s *InMemoryStore) RotateRefreshToken(ctx context.Context, requestID string, newSignature string) error {
	// A more complex implementation would handle token rotation gracefully.
	// For this example, we simply revoke the old token.
	return s.RevokeRefreshToken(ctx, requestID)
}
