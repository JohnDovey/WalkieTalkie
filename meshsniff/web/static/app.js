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

  const PHONE_ICON = "/static/icons/phone.svg";
  const WATCH_ICON = "/static/icons/watch.svg";

  function isPhoneNode(n) {
    if (!n) return false;
    var p = (n.platform || "").toLowerCase();
    if (p === "android" || p === "ios" || p === "wear") return true;
    return n.kind === "walkie";
  }

  function isWearNode(n) {
    return n && (n.platform || "").toLowerCase() === "wear";
  }

  function shapeFor(n) {
    var kind = n && n.kind;
    if (isPhoneNode(n)) return "image";
    switch (kind) {
      case "router": return "diamond";
      case "subnet": return "dot";
      case "network": return "ellipse";
      case "computer": return "square";
      case "bridge": return "star";
      case "host": return "square";
      case "remoteHint": return "dot";
      default: return "dot";
    }
  }

  function colorFor(n) {
    var kind = n && n.kind;
    if (isPhoneNode(n)) return { background: "#1f6f54", border: "#3dd68c" };
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

  function shortName(n) {
    var s = n.ssid || n.hostname || n.nickname || n.label || n.id || "";
    s = String(s).replace(/\.local$/i, "");
    if (s.length > 26) s = s.slice(0, 24) + "…";
    return s;
  }

  /** Compact on-graph caption — details live in the hover tooltip / modal. */
  function nodeLabel(n) {
    var base = shortName(n);
    if (n.kind === "router" && n.ssid) return n.ssid;
    if (n.ips && n.ips.length && n.ips[0] !== base && n.kind !== "subnet") {
      return base + "\n" + n.ips[0];
    }
    return base;
  }

  function nodeTooltip(n) {
    var lines = [];
    if (n.kind) {
      if (isPhoneNode(n)) {
        lines.push(isWearNode(n) ? "phone (wear)" : "phone");
      } else {
        lines.push(n.kind);
      }
    }
    if (n.platform) lines.push("platform " + n.platform);
    if (n.ssid) lines.push("SSID " + n.ssid);
    if (n.hostname && n.hostname !== n.ssid) lines.push(n.hostname);
    if (n.ips && n.ips.length) lines.push(n.ips.join(", "));
    if (n.meshId) lines.push("mesh " + n.meshId);
    if (n.viaRouter) lines.push("via " + n.viaRouter);
    if (n.services && n.services.length) {
      lines.push("Services:");
      n.services.slice(0, 12).forEach(function (s) {
        lines.push("  " + (s.name || "service") + (s.port ? " :" + s.port : ""));
      });
      if (n.services.length > 12) lines.push("  +" + (n.services.length - 12) + " more");
    } else if (n.openPorts && n.openPorts.length) {
      lines.push("ports " + n.openPorts.join(", "));
    }
    lines.push("(click for full detail)");
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
      var size = n.kind === "router" ? 28 : (isPhoneNode(n) ? 26 : (n.kind === "computer" || n.kind === "host" ? 16 : 14));
      var shape = shapeFor(n);
      var nodeOpts = {
        id: n.id,
        label: nodeLabel(n),
        shape: shape,
        color: colorFor(n),
        size: size,
        font: {
          size: 11,
          multi: true,
          color: "#c9d4e0",
          strokeWidth: 3,
          strokeColor: "#0f1419",
          face: "IBM Plex Sans",
        },
        title: nodeTooltip(n),
        opacity: n.kind === "remoteHint" ? 0.65 : 1,
      };
      if (shape === "image") {
        nodeOpts.image = isWearNode(n) ? WATCH_ICON : PHONE_ICON;
        nodeOpts.shapeProperties = { useBorderWithImage: true };
      }
      nodesDS.update(nodeOpts);
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
        width = 2;
        color = "#c9842b";
      } else if (e.kind === "walkietalkie") {
        color = "#3dd68c";
      }
      edgesDS.update({
        id: e.id,
        from: e.from,
        to: e.to,
        dashes: !!e.dashed || e.kind === "remote",
        label: label,
        title: e.kind || "",
        width: width,
        color: { color: color, highlight: "#3d9bfd" },
        font: { size: 9, color: "#8b9aab", strokeWidth: 0 },
      });
    });
    edgesDS.getIds().forEach(function (id) {
      if (!edgeIds[id]) edgesDS.remove(id);
    });
    if (modalNodeId && document.getElementById("backdrop").classList.contains("open")) {
      var cur = nodeCache[modalNodeId];
      if (cur) showModal(cur);
    }
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
      sysop: "Sysop",
      fidoAddresses: "Fido addresses",
      networks: "Networks",
      software: "Software",
      bbsName: "BBS name",
      discoveryMethods: "VirtBBS probes",
      fullScan: "Full port scan",
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
          if (item.name && (item.address || item.binkpPort || item.role)) {
            pill.textContent = item.name +
              (item.address ? " " + item.address : "") +
              (item.role ? " (" + item.role + ")" : "") +
              (item.binkpPort ? " :" + item.binkpPort : "");
          } else {
            pill.textContent = (item.name || "") + (item.port ? " :" + item.port : "") + (item.url ? " " + item.url : "");
            if (!item.name && !item.port) {
              pill.textContent = Object.keys(item).map(function (k) {
                return humanizeKey(k) + ": " + formatScalar(item[k]);
              }).join(" · ");
            }
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
      if (k === "fullScan") return; // shown in scan status strip
      row(dl, humanizeKey(k), detail[k]);
    });
  }

  function serviceLabel(s) {
    var known = {
      http: "HTTP",
      https: "HTTPS",
      dns: "DNS",
      ssh: "SSH",
      smb: "SMB",
      rdp: "RDP",
      vnc: "VNC",
      afp: "AFP",
      ipp: "IPP",
      kerberos: "Kerberos",
      signaling: "Signaling",
      relay: "SFU relay",
      "virtbbs web": "VirtBBS Web",
      "virtbbs telnet": "VirtBBS Telnet",
      "virtbbs ssh": "VirtBBS SSH",
      "virtbbs api": "VirtBBS API",
      "virtbbs binkp": "VirtBBS BinkP",
      "virtbbs binkp lovly": "VirtBBS BinkP (LovlyNet)",
      "virtbbs binkp virtnet": "VirtBBS BinkP (VirtNet)",
    };
    var name = (s && s.name) ? String(s.name) : "";
    if (!name && s && s.port) return "Port " + s.port;
    var key = name.toLowerCase();
    if (known[key]) return known[key];
    return humanizeKey(name);
  }

  function serviceHref(s, ips) {
    if (s && s.url) return s.url;
    if (!s || !s.port || !ips || !ips.length) return "";
    var host = ips[0];
    var name = (s.name || "").toLowerCase();
    if (s.port === 80 || name === "http") return "http://" + host + "/";
    if (s.port === 443 || name === "https") return "https://" + host + "/";
    if (s.port === 8081 || name.indexOf("virtbbs web") >= 0) return "http://" + host + ":8081/";
    if (s.port === 8080 || s.port === 8443 || s.port === 9091 || s.port === 9095 || s.port === 9096) {
      var scheme = s.port === 8443 ? "https" : "http";
      return scheme + "://" + host + ":" + s.port + "/";
    }
    return "";
  }

  /** Expand services into labeled rows (name → port / link), not pills. */
  function rowsFromServices(dl, services, ips) {
    if (!services || !services.length) return;
    services.forEach(function (s) {
      if (!s) return;
      var dt = document.createElement("dt");
      dt.textContent = serviceLabel(s);
      var dd = document.createElement("dd");
      var parts = [];
      if (s.port) {
        var portSpan = document.createElement("span");
        portSpan.className = "svc-port";
        portSpan.textContent = "port " + s.port;
        dd.appendChild(portSpan);
        parts.push(1);
      }
      var href = serviceHref(s, ips);
      if (href) {
        if (parts.length) dd.appendChild(document.createTextNode(" · "));
        var a = document.createElement("a");
        a.href = href;
        a.target = "_blank";
        a.rel = "noopener noreferrer";
        a.textContent = href;
        dd.appendChild(a);
      }
      if (!parts.length && !href) {
        dd.textContent = formatScalar(s.name || "");
      }
      dl.appendChild(dt);
      dl.appendChild(dd);
    });
  }

  function portsNotCoveredByServices(ports, services) {
    if (!ports || !ports.length) return null;
    if (!services || !services.length) return ports;
    var covered = {};
    services.forEach(function (s) {
      if (s && s.port) covered[s.port] = true;
    });
    var left = ports.filter(function (p) { return !covered[p]; });
    return left.length ? left : null;
  }

  var modalNodeId = null;

  function scanableHost(n) {
    if (!n) return "";
    if (n.ips && n.ips.length) {
      for (var i = 0; i < n.ips.length; i++) {
        if (/^\d+\.\d+\.\d+\.\d+$/.test(n.ips[i]) && n.ips[i] !== "127.0.0.1") return n.ips[i];
      }
      for (var j = 0; j < n.ips.length; j++) {
        if (/^\d+\.\d+\.\d+\.\d+$/.test(n.ips[j])) return n.ips[j];
      }
    }
    if (n.id && n.id.indexOf("host:") === 0) {
      var h = n.id.slice(5);
      if (/^\d+\.\d+\.\d+\.\d+$/.test(h)) return h;
    }
    return "";
  }

  function formatFullScan(fs) {
    if (!fs || typeof fs !== "object") return "";
    var pct = fs.total ? Math.floor((100 * (fs.checked || 0)) / fs.total) : 0;
    var s = (fs.status || "?") + " · " + (fs.checked || 0) + "/" + (fs.total || 65535) +
      " (" + pct + "%) · " + (fs.open || 0) + " open";
    if (fs.lastOpen) s += " · last " + fs.lastOpen;
    if (fs.error) s += " · " + fs.error;
    return s;
  }

  function updateScanControls(n) {
    var host = scanableHost(n);
    var scanBtn = document.getElementById("m-scan");
    var cancelBtn = document.getElementById("m-scan-cancel");
    var statusEl = document.getElementById("m-scan-status");
    var fs = n && n.detail && n.detail.fullScan;
    if (!host) {
      scanBtn.hidden = true;
      cancelBtn.hidden = true;
      statusEl.textContent = "";
      return;
    }
    scanBtn.hidden = false;
    scanBtn.dataset.host = host;
    cancelBtn.dataset.host = host;
    var running = fs && fs.status === "running";
    scanBtn.disabled = !!running;
    cancelBtn.hidden = !running;
    statusEl.textContent = fs ? formatFullScan(fs) : "TCP ports 1–65535";
  }

  function showModal(n) {
    modalNodeId = n.id;
    document.getElementById("m-kind").textContent =
      isPhoneNode(n) ? (isWearNode(n) ? "phone · wear" : "phone") : (n.kind || "node");
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
    rowsFromServices(dl, n.services, n.ips);
    row(dl, "Other open ports", portsNotCoveredByServices(n.openPorts, n.services));
    if (isPlainObject(n.urls)) {
      Object.keys(n.urls).forEach(function (k) {
        row(dl, humanizeKey(k) + " URL", n.urls[k]);
      });
    }
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
    updateScanControls(n);
    document.getElementById("backdrop").classList.add("open");
  }

  document.getElementById("m-close").onclick = function () {
    document.getElementById("backdrop").classList.remove("open");
    modalNodeId = null;
  };
  document.getElementById("backdrop").onclick = function (ev) {
    if (ev.target.id === "backdrop") {
      document.getElementById("backdrop").classList.remove("open");
      modalNodeId = null;
    }
  };

  document.getElementById("m-scan").onclick = function () {
    var host = this.dataset.host;
    if (!host) return;
    var statusEl = document.getElementById("m-scan-status");
    statusEl.textContent = "Starting full scan of " + host + "…";
    this.disabled = true;
    fetch("/api/scan", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ host: host }),
    }).then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
      .then(function (res) {
        if (res.j && res.j.scan) {
          statusEl.textContent = formatFullScan({
            status: res.j.scan.status,
            checked: res.j.scan.checked,
            total: res.j.scan.total,
            open: res.j.scan.openFound,
          }) + (res.j.alreadyRunning ? " (already running)" : "");
        } else if (res.j && res.j.error) {
          statusEl.textContent = res.j.error;
        }
        document.getElementById("m-scan-cancel").hidden = false;
      })
      .catch(function (e) {
        statusEl.textContent = "scan error: " + e;
        document.getElementById("m-scan").disabled = false;
      });
  };

  document.getElementById("m-scan-cancel").onclick = function () {
    var host = this.dataset.host;
    if (!host) return;
    fetch("/api/scan?host=" + encodeURIComponent(host), { method: "DELETE" })
      .then(function (r) { return r.json(); })
      .then(function (j) {
        document.getElementById("m-scan-status").textContent =
          j.cancelled ? "Cancel requested…" : "No running scan";
      });
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
