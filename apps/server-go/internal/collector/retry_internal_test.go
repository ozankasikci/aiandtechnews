package collector

import "time"

// Tests use fetchers that fail on purpose; do not wait the real retry pause.
func init() { feedRetryDelay = time.Millisecond }
