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
        forceAtlas2Based: { gravitationalConstant: -55, springLength: 95, springConstant: 0.09 },
        stabilization: { iterations: 100 },
      },
      interaction: { hover: true, tooltipDelay: 120 },
      nodes: { font: { color: "#e7ecf1", face: "IBM Plex Sans", multi: true }, borderWidth: 1 },
      edges: {
        color: { color: "#3a4a5c", highlight: "#3d9bfd" },
        smooth: { type: "continuous" },
        arrows: { to: { enabled: false } },
      },
    }
  );

  const nodeCache = {};

  function shapeFor(kind) {
    switch (kind) {
      case "router": return "diamond";
      case "subnet": return "box";
      case "network": return "ellipse";
      case "computer": return "box";
      case "bridge": return "star";
      case "walkie": return "dot";
      case "host": return "box";
      case "remoteHint": return "dot";
      default: return "dot";
    }
  }

  function colorFor(kind) {
    switch (kind) {
      case "router": return { background: "#c9842b", border: "#f0b35a" };
      case "subnet": return { background: "#1e3a5f", border: "#3d9bfd" };
      case "network": return { background: "#243041", border: "#5a6f88" };
      case "computer": return { background: "#2a4a6e", border: "#6eb6ff" };
      case "host": return { background: "#2a4a6e", border: "#6eb6ff" };
      case "bridge": return { background: "#5b3d8a", border: "#b89cff" };
      case "walkie": return { background: "#1f6f54", border: "#3dd68c" };
      case "remoteHint": return { background: "#3a3f48", border: "#8b9aab" };
      default: return { background: "#374151", border: "#9ca3af" };
    }
  }

  function nodeLabel(n) {
    var base = n.ssid || n.hostname || n.nickname || n.label || n.id;
    var lines = [base];
    if (n.ips && n.ips.length && n.ips[0] !== base) lines.push(n.ips[0]);
    if (n.ssid && n.channel) {
      lines.push(n.channel + (n.security ? " · " + n.security : ""));
    } else if (n.ssid && n.security) {
      lines.push(n.security);
    }
    if (n.services && n.services.length) {
      var names = n.services.slice(0, 4).map(function (s) {
        return s.name + (s.port ? ":" + s.port : "");
      });
      lines.push(names.join(", "));
      if (n.services.length > 4) lines.push("+" + (n.services.length - 4) + " more");
    } else if (n.openPorts && n.openPorts.length) {
      lines.push("ports " + n.openPorts.slice(0, 6).join(","));
    }
    if (n.viaRouter) lines.push("via " + n.viaRouter);
    return lines.join("\n");
  }

  function applyGraph(g) {
    const st = g.status || {};
    document.getElementById("status").textContent =
      "WalkieTalkie " + (st.walkieTalkieOk ? ("OK" + (st.walkieBaseName ? " (" + st.walkieBaseName + ")" : "") +
        (st.walkieSeeded ? " · " + st.walkieSeeded + " seeded" : "")) : "down") +
      " · MeshBridge " + (st.meshBridgeOk ? "OK" : "down") +
      " · ICMP " + (st.icmpEnabled ? "on" : "off") +
      (st.cidrs && st.cidrs.length ? " · " + st.cidrs.join(", ") : "") +
      (st.lastScan ? " · scan " + st.lastScan : "") +
      (st.message ? " · " + st.message : "");

    const nodeIds = {};
    (g.nodes || []).forEach(function (n) {
      nodeCache[n.id] = n;
      nodeIds[n.id] = true;
      var size = n.kind === "router" ? 28 : (n.kind === "computer" || n.kind === "host" ? 22 : 16);
      nodesDS.update({
        id: n.id,
        label: nodeLabel(n),
        shape: shapeFor(n.kind),
        color: colorFor(n.kind),
        size: size,
        font: { size: n.kind === "computer" || n.kind === "router" ? 12 : 11, multi: true, color: "#e7ecf1" },
        title: (n.kind || "") + (n.ssid ? "\nSSID " + n.ssid : "") + (n.meshId ? "\nmesh " + n.meshId : "") +
          (n.viaRouter ? "\nconnected via router " + n.viaRouter : "") +
          (n.services && n.services.length ? "\n" + n.services.length + " service(s) on this host" : ""),
        opacity: n.kind === "remoteHint" ? 0.65 : 1,
      });
    });
    nodesDS.getIds().forEach(function (id) {
      if (!nodeIds[id]) nodesDS.remove(id);
    });

    const edgeIds = {};
    (g.edges || []).forEach(function (e) {
      edgeIds[e.id] = true;
      var label = "";
      var width = 1;
      var color = "#3a4a5c";
      if (e.kind === "via-router") {
        label = "LAN";
        width = 2;
        color = "#c9842b";
      } else if (e.kind === "gateway") {
        label = "gateway";
      } else if (e.kind === "walkietalkie") {
        label = "mesh";
        color = "#3dd68c";
      }
      edgesDS.update({
        id: e.id,
        from: e.from,
        to: e.to,
        dashes: !!e.dashed || e.kind === "remote",
        label: label,
        width: width,
        color: { color: color, highlight: "#3d9bfd" },
        font: { size: 9, color: "#8b9aab", strokeWidth: 0 },
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
        if (typeof item === "object" && item !== null) {
          pill.textContent = (item.name || "") + (item.port ? " :" + item.port : "") + (item.url ? " " + item.url : "");
          if (!item.name && !item.port) pill.textContent = JSON.stringify(item);
        } else {
          pill.textContent = String(item);
        }
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
    document.getElementById("m-title").textContent = n.hostname || n.nickname || n.label || n.id;
    const dl = document.getElementById("m-body");
    dl.innerHTML = "";
    row(dl, "Hostname", n.hostname);
    row(dl, "Mesh ID", n.meshId);
    row(dl, "Label", n.label);
    row(dl, "SSID", n.ssid);
    row(dl, "BSSID", n.bssid);
    row(dl, "Channel", n.channel);
    row(dl, "Security", n.security);
    row(dl, "IPs", n.ips);
    row(dl, "MACs", n.macs);
    row(dl, "Connected via router", n.viaRouter);
    row(dl, "Same host as", n.sameHostAs);
    row(dl, "Platform", n.platform);
    row(dl, "App version", n.appVersion);
    row(dl, "Open ports", n.openPorts);
    row(dl, "Services on this machine", n.services);
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
