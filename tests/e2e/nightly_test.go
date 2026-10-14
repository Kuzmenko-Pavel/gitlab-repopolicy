//go:build e2e && nightly

package e2e

import (
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parallelMerges is the number of merge requests merged at once.
const parallelMerges = 8

// TestNightly_ParallelMerges covers OP-22 and PF-05: merges pressed at the
// same time on one node. GitLab lets one merge per target update win and
// answers 422 to the others (FACTS); the hook must allow every attempt it
// sees within its time limit, and retries merge the rest.
func TestNightly_ParallelMerges(t *testing.T) {
	p := withCIPolicy(t, "parallel")
	alice := user("alice")
	iids := make([]int, parallelMerges)
	for i := range iids {
		branch := fmt.Sprintf("par-%d", i)
		mustCommit(t, alice, p, branch, "main", "ci: parallel change", create(fmt.Sprintf("ci/par-%d.yml", i), "build"))
		iids[i] = openMR(t, alice, p, branch, "main").IID
	}
	for _, iid := range iids {
		waitMergeable(t, p, iid)
	}

	merge := func(iid int) error {
		return gl.api(t.Context(), alice.Token, http.MethodPut, p.api(fmt.Sprintf("/merge_requests/%d/merge", iid)), nil, nil)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	won := 0
	start := time.Now()
	for _, iid := range iids {
		wg.Go(func() {
			if merge(iid) == nil {
				mu.Lock()
				won++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	t.Logf("%d of %d merges won the race in %s", won, parallelMerges, time.Since(start).Round(time.Millisecond))
	require.Positive(t, won)

	for _, iid := range iids {
		waitUntil(t, 3*time.Minute, fmt.Sprintf("!%d merged", iid), func() bool {
			mr := getMR(t, p, iid)
			if mr.State == "merged" {
				return true
			}
			if mr.DetailedMergeStatus == "mergeable" {
				_ = merge(iid)
			}
			return false
		})
	}

	var durations []int64
	for _, e := range auditEvents(t, p.Path) {
		if e.Event != "decision" || e.Ref != "refs/heads/main" || e.Username != "alice" {
			continue
		}
		assert.Equal(t, "allow", e.Decision, "%+v", e)
		durations = append(durations, e.DurationMS)
	}
	require.GreaterOrEqual(t, len(durations), parallelMerges)
	slices.Sort(durations)
	p99 := durations[(len(durations)*99+99)/100-1]
	t.Logf("hook on main: %d calls, p50 %d ms, p99 %d ms", len(durations), durations[len(durations)/2], p99)
	assert.Less(t, p99, int64(10_000), "the hook time limit of architecture §17")
}
