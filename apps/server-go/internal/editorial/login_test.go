package editorial

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

type loginStoreStub struct {
	author LoginAuthor
	err    error
}

func (s loginStoreStub) AuthorByEmail(context.Context, string) (LoginAuthor, error) {
	return s.author, s.err
}

type passwordVerifierStub bool

func (v passwordVerifierStub) Verify(string, string) bool { return bool(v) }

type tokenSignerStub struct {
	token string
	err   error
}

func (s tokenSignerStub) Sign(Identity) (string, error) { return s.token, s.err }

func TestLoginServiceReturnsCompatibleResult(t *testing.T) {
	author := LoginAuthor{ID: 7, Name: "Editor", Email: "editor@example.invalid", Role: "admin", PasswordHash: "hash"}
	service := NewLoginService(loginStoreStub{author: author}, passwordVerifierStub(true), tokenSignerStub{token: "synthetic-token"})
	result, err := service.Login(context.Background(), author.Email, "password")
	if err != nil {
		t.Fatal(err)
	}
	if result.Token != "synthetic-token" || result.User != (AuthUser{ID: 7, Name: "Editor", Email: author.Email, Role: "admin"}) {
		t.Fatalf("unexpected login result: %#v", result)
	}
}

func TestLoginServiceErrorIdentity(t *testing.T) {
	storeFailure := errors.New("store failed")
	signingFailure := errors.New("signing failed")
	author := LoginAuthor{ID: 7, Email: "editor@example.invalid", Role: "admin", PasswordHash: "hash"}
	tests := []struct {
		name    string
		service *LoginService
		want    error
	}{
		{"unknown author", NewLoginService(loginStoreStub{err: sql.ErrNoRows}, passwordVerifierStub(true), tokenSignerStub{}), ErrInvalidCredentials},
		{"wrong password", NewLoginService(loginStoreStub{author: author}, passwordVerifierStub(false), tokenSignerStub{}), ErrInvalidCredentials},
		{"canceled store", NewLoginService(loginStoreStub{err: context.Canceled}, passwordVerifierStub(true), tokenSignerStub{}), context.Canceled},
		{"store failure", NewLoginService(loginStoreStub{err: storeFailure}, passwordVerifierStub(true), tokenSignerStub{}), storeFailure},
		{"signing failure", NewLoginService(loginStoreStub{author: author}, passwordVerifierStub(true), tokenSignerStub{err: signingFailure}), signingFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.service.Login(context.Background(), author.Email, "password")
			if !errors.Is(err, test.want) {
				t.Fatalf("Login() error = %v, want errors.Is(_, %v)", err, test.want)
			}
		})
	}
}
