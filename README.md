# local-mcp

A lightweight HTTP server for desktop-native capabilities — GitHub CLI integration,
desktop notifications with text-to-speech, and placeholder stubs for future
web search/fetch features. Written in Go with zero external dependencies.

## Requirements

- Go 1.22+
- Linux (for desktop notification and TTS commands)
- System commands:
  - [`gh`](https://cli.github.com/) — GitHub CLI (for PR/issue endpoints)
  - [`notify-send`](https://man.archlinux.org/man/notify-send.1) — Desktop notifications (for `/notify`)
  - [`festival`](http://www.cstr.ed.ac.uk/projects/festival/) — Text-to-speech (for `/notify`)

## Build

```bash
go build -o bin/local-mcp .
```

Produces a single static binary with no runtime dependencies.

## Usage

```bash
./local-mcp
# Server listening on port 54321
```

```bash
./local-mcp --port 9999
# Server listening on port 9999
```

Default port is `0` (random). Press Ctrl+C to stop gracefully.

## Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/gh/pull/{id}` | Fetch GitHub PR details, commits, and diff as markdown |
| GET | `/gh/issue/{id}` | Fetch GitHub issue details and comments as markdown |
| POST | `/notify` | Send desktop notification and TTS |
| GET | `/search?q=...` | Web search (stub — returns empty results) |
| POST | `/fetch` | Fetch web content (stub — returns empty results) |

The `/gh/*` endpoints accept an optional `repo` query parameter (`owner/name`).
When omitted, `gh` resolves the repository from its working directory.

```bash
# PR 42 of rickdgeerling/local-tools
curl 'localhost:54321/gh/pull/42?repo=rickdgeerling/local-tools'
```

## Architecture

Pi runs inside a Docker container. local-mcp runs directly on the host.
Communication happens via `host.docker.internal:<port>`.

The server binds to `0.0.0.0` (all interfaces) so Docker can reach it.
Pi's startup script launches local-mcp with a random port and captures
the port from its stdout output.

## Notes

- All error responses use `Content-Type: text/markdown`
- Success responses use `Content-Type: application/json` where appropriate
- External commands use context-based timeouts (30s for `gh`, 60s for `festival`)
- No authentication on any endpoint
