(function () {
  const nodesDS = new vis.DataSet([]);
  const edgesDS = new vis.DataSet([]);
  const container = document.getElementById("net");
  const network = new vis.Network(
    container,
    { nodes: nodesDS, edges: edgesDS },
    {
      physics: {
        solver: "forceAtlas2Based",
        forceAtlas2Based: { gravitationalConstant: -45, springLength: 110, springConstant: 0.08 },
        stabilization: { iterations: 80 },
      },
      interaction: { hover: true, tooltipDelay: 120 },
      nodes: { font: { color: "#e7ecf1", face: "IBM Plex Sans" }, borderWidth: 1 },
      edges: { color: { color: "#3a4a5c", highlight: "#3d9bfd" }, smooth: { type: "continuous" } },
    }
  );

  const nodeCache = {};

  function shapeFor(kind) {
    switch (kind) {
      case "router": return "diamond";
      case "subnet": return "box";
      case "network": return "ellipse";
      case "bridge": return "star";
      case "walkie": return "dot";
      case "remoteHint": return "dot";
      default: return "dot";
    }
  }

  function colorFor(kind) {
    switch (kind) {
      case "router": return { background: "#c9842b", border: "#f0b35a" };
      case "subnet": return { background: "#1e3a5f", border: "#3d9bfd" };
      case "network": return { background: "#243041", border: "#5a6f88" };
      case "bridge": return { background: "#5b3d8a", border: "#b89cff" };
      case "walkie": return { background: "#1f6f54", border: "#3dd68c" };
      case "remoteHint": return { background: "#3a3f48", border: "#8b9aab" };
      default: return { background: "#374151", border: "#9ca3af" };
    }
  }

  function applyGraph(g) {
    const st = g.status || {};
    document.getElementById("status").textContent =
      "MeshBridge " + (st.meshBridgeOk ? "OK" : "down") +
      " · Base " + (st.baseOk ? "OK" : "down") +
      " · ICMP " + (st.icmpEnabled ? "on" : "off") +
      (st.cidrs && st.cidrs.length ? " · " + st.cidrs.join(", ") : "") +
      (st.lastScan ? " · scan " + st.lastScan : "") +
      (st.message ? " · " + st.message : "");

    const nodeIds = {};
    (g.nodes || []).forEach(function (n) {
      nodeCache[n.id] = n;
      nodeIds[n.id] = true;
      nodesDS.update({
        id: n.id,
        label: n.label || n.id,
        shape: shapeFor(n.kind),
        color: colorFor(n.kind),
        title: (n.kind || "") + (n.meshId ? "\n" + n.meshId : ""),
        opacity: n.kind === "remoteHint" ? 0.65 : 1,
      });
    });
    nodesDS.getIds().forEach(function (id) {
      if (!nodeIds[id]) nodesDS.remove(id);
    });

    const edgeIds = {};
    (g.edges || []).forEach(function (e) {
      edgeIds[e.id] = true;
      edgesDS.update({
        id: e.id,
        from: e.from,
        to: e.to,
        dashes: !!e.dashed,
        label: e.kind === "gateway" ? "gw" : "",
        font: { size: 9, color: "#8b9aab" },
      });
    });
    edgesDS.getIds().forEach(function (id) {
      if (!edgeIds[id]) edgesDS.remove(id);
    });
  }

  function row(dl, k, v) {
    if (v == null || v === "" || (Array.isArray(v) && !v.length)) return;
    const dt = document.createElement("dt");
    dt.textContent = k;
    const dd = document.createElement("dd");
    if (Array.isArray(v)) {
      v.forEach(function (item) {
        const pill = document.createElement("span");
        pill.className = "pill";
        pill.textContent = typeof item === "object" ? JSON.stringify(item) : String(item);
        dd.appendChild(pill);
      });
    } else if (typeof v === "object") {
      dd.textContent = JSON.stringify(v, null, 2);
    } else {
      dd.textContent = String(v);
    }
    dl.appendChild(dt);
    dl.appendChild(dd);
  }

  function showModal(n) {
    document.getElementById("m-kind").textContent = n.kind || "node";
    document.getElementById("m-title").textContent = n.nickname || n.label || n.id;
    const dl = document.getElementById("m-body");
    dl.innerHTML = "";
    row(dl, "Mesh ID", n.meshId);
    row(dl, "Label", n.label);
    row(dl, "IPs", n.ips);
    row(dl, "MACs", n.macs);
    row(dl, "Platform", n.platform);
    row(dl, "App version", n.appVersion);
    row(dl, "Open ports", n.openPorts);
    row(dl, "Services", n.services);
    row(dl, "URLs", n.urls);
    row(dl, "GPS", n.gps);
    row(dl, "Subnet", n.subnet);
    row(dl, "Remote Base", n.remoteBaseName || n.remoteBaseId);
    row(dl, "Discovery", n.discoveryMethods);
    row(dl, "Last seen", n.lastSeen);
    row(dl, "Detail", n.detail);
    document.getElementById("backdrop").classList.add("open");
  }

  document.getElementById("m-close").onclick = function () {
    document.getElementById("backdrop").classList.remove("open");
  };
  document.getElementById("backdrop").onclick = function (ev) {
    if (ev.target.id === "backdrop") document.getElementById("backdrop").classList.remove("open");
  };

  network.on("click", function (params) {
    if (!params.nodes.length) return;
    const n = nodeCache[params.nodes[0]];
    if (n) showModal(n);
  });

  fetch("/api/graph").then(function (r) { return r.json(); }).then(applyGraph).catch(function (e) {
    document.getElementById("status").textContent = "graph error: " + e;
  });

  const es = new EventSource("/api/events");
  es.addEventListener("graph", function (ev) {
    try { applyGraph(JSON.parse(ev.data)); } catch (e) {}
  });
})();
