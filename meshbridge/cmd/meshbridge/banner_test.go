package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	mbconfig "github.com/JohnDovey/WalkieTalkie/meshbridge/config"
)

func TestPrintStartupBanner(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	printStartupBanner(startupInfo{
		Version:         "0.1.3",
		BindHost:        "0.0.0.0",
		StatusPort:      9095,
		NodeID:          "test-bridge",
		LocalBaseURL:    "http://127.0.0.1:9091",
		SyncIntervalSec: 30,
		Manual:          []mbconfig.ManualBridge{{Name: "site-b", URL: "http://10.0.0.2:9091"}},
		RunHub:          true,
		HubListenPort:   29191,
		DataDir:         "/tmp/meshbridge",
		ConfigPath:      "/tmp/meshbridge/settings.json",
		LANIPs:          []string{"192.168.0.125"},
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
		"MeshBridge  v0.1.3",
		"Status UI",
		"9095",
		"LAN",
		"192.168.0.125",
		"Node ID",
		"test-bridge",
		"Local Base",
		"Manual",
		"site-b",
		"Punch hub",
		"/tmp/meshbridge",
		"╚═",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("banner missing %q\n%s", want, out)
		}
	}
}
