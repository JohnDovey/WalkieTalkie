# WalkieTalkie — AI assistant notes

## Go Dependabot / module hygiene (standing rule)

**Incident (2026-07):** GitHub Dependabot showed ~22 alerts on `main` even though workspace builds were fine.

**Root cause:** Dependabot scans each `go.mod`/`go.sum` independently. `go.work` had pulled patched `x/crypto` / `x/net` via `core/`, but **`server/go.mod` still pinned** vulnerable older versions (`crypto v0.33.0`, `net v0.35.0`). Workspace resolution does **not** clear Dependabot.

**Required fix pattern (any Go mono/multi-module repo):**

1. For **every** `go.mod`: `GOWORK=off go list -m golang.org/x/crypto golang.org/x/net` (and other flagged packages).
2. Bump vulnerable pins (`crypto` ≥ 0.54, `net` ≥ 0.57 as of the 2026-07 fix), `go mod tidy` per module, `go work sync`.
3. Keep Go on the current point release of the active series (e.g. **1.26.5**) — stdlib CVEs need toolchain bumps; use `govulncheck`.
4. Prefer `replace sibling => ../sibling` so `GOWORK=off` builds stay local.
5. Ignore only true noise (e.g. `GO-2026-5932` openpgp unmaintained if unused).
6. Patch-bump the affected app `VERSION`, commit, push; wait for Dependabot rescan.

Cursor rule: `.cursor/rules/dependabot-go-modules.mdc`. Fix commit reference: `66855b5`.

## Related project notes

See `.cursor/rules/` for volume storage, versioning, Android/dev environment, tools scripts, docs, and Manual conventions.
