package replicated

import (
	"math/rand/v2"
	"testing"
	"time"

	"devboard/internal/team/infra/rqlite/rqlitetest"
)

func TestSeededRandomHostRestartsPreserveCommittedHistory(t *testing.T) {
	c := rqlitetest.New(t, rqlitetest.Options{Nodes: 3})
	hosts := hostsOf(t, c)
	k := seed(t, hosts[0])
	rng := rand.New(rand.NewPCG(42, 1701))
	var committed []string
	for round := range 4 {
		stopped := rng.IntN(3)
		c.Node(stopped).Kill()
		committed = append(committed, noteSoon(t, hosts[(stopped+1)%3], k, 30*time.Second))
		c.Node(stopped).Start()
		c.WaitMembers(30*time.Second, 3)
		settle(t, hosts...)
		for i, h := range hosts {
			have := notes(h, k)
			for _, text := range committed {
				if !have[text] {
					t.Fatalf("round %d host %d lost %s", round, i, text)
				}
			}
		}
	}
}
