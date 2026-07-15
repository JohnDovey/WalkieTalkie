package engine

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/JohnDovey/WalkieTalkie/meshsniff/graph"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/tcpprobe"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/virtbbs"
)

// FullScan tracks a background TCP 1–65535 sweep for one host.
type FullScan struct {
	Host      string    `json:"host"`
	NodeID    string    `json:"nodeId"`
	Status    string    `json:"status"` // running|done|cancelled|error
	Checked   int       `json:"checked"`
	Total     int       `json:"total"`
	OpenFound int       `json:"openFound"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type fullScanJob struct {
	cancel context.CancelFunc
	info   *FullScan
}

type engineScanBook struct {
	mu   sync.Mutex
	jobs map[string]*fullScanJob
}

var scanBooks sync.Map // *Engine → *engineScanBook

func (e *Engine) book() *engineScanBook {
	v, _ := scanBooks.LoadOrStore(e, &engineScanBook{jobs: map[string]*fullScanJob{}})
	return v.(*engineScanBook)
}

// StartFullPortScan begins a background TCP sweep of ports 1–65535 on host.
// Open ports are merged into the graph as they are found.
func (e *Engine) StartFullPortScan(host string) (*FullScan, error) {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("full scan requires an IPv4 address")
	}
	host = ip.String()
	nodeID := "host:" + host

	book := e.book()
	book.mu.Lock()
	if old, ok := book.jobs[host]; ok && old.info.Status == "running" {
		cp := *old.info
		book.mu.Unlock()
		return &cp, fmt.Errorf("full port scan already running for %s", host)
	}
	ctx, cancel := context.WithCancel(context.Background())
	info := &FullScan{
		Host:    host,
		NodeID:  nodeID,
		Status:  "running",
		Total:   65535,
		Started: time.Now(),
	}
	book.jobs[host] = &fullScanJob{cancel: cancel, info: info}
	book.mu.Unlock()

	e.Graph.Upsert(graph.Node{
		ID:  nodeID,
		IPs: []string{host},
		Detail: map[string]any{
			"fullScan": map[string]any{
				"status":  "running",
				"checked": 0,
				"total":   65535,
				"open":    0,
				"started": info.Started,
			},
		},
		DiscoveryMethods: []string{"full-scan"},
	})
	st := e.Graph.Snapshot().Status
	st.Message = fmt.Sprintf("full TCP scan %s started", host)
	e.Graph.SetStatus(st)

	go e.runFullPortScan(ctx, host, nodeID, info)
	return info, nil
}

// CancelFullPortScan stops a running full scan for host.
func (e *Engine) CancelFullPortScan(host string) bool {
	book := e.book()
	book.mu.Lock()
	defer book.mu.Unlock()
	job, ok := book.jobs[host]
	if !ok || job.info.Status != "running" {
		return false
	}
	job.cancel()
	return true
}

// FullScanStatus returns the latest full-scan info for host, if any.
func (e *Engine) FullScanStatus(host string) *FullScan {
	book := e.book()
	book.mu.Lock()
	defer book.mu.Unlock()
	if job, ok := book.jobs[host]; ok {
		cp := *job.info
		return &cp
	}
	return nil
}

func (e *Engine) runFullPortScan(ctx context.Context, host, nodeID string, info *FullScan) {
	defer func() {
		book := e.book()
		book.mu.Lock()
		if info.Status == "running" {
			info.Status = "done"
		}
		info.Finished = time.Now()
		book.mu.Unlock()
		e.Graph.Upsert(graph.Node{
			ID: nodeID,
			Detail: map[string]any{
				"fullScan": map[string]any{
					"status":   info.Status,
					"checked":  info.Checked,
					"total":    info.Total,
					"open":     info.OpenFound,
					"started":  info.Started,
					"finished": info.Finished,
					"error":    info.Error,
				},
			},
		})
		st := e.Graph.Snapshot().Status
		st.Message = fmt.Sprintf("full TCP scan %s %s (%d open)", host, info.Status, info.OpenFound)
		e.Graph.SetStatus(st)
		log.Printf("meshsniff full scan %s: %s checked=%d open=%d", host, info.Status, info.Checked, info.OpenFound)

		var open []int
		for _, n := range e.Graph.Nodes() {
			if n.ID == nodeID {
				open = n.OpenPorts
				break
			}
		}
		if virtbbs.LooksLike(open) {
			e.applyVirtBBS(host, open)
		}
	}()

	err := tcpprobe.Sweep(ctx, host, 1, 65535, 150*time.Millisecond, 256,
		func(port int) {
			svc := graph.Service{Name: portName(port), Port: port}
			e.Graph.Upsert(graph.Node{
				ID:               nodeID,
				IPs:              []string{host},
				OpenPorts:        []int{port},
				Services:         []graph.Service{svc},
				DiscoveryMethods: []string{"full-scan"},
				Detail: map[string]any{
					"fullScan": map[string]any{
						"status":   "running",
						"checked":  info.Checked,
						"total":    info.Total,
						"open":     info.OpenFound,
						"started":  info.Started,
						"lastOpen": port,
					},
				},
			})
			e.tryIdentify(host, port)
		},
		func(p tcpprobe.SweepProgress) {
			book := e.book()
			book.mu.Lock()
			info.Checked = p.Checked
			info.OpenFound = p.Open
			book.mu.Unlock()
			e.Graph.Upsert(graph.Node{
				ID: nodeID,
				Detail: map[string]any{
					"fullScan": map[string]any{
						"status":  "running",
						"checked": p.Checked,
						"total":   p.Total,
						"open":    p.Open,
						"started": info.Started,
					},
				},
			})
		},
	)
	if err == context.Canceled {
		info.Status = "cancelled"
		return
	}
	if err != nil {
		info.Status = "error"
		info.Error = err.Error()
		return
	}
	info.Status = "done"
}
