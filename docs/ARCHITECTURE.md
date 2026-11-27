# Kinora architecture

Kinora is one Go binary (`cmd/kinora`) with an embedded web client, an SQLite database and FFmpeg/ffprobe as
external tools. This page maps the packages and the main data flows. It describes the code as of v1.0.30.

## Process layout

`cmd/kinora/main.go` wires everything and starts these long-running goroutines:

| Goroutine | Package | What it does |
|---|---|---|
| HTTP server | `internal/server` | REST API under `/api/`, web client under `/`, WebSocket for watch parties |
| `sc.Run` | `internal/scanner` | Periodic and triggered library scans |
| `mgr.Reap` | `internal/media` | Stops idle HLS transcode sessions |
| `dl.Run` | `internal/media` | Offline-download worker (one job at a time) and hourly pruning |
| `analyzer.Run` | `internal/intro` | Intro/credits detection from Chromaprint fingerprints, paused during playback |
| `hub.Run` | `internal/party` | Watch-party rooms |
| `srv.Acquire.Run` | `internal/acquire` | Follows downloads in the download clients and imports finished ones |
| `srv.Push.Run` | `internal/push` | iOS push notifications through the relay (`docs/PUSH_RELAY.md`) |
| `srv.RunDiscovery` | `internal/server` | UDP LAN discovery (default port 7359, `--discovery-port 0` turns it off) |
| `srv.Background` | `internal/server` | Hourly session and Quick Connect cleanup |

Configuration comes from flags and `KINORA_*` environment variables. Some admin settings can be pinned by
environment variables (`server.SettingEnv`); a pinned setting is shown as locked in the admin UI.

## Packages

| Package | Responsibility |
|---|---|
| `internal/store` | SQLite persistence (WAL, `_txlock=immediate`, foreign keys on). Append-only migration list in `db.go`. Items, libraries, users, profiles, sessions/devices, user data (watched, position, favourites), collections and smart rules, acquisition tables, push registrations. **Visibility** (`visibility.go`): `RatingSQL`/`LibrarySQL` build the WHERE fragment that hides items above a profile's rating limit or outside the account's allowed libraries. |
| `internal/scanner` | Walks library folders, parses file names (`parse.go`), reads NFO files (`nfo.go`) and local artwork (`images.go`), probes new/changed files with ffprobe through a worker pool (max. 6), detects moved files, groups episodes into series/seasons. Refuses to mark anything missing if the library root is unreachable or suddenly empty. |
| `internal/meta` | Optional online metadata: TMDB (match, details, images, discover), OMDb scores, OpenSubtitles search/download, API-key checks. |
| `internal/media` | ffprobe wrapper (`probe.go`), playback decision (`decision.go`: direct play / remux / transcode, with human-readable reasons), hardware encoder self-test (`encoders.go`), HLS sessions (`transcode.go`), WebVTT conversion (`subtitles.go`), server-side offline downloads (`download.go`). |
| `internal/intro` | Chromaprint-based intro/credits detection per season. |
| `internal/party` | Watch parties over WebSocket with a server-owned clock. |
| `internal/push` | Encrypted iOS notifications via the push relay. |
| `internal/acquire` | Built-in acquisition: indexers (Torznab `torznab.go`, Cardigann YAML definitions `cardigann/`, Go-written sites `sites/`), release parsing and scoring (`release.go`, `quality.go`), download clients (qBittorrent, Deluge, Transmission, SABnzbd/NZBGet in `usenet.go`), requests, series monitoring (`series.go`), quality upgrades (`upgrade.go`), import into a library (`importer.go`) and removal (`remove.go`). Off by default (`acq_enabled`). |
| `internal/server` | HTTP handlers, auth middleware, CORS, stream tokens, Quick Connect, invites, admin API, self-update, branding, app languages. |
| `web` | `web/static`: the vanilla JS single-page client (no build step), embedded with `go:embed`. `app.legacy.js` is generated on the fly with esbuild for older TV browsers (`internal/server/legacyjs.go`). |
| `apps/` | Clients: `launcher` + Capacitor/TV wrappers (Android, Tizen, webOS, Titan OS) that open the server's web client in app mode; `shared` is the older TypeScript UI kept for 2018 TVs; `ios` is a native SwiftUI app (iPhone, iPad, Apple TV) with the `KinoraKit` Swift package. |

## Request pipeline and permissions

`Server.Handler()` = `securityHeaders → logRequests → cors → mux`. Every route is wrapped by one of:

- `public` – resolves the session if there is one, never requires it;
- `authed` – a valid session (Bearer token, or the `kinora_session` cookie for same-origin web requests);
- `withProfile` – `authed` plus a selected profile (HTTP 428 otherwise);
- `admin` – `authed`, admin user, and not a profile-locked (kids Quick Connect) device;
- `media(needProfile)` – like `authed`/`withProfile`, but also accepts a `?st=` stream token (HMAC, scoped to one
  item and device) because `<video>`/`<img>` cannot send headers;
- `notKids` – additionally rejects kids profiles on write endpoints.

Item access goes through store functions that take the `*Profile` and append `RatingSQL`, or through
`ItemVisible` for handlers that only have an id. Session tokens are stored hashed; passwords use
PBKDF2-SHA256 (210 000 iterations).

## Data flows

### Library: scan → store → API → playback

```
library folder ─▶ scanner.walk ─▶ stat() unchanged? ─yes─▶ skip
                                   │no
                                   ▼
                    parse name / NFO / artwork ─▶ ffprobe (worker pool)
                                   ▼
                    store.scanwrite (one short tx per file; move detection)
                                   ▼
                    OnScanned ─▶ meta.Refresh (TMDB, optional) ─▶ intro.Analyzer.Kick
```

```
client ─▶ GET /api/home, /api/items, /api/search   (store queries + RatingSQL)
client ─▶ POST /api/items/{id}/playback {capabilities}
            └─ media.Decide ─▶ direct play  ─▶ GET /api/stream/{id}  (http.ServeContent, ranges)
                           └─▶ remux/transcode ─▶ media.Manager.Start (ffmpeg → HLS in cache/transcode/<session>)
                                                 └─ GET /api/hls/{session}/{index.m3u8|segNNNNN.ts|m4s}
client ─▶ POST /api/items/{id}/progress …          (user_data; Reap keeps sessions with recent activity)
```

Offline download: `POST /api/items/{id}/download` → `media.Downloader` queue → faststart MP4 in
`cache/downloads` (kept 7 days) → `GET /api/downloads/{key}/file`. Quality `original` returns the direct
stream URL with `?dl=1`.

### Acquisition: request → acquire → import

```
user ─▶ POST /api/requests (TMDB id)  ─▶ admin approves (or auto-approve)
                                              ▼
            acquire.Manager: search indexers (Torznab / Cardigann / sites) ─▶ ParseRelease + score against profile
                                              ▼
            Grab ─▶ download client (qBittorrent / Deluge / Transmission / SABnzbd / NZBGet)
                                              ▼
            Poll (Run loop) ─▶ finished? ─▶ MapPath (client path → local path)
                                              ▼
            importer: hardlink, else copy into "<Title> (<Year>) {imdb-…}/" or "<Show>/Season NN/"
                                              ▼
            scanner.Trigger(library) ─▶ item appears ─▶ push notification to the requester
```

Series monitoring (`series.go`) keeps the wanted-episode list from TMDB and searches for missing episodes;
`upgrade.go` replaces a file with a better release while the profile allows it.

## Storage layout

```
<data>/kinora.db            SQLite (WAL); backup via VACUUM INTO (GET /api/admin/backup)
<data>/images/<id>/…         artwork per item
<cache>/transcode/<session>  HLS sessions (removed on stop/reap)
<cache>/downloads/           offline downloads (*.mp4 + *.json)
<cache>/subs/                converted WebVTT
<cache>/fingerprints/        Chromaprint data for intro detection
<cache>/trickplay/<item>/    timeline preview sheets (0.jpg…, manifest.json)
```

## Testing

- Go: `scripts/go.sh go vet ./...` and `scripts/go.sh go test -race ./...` run inside the dev container (no local Go needed).
- Container smoke test: `scripts/smoke.sh` (generates media with `scripts/mkmedia.sh`, builds the runtime image, exercises the API).
- Clients: `npm test` in `apps/` (shared unit tests); iOS: `swift test --package-path apps/ios/KinoraKit` on macOS.
- CI: `.github/workflows/ci.yml` (vet, race tests, smoke, multi-arch images, Windows release) and `apps.yml` (client builds).
