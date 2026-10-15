package conformance

import (
	"os"
	"testing"

	"devboard/internal/relay/relaytest"
)

func TestInProcessRelay(t *testing.T) {
	s := relaytest.New()
	defer s.Close()
	Run(t, s.URL)
}

// TestCloudflareWorker runs the same suite against a real Worker: the local one
// (`npm run dev` in relay/worker, http://localhost:8787) or a deployed one.
func TestCloudflareWorker(t *testing.T) {
	base := os.Getenv("WERKBORD_RELAY_TEST_URL")
	if base == "" {
		t.Skip("set WERKBORD_RELAY_TEST_URL to run against a real Worker")
	}
	Run(t, base)
}
