package tcpprobe

import (
	"context"
	"testing"
	"time"
)

func TestSweepEmptyRange(t *testing.T) {
	err := Sweep(context.Background(), "127.0.0.1", 5, 4, time.Millisecond, 4, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSweepFindsLocalhostListenerIfAny(t *testing.T) {
	// Soft check: sweep a tiny range; should complete without error.
	var open []int
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := Sweep(ctx, "127.0.0.1", 9096, 9096, 200*time.Millisecond, 8, func(p int) {
		open = append(open, p)
	}, nil)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatal(err)
	}
	t.Logf("open on 9096: %v", open)
}
