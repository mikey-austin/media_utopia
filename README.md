```
 .----------------.  .----------------. 
| .--------------. || .--------------. |
| | ____    ____ | || | _____  _____ | |
| ||_   \  /   _|| || ||_   _||_   _|| |
| |  |   \/   |  | || |  | |    | |  | |
| |  | |\  /| |  | || |  | '    ' |  | |
| | _| |_\/_| |_ | || |   \ `--' /   | |
| ||_____||_____|| || |    `.__.'    | |
| |              | || |              | |
| '--------------' || '--------------' |
 '----------------'  '----------------' 
```

# Media Utopia (`mu`)

[![CI](https://github.com/mikey-austin/media_utopia/actions/workflows/ci.yml/badge.svg)](https://github.com/mikey-austin/media_utopia/actions/workflows/ci.yml)

**無 — nothing in the way.**
Media Utopia is a homelab-first media control protocol and reference implementation designed to replace shaky legacy stacks (UPnP/DLNA control plane quirks, MPD client sync pain, proprietary casting silos) with a small, observable core:

- **Control plane:** MQTT (commands, state, events)
- **Data plane:** HTTP (media streaming, artwork)
- **Primary UI:** Home Assistant (first-class, reference UI)
- **Reference CLI:** `mu` (think `mpc`, but lease-aware and deterministic)
- **Bridges over rewrites:** integrate with UPnP, Jellyfin, Kodi via adapters

The goal is a system that is *boringly reliable*: explicit state, explicit ownership, minimal surface area, no hidden heuristics.

---

## Status

This repository contains:
- Protocol specification drafts (`docs/spec/`)
- Design rationale and decisions (`docs/design/`)
- (Planned) reference implementations:
  - playlist server
  - renderer bridges (UPnP, Kodi)
  - library bridges (UPnP, Jellyfin)
  - `mu` CLI
  - Home Assistant MQTT Discovery mapping

## Documentation

- Docs index: `docs/README.md`
- Spec overview: `docs/spec/overview.md`
- Message formats: `docs/spec/messages.md`
- Design motivation: `docs/design/motivation.md`
- Design decisions: `docs/design/decisions.md`
- Architecture overview: `docs/design/architecture.md`
- Integrations: `docs/design/integrations.md`
- Roadmap: `docs/design/roadmap.md`

## Development

Build:

```bash
go build ./cmd/mu ./cmd/mud
```

Test:

```bash
GOCACHE=/home/mikey/Workspace/media_utopia/.gocache go test ./...
```

Or use the Make targets:

```bash
make build
make test
make fmt
make integration
```

Integration tests (embedded MQTT + `mud` + `mu`) run via `make integration`.
Set `MU_INTEGRATION_DEBUG=1` for verbose integration logs.

## Contributing / Next Steps

If you’re implementing:
1) Bring up MQTT broker (Mosquitto) and define ACLs
2) Implement playlist server (required v1)
3) Implement UPnP renderer bridge (unlocks existing renderers)
4) Implement `mu` CLI command surface
5) Add HA MQTT Discovery mapping

## License

TBD
