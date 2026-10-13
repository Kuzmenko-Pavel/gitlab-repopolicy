//go:build e2e

// Package e2e runs scenarios against GitLab CE in docker compose (design §8.3).
// TestMain bootstraps the stand before the tests.
package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	s, err := newStand()
	if err == nil {
		err = s.bootstrap(ctx)
	}
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e stand:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
