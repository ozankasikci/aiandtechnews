package media

import "io"

// SetRandomForTest replaces the crypto/rand source used for upload names.
func (u *Uploads) SetRandomForTest(random io.Reader) { u.random = random }
