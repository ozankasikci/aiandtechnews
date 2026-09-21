package editorial

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrInvalidCredentials = errors.New("invalid email or password")

type loginStore interface {
	AuthorByEmail(context.Context, string) (LoginAuthor, error)
}

type passwordVerifier interface {
	Verify(hash, password string) bool
}

type tokenSigner interface {
	Sign(Identity) (string, error)
}

type LoginService struct {
	store    loginStore
	password passwordVerifier
	tokens   tokenSigner
}

type LoginResult struct {
	Token string
	User  AuthUser
}

func NewLoginService(store loginStore, password passwordVerifier, tokens tokenSigner) *LoginService {
	return &LoginService{store: store, password: password, tokens: tokens}
}

func (s *LoginService) Login(ctx context.Context, email, password string) (LoginResult, error) {
	author, err := s.store.AuthorByEmail(ctx, email)
	if errors.Is(err, sql.ErrNoRows) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("find login author: %w", err)
	}
	if !s.password.Verify(author.PasswordHash, password) {
		return LoginResult{}, ErrInvalidCredentials
	}
	token, err := s.tokens.Sign(Identity{ID: author.ID, Email: author.Email, Role: author.Role})
	if err != nil {
		return LoginResult{}, fmt.Errorf("sign login token: %w", err)
	}
	return LoginResult{Token: token, User: AuthUser{ID: author.ID, Name: author.Name, Email: author.Email, Role: author.Role}}, nil
}
