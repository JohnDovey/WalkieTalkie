# MeshSniff

LAN / dual-network discovery map for WalkieTalkie. Version **0.1.0**.

## Run

```sh
source tools/go-env.sh
go run ./meshsniff/cmd/meshsniff
# UI: http://127.0.0.1:9096
```

Default config contacts MeshBridge at `http://127.0.0.1:9095` and the Base Station at `http://127.0.0.1:9091`.

## Build

```sh
./tools/build-macos-meshsniff.sh
./tools/build-windows-meshsniff.sh
./tools/build-linux-meshsniff.sh   # on Linux
```

## What it shows

Networks, subnets, routers, hosts, WalkieTalkie devices (meshId + MAC when available), MeshBridge remotes as dashed remoteHints until L3 discovery upgrades them. Click a node for a modal of all known fields.

See [`docs/2026-07-14-meshsniff-plan.md`](../docs/2026-07-14-meshsniff-plan.md) and Manual → MeshSniff.
