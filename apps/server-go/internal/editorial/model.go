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
