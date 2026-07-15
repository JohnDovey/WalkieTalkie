package seed

import (
	"testing"

	"github.com/JohnDovey/WalkieTalkie/core/registry"
	"github.com/JohnDovey/WalkieTalkie/meshsniff/graph"
)

func TestLinkNetworkTransportCellular(t *testing.T) {
	g := graph.NewStore()
	phoneID := g.Upsert(graph.Node{
		ID: "dev:phone1", Kind: graph.KindWalkie, Label: "Lejana", MeshID: "phone1",
	})
	LinkNetworkTransport(g, phoneID, &registry.Device{
		ID: "phone1", NetworkType: "cellular", NetworkName: "Vodacom",
	})
	snap := g.Snapshot()
	var foundNet, foundEdge bool
	for _, n := range snap.Nodes {
		if n.ID == "network:cellular:vodacom" && n.Kind == graph.KindNetwork {
			foundNet = true
			if n.Label != "Vodacom" {
				t.Fatalf("label=%q", n.Label)
			}
		}
	}
	for _, e := range snap.Edges {
		if e.From == "network:cellular:vodacom" && e.To == phoneID && e.Kind == "cellular" && e.Dashed {
			foundEdge = true
		}
	}
	if !foundNet || !foundEdge {
		t.Fatalf("net=%v edge=%v nodes=%d edges=%d", foundNet, foundEdge, len(snap.Nodes), len(snap.Edges))
	}
}

func TestLinkNetworkTransportIgnoresWifi(t *testing.T) {
	g := graph.NewStore()
	id := g.Upsert(graph.Node{ID: "dev:p", Kind: graph.KindWalkie, Label: "P"})
	LinkNetworkTransport(g, id, &registry.Device{NetworkType: "wifi", NetworkName: "Home"})
	for _, n := range g.Nodes() {
		if n.Kind == graph.KindNetwork {
			t.Fatal("wifi should not create cellular cloud")
		}
	}
}

func TestSanitizeNetSlug(t *testing.T) {
	if got := sanitizeNetSlug("  Vodacom ZA "); got != "vodacom-za" {
		t.Fatalf("got %q", got)
	}
}
