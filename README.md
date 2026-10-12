# Borderlands Online Server

A Go reimplementation of the backend servers for Borderlands Online: authentication, the online service (lobby/characters/inventory/chat/friends), and the Photon game server.

## Components

| Binary | Role | Default port |
|---|---|---|
| `cmd/auth-server` | Authentication (SDO), TLS | 443 (HTTP 80) |
| `cmd/online-server` | Online service: characters, inventory, lobby, chat, friends | 2001 / 5055 |
| `cmd/photon-server` | Game/match server (Photon), world/NPC simulation | 4530 (TCP), 5055 (UDP), 9090 (WS) |
| `cmd/log-server` | Log viewer (`viewer.html`) | - |
| `cmd/eridian-server` | Starts online + photon (+ auth) together in a single process | own flags |

## Structure

```
cmd/            binaries (main.go for each server)
internal/
  auth/         authentication logic
  db/           sqlite access, migrations
  domain/       domain models
  online/       online TCP server and its services (character, inventory, lobby, chat, friend)
  photon/       game server: level cluster/state, NPCs, navmesh, network handlers
pkg/config/     config/server.json loader
config/         server configuration
data/           static game data
```

## Requirements

- Go 1.25+
- SQLite (via `modernc.org/sqlite`, no CGO required)

## Build

```sh
go build ./cmd/...
```

## Run

Combined mode (recommended for development):

```sh
go run ./cmd/eridian-server -db borderlands.db
```

Main flags: `-host`, `-online-port`, `-photon-port`, `-auth-port`, `-max-conns`, `-db`, `-cert`, `-key`.

To get a double-clickable Windows executable, build it to the repo root (it needs to sit next to `data/`, `config/`, and `borderlands.db` since it resolves those paths relative to its own working directory, which is what double-clicking from Explorer sets):

```sh
go build -o eridian-server.exe ./cmd/eridian-server
```

Or run each server separately:

```sh
go run ./cmd/auth-server
go run ./cmd/online-server
go run ./cmd/photon-server
```

Base configuration lives in `config/server.json` (ports, database path, game version, max players per room, PvP).

## Database

SQLite at `borderlands.db`. Migrations run automatically on startup (`db.RunMigrations`, `db.MigrateCharacterAvatar`).

## Tests

```sh
go test ./...
```

## License

This program is free software: you can redistribute it and/or modify it under the terms of the GNU General Public License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any later version. This program is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for more details. You should have received a copy of the GNU General Public License along with this program. If not, see <https://www.gnu.org/licenses/>.
