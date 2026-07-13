// Shared helpers for the device list, used by both index.html (current
// devices) and old-nodes.html (devices not seen in OLD_NODE_THRESHOLD_MS).

// A device moves from the main page to the Old Nodes page once it hasn't
// been seen in this long — see docs/2026-07-13-implementation-plan.md.
const OLD_NODE_THRESHOLD_MS = 2 * 60 * 60 * 1000; // 2 hours

function isOldNode(d) {
  if (!d.lastSeen) return false;
  return (Date.now() - new Date(d.lastSeen).getTime()) > OLD_NODE_THRESHOLD_MS;
}

function sortByLastSeenDesc(devices) {
  return devices.slice().sort(function (a, b) {
    return new Date(b.lastSeen).getTime() - new Date(a.lastSeen).getTime();
  });
}

function statusBadge(status) {
  var cls = status === 'connected' ? 'bg-success' : 'bg-secondary';
  return '<span class="badge ' + cls + '">' + status + '</span>';
}

// Color for the device's name text itself, distinct from (but consistent
// with) the status badge color.
function nameColorClass(status) {
  return status === 'connected' ? 'text-success' : 'text-secondary';
}

function discoveryBadge(d) {
  var methods = (d.discoveryMethods || []).join(', ') || 'unknown';
  if (d.reportedBy && d.reportedBy.length) {
    return methods + ' &mdash; via ' + d.reportedBy.join(', ');
  }
  return methods;
}

function relativeTime(iso) {
  if (!iso) return '—';
  var seconds = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000));
  if (seconds < 60) return seconds + 's ago';
  if (seconds < 3600) return Math.round(seconds / 60) + 'm ago';
  return Math.round(seconds / 3600) + 'h ago';
}

function gpsText(d) {
  var p = d.currentLocation || d.lastKnownLocation;
  if (!p) return '—';
  var label = d.currentLocation ? '' : ' (last known)';
  return p.lat.toFixed(5) + ', ' + p.lon.toFixed(5) + label;
}

function deviceNameHTML(d) {
  var name = '<span class="' + nameColorClass(d.status) + '">' + d.name + '</span>';
  if (d.capabilities && d.capabilities.indexOf('presence-only') !== -1) {
    name += ' <span class="badge bg-warning text-dark">presence-only</span>';
  }
  return name;
}
