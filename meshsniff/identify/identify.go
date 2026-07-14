package identify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/JohnDovey/WalkieTalkie/core/sniff"
)

// Probe tries GET /sniff then GET /api/sniff on host:port.
func Probe(host string, port int, timeout time.Duration) (*sniff.IdentifyPayload, string, error) {
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	client := &http.Client{Timeout: timeout}
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
