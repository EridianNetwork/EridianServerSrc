# Lua Scripting Interface

Reference documentation for the server's Lua scripting interface. It describes the execution model, the available events, the functions exposed to scripts, and recommended usage practices.

Implementation: `internal/photon/scripting.go`. Interpreter: [gopher-lua](https://github.com/yuin/gopher-lua) (Lua 5.1).

## 1. Overview

The scripting system allows level content and logic to be added without recompiling the server. A script can, among other things:

- Instantiate NPCs and decorative objects when the map loads.
- React to events such as a player entering the room or an NPC dying.
- Schedule deferred or periodic actions.
- Display on-screen messages to the players in the room.

Each room has its own Lua state, isolated from all others. A script never shares global variables with another room, even if both run the same file.

## 2. Script Location and Naming

Scripts are stored in:

```
scripts/maps/<level_name>.lua
```

`<level_name>` is the scene name the server resolves for the room, that is, the `m_scene` value of the corresponding playlist in `data/playlists/Playlist.xml`. Examples:

| Scene | File |
|---|---|
| `lvl_Floasm_Small` | `scripts/maps/lvl_floasm_small.lua` |
| `lvl_RV_4x` | `scripts/maps/lvl_rv_4x.lua` |
| `lvl_Arid_Mine_Part2` | `scripts/maps/lvl_arid_mine_part2.lua` |

Notes:

- Multi-part maps use a separate script for each part (`_Part1`, `_Part2`, ...), because each part is a distinct scene.
- File lookup is case-sensitive on case-sensitive filesystems (Linux). It is not on Windows. Lowercase names are recommended, as in the existing scripts.
- If the file does not exist, the level simply has no script. No message is logged and no state is created.

## 3. Lifecycle

1. The script is loaded exactly once per room, when the first player reaches the "map-ready" state. Loading earlier would be pointless, because the client discards prefab instantiations sent before that point.
2. The whole file is executed (file-scope code). This is the place to define functions and variables; instantiating entities at this point is not recommended.
3. The `OnMapLoad(level)` event is invoked. This is the appropriate place to create initial content.
4. From then on, the server invokes events and timers on every tick.
5. When the room is closed, the Lua state is discarded along with its timers.

All entry points (events, timers and `OnTick`) run on the server's main dispatch thread. Script code therefore needs no synchronization, but it must be short: a slow operation or a long loop blocks the entire server.

## 4. Error Handling

Every call into script functions is protected. A runtime error is written to the server console in the format:

```
[SCRIPT] scripts/maps/<file>.lua: <Event>: <error message>
```

It is not propagated to the server and does not interrupt the match. A syntax or runtime error during the initial file load (`load error`) cancels the script for that room: no later event is invoked.

## 5. Events

An event is a global function with a specific name. Define only the ones you need; undefined events are ignored.

### `OnMapLoad(level)`

Invoked once, immediately after the script is loaded.

| Parameter | Type | Description |
|---|---|---|
| `level` | string | Scene name, for example `"lvl_Floasm_Small"`. |

### `OnTick(dt)`

Invoked on every server tick (approximately every 66 ms).

| Parameter | Type | Description |
|---|---|---|
| `dt` | number | Time elapsed since the previous tick, in seconds. |

It must stay lightweight, since it runs at high frequency. For actions spaced out in time, `After` is preferable.

### `OnPlayerSpawn(actor, name)`

Invoked when a player's avatar appears in the room.

| Parameter | Type | Description |
|---|---|---|
| `actor` | number | The player's actor number in the room. |
| `name` | string | Character name. |

### `OnPlayerLeave(actor)`

Invoked when a player leaves the room.

| Parameter | Type | Description |
|---|---|---|
| `actor` | number | Actor number of the departing player. |

### `OnNpcDeath(view, kit, killer)`

Invoked when an NPC dies, regardless of cause.

| Parameter | Type | Description |
|---|---|---|
| `view` | number | The NPC's view identifier (`viewBase`), the same value returned by `SpawnNpc`. |
| `kit` | string | NPC kit name, for example `"SkagPup"`. |
| `killer` | number | Actor number of the player who killed it, or `0` if it was not a player (script kill, environment or another NPC). |

## 6. Function Reference

The following functions are registered as globals in every Lua state.

### Utilities

#### `Log(text)`

Writes a line to the server console, prefixed with `[SCRIPT]` and the room name.

#### `GetLevel()` -> string

Returns the room's scene name.

#### `SendMessage(text [, seconds])`

Displays an on-screen notification to all players in the room.

| Parameter | Type | Default | Description |
|---|---|---|---|
| `text` | string | (required) | Text to display. |
| `seconds` | number | `5` | On-screen duration. |

#### `After(seconds, fn)`

Runs `fn` once after `seconds` seconds have elapsed. The effective resolution is the server tick (approximately 66 ms). It may be called at file scope, inside an event, or inside another deferred function. A deferred function can re-register itself to form a loop.

### Entity Creation

#### `SpawnNpc(kit, x, y, z)` -> number

Creates an NPC from a building-kit name (`"SkagPup"`, `"BanditKiller"`, etc.). Returns the NPC's `viewBase`, which identifies the entity in the other functions.

If no configuration exists for the kit, the NPC is still created (the client decides whether it can instantiate it) and a warning is logged to the console, since default health and behavior values are used.

A kit only renders correctly if its assets are preloaded by the current scene. A kit whose models belong only to another scene may appear broken or invisible.

#### `SpawnProp(prefab, x, y, z [, viewCount [, pitch [, yaw [, roll]]]])` -> number

Creates a simple, self-contained entity from a `Resources.Load` path (for example `"Entities/Object/Item/Claptrap"`). It is not valid for kit names; use `SpawnNpc` for those.

| Parameter | Type | Default | Description |
|---|---|---|---|
| `prefab` | string | (required) | Prefab path. |
| `x`, `y`, `z` | number | (required) | World position. `y` is the vertical axis. |
| `viewCount` | number | `1` | Number of network components (PhotonView) on the prefab. |
| `pitch`, `yaw`, `roll` | number | `0` | Rotation in degrees. |

Warning: `viewCount` must match exactly the number of PhotonView components the prefab actually has. If it does not, the client rejects the entire instantiation and the entity does not appear. A dynamic prefab (with physics) may also fall or tip over after spawning; place it close to the ground.

### NPC Control

#### `KillNpc(view)` -> boolean

Kills the given NPC as a server-originated death: no attacker and no experience awarded. Returns `true` if the NPC existed and was alive, `false` otherwise. The death fires `OnNpcDeath` with `killer = 0`.

#### `MoveNpc(view, x, y, z [, run])` -> boolean

Orders the NPC to move to the given destination. If the level has a navigation mesh (navmesh), the route is computed over it; otherwise a straight line is used. If `run` is `true`, the NPC runs instead of walking.

Returns `true` if the NPC exists and is alive. If the NPC enters combat or has an assigned patrol, the AI takes control back once it has a task of its own.

### State Queries

#### `GetPlayers()` -> table

Returns an array of the players that have an avatar in the room. Each element is a table:

| Field | Type | Description |
|---|---|---|
| `actor` | number | Actor number. |
| `name` | string | Character name. |
| `x`, `y`, `z` | number | Last known position. |

#### `GetNpcs()` -> table

Returns an array of the live NPCs in the room. Each element is a table:

| Field | Type | Description |
|---|---|---|
| `view` | number | View identifier. |
| `kit` | string | Kit name. |
| `hp` | number | Current health. |
| `x`, `y`, `z` | number | Current position. |

## 7. Examples

### 7.1 Initial Content

```lua
function OnMapLoad(level)
    SpawnProp("Entities/Object/Item/Claptrap", -813.95, -2.57, 701.021, 1, 180, 0, 0)
    Log("decorative prop created in " .. level)
end
```

### 7.2 Greeting Each Player

```lua
function OnPlayerSpawn(actor, name)
    SendMessage("Welcome, " .. name, 4)
end
```

### 7.3 Waves With a Recursive Timer

```lua
local waves = 0

local function spawnWave()
    waves = waves + 1
    for i = 1, 3 do
        SpawnNpc("SkagPup", 10 + i * 2, 5, 20)
    end
    SendMessage("Wave " .. waves, 3)
    if waves < 5 then
        After(30, spawnWave)
    end
end

function OnMapLoad(level)
    After(10, spawnWave)
end
```

### 7.4 Reacting to an NPC Death

```lua
local kills = 0

function OnNpcDeath(view, kit, killer)
    kills = kills + 1
    if killer ~= 0 and kills % 10 == 0 then
        SendMessage("Total kills: " .. kills, 4)
    end
end
```

### 7.5 Sending an NPC Toward the First Player

```lua
function OnMapLoad(level)
    local npc = SpawnNpc("BanditKiller", 0, 5, 0)
    After(5, function()
        local players = GetPlayers()
        if #players > 0 then
            local p = players[1]
            MoveNpc(npc, p.x, p.y, p.z, true)
        end
    end)
end
```

## 8. Administration and Reload

The `reload` command, typed into the server console, discards the Lua state of every active room (including its timers) and re-runs each script from scratch. `OnMapLoad` is invoked again.

Consequences to keep in mind:

- Script variables are reset.
- Entities created by the previous run are not removed. If the script creates them again in `OnMapLoad`, they will appear duplicated until the room is restarted.

To test multi-part maps, it can be combined with `debugscene <playlistId> <sceneIdx>`, which forces the next room of that playlist to start at the given part.

## 9. Best Practices and Limitations

- Keep events and callbacks short; all code shares the server's main thread.
- Avoid `OnTick` for logic that does not need to run on every tick; use `After`.
- Store the value returned by `SpawnNpc` and `SpawnProp` if the entity needs to be referenced later.
- Check the return value of `KillNpc` and `MoveNpc`; an NPC may have died or disappeared between calls.
- Use `Log` for debugging. Messages appear in the console with the `[SCRIPT]` prefix.
- The Lua state is initialized with the full standard libraries (including `os` and `io`). Only trusted scripts should be run.
- The current API does not allow modifying items, experience or player state, nor registering handlers for events other than those listed in section 5.
- An error during the initial load disables the script for that room; check the console after a `reload`.
