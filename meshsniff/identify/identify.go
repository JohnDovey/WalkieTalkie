package identify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/sniff"
)

// SkipHTTPIdentify is true for ports that speak a non-HTTP protocol.
// Probing them with GET /sniff confuses Go's HTTP keep-alive pool and logs
// "Unsolicited response received on idle HTTP channel" (telnet/SSH/BinkP/VNC banners).
func SkipHTTPIdentify(port int) bool {
	switch port {
	case 22, // ssh
		53,    // dns
		88,    // kerberos
		139,   // netbios
		445,   // smb
		548,   // afp
		631,   // ipp
		2323,  // VirtBBS telnet
		3232,  // VirtBBS ssh
		3389,  // rdp
		5900,  // vnc
		9998,  // VirtBBS VirtAnd API (length-prefixed JSON, not HTTP)
		24554, // VirtBBS BinkP
		24555: // VirtBBS BinkP
		return true
	default:
		return false
	}
}

// Probe tries GET /sniff then GET /api/sniff on host:port.
func Probe(host string, port int, timeout time.Duration) (*sniff.IdentifyPayload, string, error) {
	if port <= 0 {
		return nil, "", fmt.Errorf("identify: bad port")
	}
	if SkipHTTPIdentify(port) {
		return nil, "", fmt.Errorf("identify: port %d is not HTTP", port)
	}
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: timeout,
			// Avoid HTTP/2 persuasion on odd ports.
			ForceAttemptHTTP2: false,
		},
	}
	paths := []string{"/sniff", "/api/sniff"}
	var lastErr error
	for _, path := range paths {
		url := fmt.Sprintf("http://%s:%d%s", host, port, path)
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("%s: %s", url, resp.Status)
			continue
		}
		var p sniff.IdentifyPayload
		err = json.NewDecoder(resp.Body).Decode(&p)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if p.MeshID == "" && p.Name == "" {
			lastErr = fmt.Errorf("%s: empty identify", url)
			continue
		}
		return &p, url, nil
	}
	return nil, "", lastErr
}
