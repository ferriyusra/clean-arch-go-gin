package csrf

import "time"

// testCSRFTTL is long enough that no test hits expiry by accident; the tests
// that are about expiry set their own clock instead of waiting.
const testCSRFTTL = 12 * time.Hour

// Sinks for the benchmark results, so the compiler cannot delete the work.
var (
	benchToken string
	benchValid bool
)
