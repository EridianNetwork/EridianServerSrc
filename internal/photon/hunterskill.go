package photon

import (
	"log"
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

func (s *EnetServer) hostRetrieveHunterDroid(room *Room, sess *ClientSession, manual bool) {
	if !sess.hunterDroidActive {
		return
	}
	sess.hunterDroidActive = false
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

func (s *EnetServer) hostTickHunterDroids(room *Room) {
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
