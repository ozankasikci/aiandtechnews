package editorial

// Author is the compatibility representation exposed by the public author
// endpoint. Email exposure is retained for Node parity; password hashes are
// deliberately absent. See the README privacy-debt note.
type Author struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Email  string  `json:"email"`
	Avatar *string `json:"avatar"`
	Bio    *string `json:"bio"`
	Role   string  `json:"role"`
}

// LoginAuthor is private authentication data. It is never serialized.
type LoginAuthor struct {
	ID           int64
	Name         string
	Email        string
	Role         string
	PasswordHash string
}

type AuthUser struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}
