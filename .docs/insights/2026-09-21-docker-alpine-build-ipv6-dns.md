# Insight: Docker alpine build with IPv6-only DNS in container

**Date**: 2026-09-21
**Context**: Step 1.17 Dockerfile build hung for 8+ minutes on `go mod download`. The container's DNS resolved `proxy.golang.org` to an IPv6 address (`2001:4860::8888`) that wasn't reachable from the build environment.

**Insight**: Some Docker build environments (especially inside WSL2 or behind certain VPNs) have IPv6 DNS but no IPv6 routing. Go's net resolver tries AAAA records first, fails, and doesn't always fall back to IPv4.

**Solution pattern** (for `golang:1.27-alpine`):
```dockerfile
RUN apk add --no-cache git ca-certificates && update-ca-certificates
ENV GOFLAGS="-mod=mod"
ENV GOPROXY=https://proxy.golang.org,direct
ENV GOSUMDB=sum.golang.org
```

Key points:
- `git` is needed because `GOPROXY=...,direct` falls back to `git` for non-modulestore URLs (e.g. `dario.cat`)
- `ca-certificates` for HTTPS to proxy.golang.org
- `proxy.golang.org,direct` (not just `direct`) — proxy first, git fallback
- Don't use `GODEBUG=netdns=go-only+1` (invalid syntax in Go 1.27; runtime logs warning and ignores)

**Workaround outside Dockerfile** (for ad-hoc builds):
```bash
docker build --network=host -t webhook-relay-api .
```

**Skill update candidate**: `agentic-checkpoints` (build troubleshooting section) or new `docker-build` skill (overkill for one insight).

**Where fixed in code**: `Dockerfile` (commit dfb7269).
