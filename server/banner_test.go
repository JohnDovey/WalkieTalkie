package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPrintStartupBanner(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	printStartupBanner(startupInfo{
		Version:             "1.7.0",
		Name:                "Base Station: test",
		DeviceID:            "dev-abc",
		Platform:            "desktop-darwin",
		WebPort:             9091,
		SignalPort:          12345,
		RelayPort:           23456,
		DataDir:             "/tmp/wt",
		AudioDisabled:       false,
		RelayEnabled:        true,
		RelayThreshold:      10,
		SyncIntervalSeconds: 30,
	})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"╔═",
		"WalkieTalkie Base Station  v1.7.0",
		"Web UI / API",
		"9091",
		"SFU relay",
		"23456",
		"dev-abc",
		"╚═",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("banner missing %q\n%s", want, out)
		}
	}
}
