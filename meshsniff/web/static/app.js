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

  function humanizeKey(k) {
    var known = {
      wifiIface: "Wi‑Fi interface",
      phyMode: "PHY mode",
      rateMbps: "Link rate (Mbps)",
      signalNoise: "Signal / noise",
      sameMachineNote: "Note",
      identifyURL: "Identify URL",
      seededFrom: "Seeded from",
      baseURL: "Base URL",
      iface: "Interface",
      role: "Role",
      gateway: "Gateway",
      country: "Country",
      note: "Note",
      lat: "Latitude",
      lon: "Longitude",
      accuracy: "Accuracy (m)",
      timestamp: "At",
      at: "At",
    };
    if (known[k]) return known[k];
    return String(k)
      .replace(/([a-z])([A-Z])/g, "$1 $2")
      .replace(/[_-]+/g, " ")
      .replace(/\b\w/g, function (c) { return c.toUpperCase(); });
  }

  function isPlainObject(v) {
    return v && typeof v === "object" && !Array.isArray(v) && !(v instanceof Date);
  }

  function looksLikeURL(s) {
    return typeof s === "string" && /^https?:\/\//i.test(s);
  }

  function formatScalar(v) {
    if (v == null) return "";
    if (typeof v === "boolean") return v ? "yes" : "no";
    if (typeof v === "number") return String(v);
    if (typeof v === "string") {
      // ISO timestamps → locale string when parseable
      if (/^\d{4}-\d{2}-\d{2}T/.test(v)) {
        var d = new Date(v);
        if (!isNaN(d.getTime())) return d.toLocaleString();
      }
      return v;
    }
    return String(v);
  }

  function fillValue(dd, v) {
    if (Array.isArray(v)) {
      if (!v.length) return false;
      v.forEach(function (item) {
        var pill = document.createElement("span");
        pill.className = "pill";
        if (typeof item === "object" && item !== null) {
          pill.textContent = (item.name || "") + (item.port ? " :" + item.port : "") + (item.url ? " " + item.url : "");
          if (!item.name && !item.port) {
            pill.textContent = Object.keys(item).map(function (k) {
              return humanizeKey(k) + ": " + formatScalar(item[k]);
            }).join(" · ");
          }
        } else {
          pill.textContent = formatScalar(item);
        }
        dd.appendChild(pill);
      });
      return true;
    }
    if (isPlainObject(v)) {
      var nested = document.createElement("dl");
      nested.className = "nested";
      var any = false;
      Object.keys(v).forEach(function (k) {
        if (row(nested, humanizeKey(k), v[k])) any = true;
      });
      if (!any) return false;
      dd.appendChild(nested);
      return true;
    }
    var s = formatScalar(v);
    if (s === "") return false;
    if (looksLikeURL(s)) {
      var a = document.createElement("a");
      a.href = s;
      a.target = "_blank";
      a.rel = "noopener noreferrer";
      a.textContent = s;
      dd.appendChild(a);
    } else {
      dd.textContent = s;
    }
    return true;
  }

  function row(dl, k, v) {
    if (v == null || v === "") return false;
    if (Array.isArray(v) && !v.length) return false;
    if (isPlainObject(v) && !Object.keys(v).length) return false;
    var dt = document.createElement("dt");
    dt.textContent = k;
    var dd = document.createElement("dd");
    if (!fillValue(dd, v)) return false;
    dl.appendChild(dt);
    dl.appendChild(dd);
    return true;
  }

  /** Promote flat detail keys into top-level labeled rows (no raw JSON blob). */
  function rowsFromDetail(dl, detail) {
    if (!isPlainObject(detail)) return;
    Object.keys(detail).sort().forEach(function (k) {
      row(dl, humanizeKey(k), detail[k]);
    });
  }

  function showModal(n) {
    document.getElementById("m-kind").textContent = n.kind || "node";
    document.getElementById("m-title").textContent = n.ssid || n.hostname || n.nickname || n.label || n.id;
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
    if (n.gps) {
      row(dl, "Latitude", n.gps.lat);
      row(dl, "Longitude", n.gps.lon);
      row(dl, "Accuracy (m)", n.gps.accuracy);
      row(dl, "GPS at", n.gps.timestamp || n.gps.at);
    }
    row(dl, "Subnet", n.subnet);
    row(dl, "Remote Base", n.remoteBaseName || n.remoteBaseId);
    row(dl, "Discovery", n.discoveryMethods);
    row(dl, "Last seen", n.lastSeen);
    rowsFromDetail(dl, n.detail);
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
