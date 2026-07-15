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
		Version:         "0.1.4",
		BindHost:        "0.0.0.0",
		StatusPort:      9096,
		LocalBaseURL:    "http://127.0.0.1:9091",
		MeshBridgeURL:   "http://127.0.0.1:9095",
		ScanIntervalSec: 20,
		ICMPEnabled:     false,
		DataDir:         "/tmp/meshsniff",
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
		"MeshSniff  v0.1.4",
		"Web UI",
		"9096",
		"LAN",
		"192.168.0.125",
		"Base Station",
		"MeshBridge",
		"ICMP",
		"/tmp/meshsniff",
		"╚═",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("banner missing %q\n%s", want, out)
		}
	}
}
