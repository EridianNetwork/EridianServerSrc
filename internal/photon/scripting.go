package photon
import (
	"log"
	"os"
	"path/filepath"
	"time"
	lua "github.com/yuin/gopher-lua"
)
const scriptsDir = "scripts/maps"
type scriptTimer struct {
	at time.Time
	fn *lua.LFunction
}
type roomScript struct {
	L      *lua.LState
	path   string
	timers []scriptTimer
}
func (s *EnetServer) scriptEnsureLoaded(room *Room) {
	if room.scriptLoaded {
		return
	}
	room.scriptLoaded = true
	s.scriptLoad(room)
}
func (s *EnetServer) scriptLoad(room *Room) {
	level := getCurScnFromProps(room.Name, room.Properties)
	path := filepath.Join(scriptsDir, level+".lua")
	if _, err := os.Stat(path); err != nil {
		return
	}
	L := lua.NewState()
	s.scriptRegisterAPI(L, room)
	if err := L.DoFile(path); err != nil {
		delete(pendingTimers, L)
		L.Close()
		log.Printf("[SCRIPT] %s: load error: %v", path, err)
		return
	}
	room.script = &roomScript{L: L, path: path, timers: pendingTimers[L]}
	delete(pendingTimers, L)
	log.Printf("[SCRIPT] loaded %s room=%s", path, room.Name)
	s.scriptFire(room, "OnMapLoad", lua.LString(level))
}
func (s *EnetServer) scriptReload(room *Room) {
	room.scriptClose()
	room.scriptLoaded = true
	s.scriptLoad(room)
}
func (r *Room) scriptClose() {
	if r.script != nil {
		r.script.L.Close()
		r.script = nil
	}
}
func (s *EnetServer) scriptFire(room *Room, event string, args ...lua.LValue) {
	sc := room.script
	if sc == nil {
		return
	}
	fn := sc.L.GetGlobal(event)
	if fn == lua.LNil {
		return
	}
	if err := sc.L.CallByParam(lua.P{Fn: fn, NRet: 0, Protect: true}, args...); err != nil {
		log.Printf("[SCRIPT] %s: %s: %v", sc.path, event, err)
	}
}
func (s *EnetServer) scriptTick(room *Room, dt float64) {
	sc := room.script
	if sc == nil {
		return
	}
	if len(sc.timers) > 0 {
		now := time.Now()
		var due []scriptTimer
		remaining := sc.timers[:0]
		for _, t := range sc.timers {
			if now.Before(t.at) {
				remaining = append(remaining, t)
			} else {
				due = append(due, t)
			}
		}
		sc.timers = remaining
		for _, t := range due {
			if err := sc.L.CallByParam(lua.P{Fn: t.fn, NRet: 0, Protect: true}); err != nil {
				log.Printf("[SCRIPT] %s: timer: %v", sc.path, err)
			}
		}
	}
	s.scriptFire(room, "OnTick", lua.LNumber(dt))
}
func (s *EnetServer) scriptActorForView(room *Room, view int32) int32 {
	if view == 0 {
		return 0
	}
	for nr, uid := range room.Players {
		if sess := s.sessionInRoom(uid, room.Name); sess != nil && sess.avatarBase == view {
			return nr
		}
	}
	return 0
}
func (s *EnetServer) scriptRegisterAPI(L *lua.LState, room *Room) {
	level := func() string { return getCurScnFromProps(room.Name, room.Properties) }
	reg := func(name string, fn lua.LGFunction) { L.SetGlobal(name, L.NewFunction(fn)) }
	reg("Log", func(L *lua.LState) int {
		log.Printf("[SCRIPT] %s: %s", room.Name, L.CheckString(1))
		return 0
	})
	reg("GetLevel", func(L *lua.LState) int {
		L.Push(lua.LString(level()))
		return 1
	})
	reg("SendMessage", func(L *lua.LState) int {
		text := L.CheckString(1)
		dur := float32(L.OptNumber(2, 5))
		s.hostSendUIMessage(room, text, dur)
		return 0
	})
	reg("SpawnNpc", func(L *lua.LState) int {
		kit := L.CheckString(1)
		pos := [3]float32{
			float32(L.CheckNumber(2)),
			float32(L.CheckNumber(3)),
			float32(L.CheckNumber(4)),
		}
		if npcConfigFor(kit) == nil {
			log.Printf("[SCRIPT] %s: SpawnNpc: no config for kit %q (spawning anyway)", room.Name, kit)
		}
		base := s.hostSpawnNpcAt(room, level(), kit, pos, nil, 0)
		s.hostSendSerialize(room) 
		L.Push(lua.LNumber(base))
		return 1
	})
	reg("SpawnProp", func(L *lua.LState) int {
		prefab := L.CheckString(1)
		pos := [3]float32{
			float32(L.CheckNumber(2)),
			float32(L.CheckNumber(3)),
			float32(L.CheckNumber(4)),
		}
		viewCount := int32(L.OptNumber(5, 1))
		pitch := float32(L.OptNumber(6, 0))
		yaw := float32(L.OptNumber(7, 0))
		roll := float32(L.OptNumber(8, 0))
		var rot []float32
		if pitch != 0 || yaw != 0 || roll != 0 {
			q := quatFromEulerDegrees(pitch, yaw, roll)
			rot = q[:]
		}
		base := s.hostSpawnPropAt(room, prefab, pos, viewCount, rot)
		s.hostSendSerialize(room)
		L.Push(lua.LNumber(base))
		return 1
	})
	reg("KillNpc", func(L *lua.LState) int {
		view := int32(L.CheckNumber(1))
		for i := range room.npcs {
			npc := &room.npcs[i]
			if npc.viewBase != view || npc.hp <= 0 {
				continue
			}
			npc.hp = 0
			s.hostKillNpc(room, npc, 0, 9999, elementNone, false) 
			L.Push(lua.LTrue)
			return 1
		}
		L.Push(lua.LFalse)
		return 1
	})
	reg("MoveNpc", func(L *lua.LState) int {
		view := int32(L.CheckNumber(1))
		dest := [3]float32{
			float32(L.CheckNumber(2)),
			float32(L.CheckNumber(3)),
			float32(L.CheckNumber(4)),
		}
		speed := int32(0) 
		if L.OptBool(5, false) {
			speed = 2 
		}
		for i := range room.npcs {
			npc := &room.npcs[i]
			if npc.viewBase != view || npc.hp <= 0 {
				continue
			}
			if wps, ok := navmeshFindPath(level(), npc.pos[0], npc.pos[2], dest[0], dest[2], npc.pos[1]); ok {
				npc.path = wps
			} else {
				npc.path = [][3]float32{dest}
			}
			npc.moving = true
			npc.patrolling = false
			npcSetTask(npc, bhvTaskMoveTo, -1, dest, speed)
			L.Push(lua.LTrue)
			return 1
		}
		L.Push(lua.LFalse)
		return 1
	})
	reg("GetPlayers", func(L *lua.LState) int {
		out := L.NewTable()
		for nr, uid := range room.Players {
			if nr == HostActorNr {
				continue
			}
			sess := s.sessionInRoom(uid, room.Name)
			if sess == nil || sess.avatarBase == 0 {
				continue
			}
			t := L.NewTable()
			t.RawSetString("actor", lua.LNumber(nr))
			if sess.hostChar != nil {
				t.RawSetString("name", lua.LString(sess.hostChar.Name))
			}
			t.RawSetString("x", lua.LNumber(sess.broadcastPos[0]))
			t.RawSetString("y", lua.LNumber(sess.broadcastPos[1]))
			t.RawSetString("z", lua.LNumber(sess.broadcastPos[2]))
			out.Append(t)
		}
		L.Push(out)
		return 1
	})
	reg("GetNpcs", func(L *lua.LState) int {
		out := L.NewTable()
		for i := range room.npcs {
			npc := &room.npcs[i]
			if npc.hp <= 0 {
				continue
			}
			t := L.NewTable()
			t.RawSetString("view", lua.LNumber(npc.viewBase))
			t.RawSetString("kit", lua.LString(npc.kit))
			t.RawSetString("hp", lua.LNumber(npc.hp))
			t.RawSetString("x", lua.LNumber(npc.pos[0]))
			t.RawSetString("y", lua.LNumber(npc.pos[1]))
			t.RawSetString("z", lua.LNumber(npc.pos[2]))
			out.Append(t)
		}
		L.Push(out)
		return 1
	})
	reg("After", func(L *lua.LState) int {
		secs := float64(L.CheckNumber(1))
		fn := L.CheckFunction(2)
		sc := room.script
		if sc == nil {
			pendingTimers[L] = append(pendingTimers[L], scriptTimer{
				at: time.Now().Add(time.Duration(secs * float64(time.Second))),
				fn: fn,
			})
			return 0
		}
		sc.timers = append(sc.timers, scriptTimer{
			at: time.Now().Add(time.Duration(secs * float64(time.Second))),
			fn: fn,
		})
		return 0
	})
}
var pendingTimers = map[*lua.LState][]scriptTimer{}
