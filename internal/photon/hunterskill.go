package photon

import (
	"encoding/binary"
	"log"
	"math/rand"
	"time"

	"eridian/internal/domain"
)


const (

	hunterDroidPrefab = "Entities/Object/HunterDroid"

	hunterDroidSpawnDelay = 1000 * time.Millisecond
	// lifetime is used for if the player does not make a manual detonation. just an expiry timer
	hunterDroidLifetime = 15 * time.Second

	hunterMarkDuration = 8 * time.Second

	// this controls how often an enemy will be re-marked while the droid is active
	hunterMarkTickInterval = 3 * time.Second

	hunterMarkRadius = float32(20)

	hunterMarkCount = 1

	hunterTrackshotBonus = int32(80)

	hunterMarkExplosionRadius = float32(5)
)

func hunterExplosionForElement(element int32) (damage, outElement int32) {
	switch element {
	case elementShock:
		return 180, elementShock
	case elementCorrosive:
		return 50, elementCorrosive
	default:
		return 70, elementFire
	}
}

func (s *EnetServer) hostSpawnHunterDroid(room *Room, sess *ClientSession) {
	base := hostViewBase + 1000 + room.npcViewCtr*10
	room.npcViewCtr++
	pos, ok := room.playerPos[sess.avatarBase+1]
	if !ok {
		return
	}
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): hunterDroidPrefab,
			byte(1): vector3Custom(pos[0], pos[1]+1.5, pos[2]),
			byte(4): IntArray{Items: []int32{base}},
			byte(5): []any{sess.avatarBase},
			byte(6): int32(msNow()),
			byte(7): base,
		},
	}, true)
	sess.hunterDroidView = base
	sess.hunterDroidActive = true
	sess.hunterDroidEndAt = time.Now().Add(hunterDroidLifetime)
	sess.hunterDroidLastMark = time.Time{} // eligible to mark immediately
	s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
		int32(1), sess.avatarBase, int32(skillEventHunterDroidSpawned),
		float32(hunterDroidLifetime.Seconds()), vector3Custom(pos[0], pos[1], pos[2]),
	})
	log.Printf("[HOST] actor view=%d spawned HunterDroid view=%d room=%s", sess.avatarBase, base, room.Name)
	s.hostMarkNearestNpcs(room, pos, sess.hostChar)
}

// markedTransferTarget picks the marked npc that absorbs damage redirected by
// Total Fraud: the attacker itself if it is marked, otherwise any live marked one.
func markedTransferTarget(room *Room, attackerView int32) int32 {
	now := time.Now()
	alive := func(view int32) bool {
		for i := range room.npcs {
			if room.npcs[i].viewBase == view {
				return room.npcs[i].hp > 0
			}
		}
		return false
	}
	if m, ok := room.markedNpcs[attackerView]; ok && now.Before(m.until) && alive(attackerView) {
		return attackerView
	}
	for view, m := range room.markedNpcs {
		if now.Before(m.until) && alive(view) {
			return view
		}
	}
	return 0
}

// hostGaleShot is the second F press: every marked enemy takes an explosion and the marks are consumed.
func (s *EnetServer) hostGaleShot(room *Room, sess *ClientSession) {
	now := time.Now()
	var views []int32
	for view, m := range room.markedNpcs {
		if now.Before(m.until) {
			views = append(views, view)
		}
	}
	for _, view := range views {
		delete(room.markedNpcs, view)
	}
	var weaponElement int32
	if int(sess.activeWeaponSlot) < len(sess.equippedWeapons) {
		weaponElement = room.weaponElement[sess.equippedWeapons[sess.activeWeaponSlot].view]
	}
	damage, element := hunterExplosionForElement(weaponElement)
	damage = int32(float64(damage) * (1 + effectValue(sess.hostChar, "DroidBlastDamage")))
	dazeChance := effectValue(sess.hostChar, "DroidBlastDazeChance")
	for _, view := range views {
		for i := range room.npcs {
			npc := &room.npcs[i]
			if npc.viewBase != view || npc.hp <= 0 {
				continue
			}
			s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
				int32(1), sess.avatarBase, int32(skillEventHunterExplodeMarked),
				float32(0), vector3Custom(npc.pos[0], npc.pos[1], npc.pos[2]),
			})
			if dazeChance > 0 && rand.Float64() < dazeChance {
				npc.dazedUntil = now.Add(3 * time.Second)
				npc.dazeEffectUID = (npc.dazeEffectUID + 1) % 126
				npc.dazeDurationMult = 1
				npc.moving, npc.path = false, nil
				npcSetTask(npc, bhvTaskIdle, -1, npc.pos, -1)
			}
			s.hostDamageNpcByView(room, sess.avatarBase, view, damage, 0, element, true)
			break
		}
	}
}

func (s *EnetServer) hostRetrieveHunterDroid(room *Room, sess *ClientSession, manual bool) {
	if !sess.hunterDroidActive {
		return
	}
	sess.hunterDroidActive = false
	if manual {
		s.hostGaleShot(room, sess)
	}
	cooldown := s.hunterCooldownLong(sess.hostChar)
	if manual {
		cooldown = s.hunterCooldownShort()
	}
	sess.skillCooldownEndAt = time.Now().Add(cooldown)

	s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
		int32(1), sess.avatarBase, int32(skillEventHunterDroidEnd),
		float32(0), vector3Custom(0, 0, 0),
	})
	s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
		int32(0), sess.avatarBase, int32(skillEventHunterCooldownStart),
		float32(cooldown.Seconds()), vector3Custom(0, 0, 0),
	})
	if sess.hunterDroidView != 0 {
		s.hostBroadcastJournaled(room, 204, map[byte]any{
			ParamPlayerNr:  HostActorNr,
			ParamEventData: map[any]any{byte(0): sess.hunterDroidView},
		}, true)
		sess.hunterDroidView = 0
	}
	if !manual {
		s.hostResetSkillPhaseAfterTimeout(room, sess)
	}
	log.Printf("[HOST] actor view=%d retrieved HunterDroid (manual=%v) room=%s", sess.avatarBase, manual, room.Name)
}

func (s *EnetServer) hostRunPendingHunterDroidSpawns(room *Room) {
	if len(room.pendingHunterDroidSpawns) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingHunterDroidSpawns[:0]
	for _, p := range room.pendingHunterDroidSpawns {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		if p.sess.avatarBase != 0 && !p.sess.isDead {
			s.hostSpawnHunterDroid(room, p.sess)
		}
	}
	room.pendingHunterDroidSpawns = remaining
}

// markBroadcastViewID is the MarkBroadcast component of the Session prefab (6th view).
const markBroadcastViewID = hostViewBase + 6

const hunterMarkLevel = 1

// hostBroadcastMarks streams (entityID, markLevel) pairs to MarkBroadcast.OnPhotonSerializeView so
// clients draw the Hunter mark on marked npcs. Only changes are sent; level 0 clears a mark.
func (s *EnetServer) hostBroadcastMarks(room *Room) {
	if len(room.markedNpcs) == 0 && len(room.markSent) == 0 {
		return
	}
	now := time.Now()
	current := map[int32]bool{}
	for view, m := range room.markedNpcs {
		if !now.Before(m.until) {
			delete(room.markedNpcs, view)
			continue
		}
		current[view] = true
	}
	for i := range room.npcs {
		if room.npcs[i].hp <= 0 {
			delete(current, room.npcs[i].viewBase)
		}
	}
	var changes []int32
	var levels []int32
	for view := range current {
		if !room.markSent[view] {
			changes = append(changes, view)
			levels = append(levels, hunterMarkLevel)
		}
	}
	for view := range room.markSent {
		if !current[view] {
			changes = append(changes, view)
			levels = append(levels, 0)
		}
	}
	if len(changes) == 0 {
		return
	}
	room.markSent = current
	buf := make([]byte, 0, len(changes)*8)
	for i, view := range changes {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(view))
		buf = binary.LittleEndian.AppendUint32(buf, uint32(levels[i]))
	}
	s.broadcastEvent(room, 201, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): int32(msNow()),
			Short(1): map[any]any{
				byte(0): int32(markBroadcastViewID),
				byte(1): []any{buf},
			},
		},
	}, nil, nil)
}

func (s *EnetServer) hostTickHunterDroids(room *Room) {
	s.hostBroadcastMarks(room)
	now := time.Now()
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || !sess.hunterDroidActive || sess.avatarBase == 0 {
			continue
		}
		if now.After(sess.hunterDroidEndAt) {
			s.hostRetrieveHunterDroid(room, sess, false)
			continue
		}
		if now.Sub(sess.hunterDroidLastMark) < hunterMarkTickInterval {
			continue
		}
		sess.hunterDroidLastMark = now
		ppos, ok := room.playerPos[sess.avatarBase+1]
		if !ok {
			continue
		}
		s.hostMarkNearestNpcs(room, ppos, sess.hostChar)
	}
}
func (s *EnetServer) hostMarkNearestNpcs(room *Room, center [3]float32, ch *domain.Character) {
	maxMarks := hunterMarkCount + int(multiMarkCountBonus(ch))
	r2 := hunterMarkRadius * hunterMarkRadius
	type cand struct {
		view int32
		d2   float32
		idx  int
	}
	var candidates []cand
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp <= 0 {
			continue
		}
		dx, dz := center[0]-npc.pos[0], center[2]-npc.pos[2]
		d2 := dx*dx + dz*dz
		if d2 > r2 {
			continue
		}
		candidates = append(candidates, cand{npc.viewBase, d2, i})
	}
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].d2 < candidates[i].d2 {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	if len(candidates) > maxMarks {
		candidates = candidates[:maxMarks]
	}
	if len(candidates) == 0 {
		return
	}
	if room.markedNpcs == nil {
		room.markedNpcs = map[int32]markedNpc{}
	}
	until := time.Now().Add(hunterMarkDuration)

	slowFrac := float32(effectValue(ch, "MarkingSlowRate"))
	if slowFrac > 0.9 {
		slowFrac = 0.9
	}
	for _, c := range candidates {
		room.markedNpcs[c.view] = markedNpc{until: until}
		if slowFrac > 0 {
			room.npcs[c.idx].markSlowFrac = slowFrac
			room.npcs[c.idx].markSlowUntil = until
		}
	}
}
