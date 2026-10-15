package photon

import (
	"log"
	"math"
	"math/rand"
	"time"
)

const (
	fireBirdFireRadius      = float32(3)
	fireBirdDuration        = 10 * time.Second
	fireBirdExplosionRadius = float32(5)
	fireBirdFlySpeed        = float32(30)
	fireBirdChainRadius     = float32(2)
	fireBirdChainInterval   = 300 * time.Millisecond
	fireBirdCollideRun      = float32(1.5)

	fireBirdDotDamage       = 8.0
	fireBirdExplosionDamage = 65.0

	fireBirdDotTickInterval = 1 * time.Second
	fireBirdStunBase        = 3 * time.Second

	phoenixNirvanaCooldown = 120 * time.Second
)

func fireBirdLevel(sess *ClientSession) int {
	if sess.hostChar == nil {
		return 1
	}
	return sess.hostChar.Level
}

func fireBirdFireRadiusFor(sess *ClientSession) float32 {
	return float32(int(float64(fireBirdFireRadius) * (1 + effectValue(sess.hostChar, "FirebirdRadius"))))
}

func fireBirdExplosionDamageFor(sess *ClientSession) int32 {
	return int32(fireBirdExplosionDamage * powerRatingForLevel(fireBirdLevel(sess)) * (1 + effectValue(sess.hostChar, "FirebirdExplosionDamage")))
}

func fireBirdExplosionRadiusFor(sess *ClientSession) float32 {
	return fireBirdExplosionRadius * float32(1+effectValue(sess.hostChar, "FirebirdExplosionDamage"))
}

func (s *EnetServer) hostSpawnFireBird(room *Room, sess *ClientSession) {
	base := hostViewBase + 1000 + room.npcViewCtr*10
	room.npcViewCtr++
	pos, ok := room.playerPos[sess.avatarBase+1]
	if !ok {
		return
	}
	angles := [3]int32{int32(rand.Intn(360)), int32(rand.Intn(360)), int32(rand.Intn(360))}
	s.hostBroadcastJournaled(room, 202, map[byte]any{
		ParamPlayerNr: HostActorNr,
		ParamEventData: map[any]any{
			byte(0): "BKT|NPC|BloodWing",
			byte(1): vector3Custom(pos[0], pos[1]+1.5, pos[2]),
			byte(4): IntArray{Items: []int32{base}},
			byte(5): []any{sess.avatarBase, angles[0], angles[1], angles[2]},
			byte(6): int32(msNow()),
			byte(7): base,
		},
	}, true)
	yaw := sess.lastYawDeg * math.Pi / 180
	now := time.Now()
	sess.fireBirdActive = true
	sess.fireBirdLanded = false
	sess.fireBirdOffRun = 0
	sess.fireBirdPos = [3]float32{pos[0], pos[1] + 1.5, pos[2]}
	sess.fireBirdDir = [2]float32{float32(math.Sin(yaw)), float32(math.Cos(yaw))}
	sess.fireBirdAngles = angles
	sess.fireBirdStepAt = now
	sess.fireBirdLastTick = time.Time{}
	sess.fireBirdEndAt = now.Add(time.Duration(float64(fireBirdDuration) * (1 + effectValue(sess.hostChar, "FirebirdDuration"))))
	if zen := effectValue(sess.hostChar, "EvadeDuration"); zen > 0 {
		sess.evadeUntil = now.Add(time.Duration(zen * float64(time.Second)))
	}
	log.Printf("[HOST] actor view=%d spawned FireBird view=%d room=%s", sess.avatarBase, base, room.Name)
}

// fireBirdTouched reports whether a live hostile npc is inside the bird's fire radius at pos.
func fireBirdTouched(room *Room, pos [3]float32, radius float32) bool {
	r2 := radius * radius
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp <= 0 || npc.friendly {
			continue
		}
		dx, dz := npc.pos[0]-pos[0], npc.pos[2]-pos[2]
		if dx*dx+dz*dz <= r2 && absf32(npc.pos[1]-pos[1]) <= 4 {
			return true
		}
	}
	return false
}

func (s *EnetServer) hostFireBirdStun(room *Room, sess *ClientSession, center [3]float32, radius float32) {
	durMult := float32(1 + effectValue(sess.hostChar, "DazeDuration"))
	until := time.Now().Add(time.Duration(float64(fireBirdStunBase) * float64(durMult)))
	r2 := radius * radius
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp <= 0 || npc.friendly {
			continue
		}
		dx, dz := npc.pos[0]-center[0], npc.pos[2]-center[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		npc.dazedUntil = until
		npc.dazeEffectUID = (npc.dazeEffectUID + 1) % 126
		npc.dazeDurationMult = durMult
		npc.moving, npc.path = false, nil
		npcSetTask(npc, bhvTaskIdle, -1, npc.pos, -1)
	}
}

func (s *EnetServer) hostTickFireBirds(room *Room) {
	now := time.Now()
	s.hostRunPendingFireChains(room, now)
	level := m2level(room)
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || !sess.fireBirdActive || sess.avatarBase == 0 {
			continue
		}
		if now.After(sess.fireBirdEndAt) {
			s.hostAbsorbFireBird(room, sess)
			continue
		}
		radius := fireBirdFireRadiusFor(sess)
		if !sess.fireBirdLanded {
			dt := float32(now.Sub(sess.fireBirdStepAt).Seconds())
			sess.fireBirdStepAt = now
			step := fireBirdFlySpeed * dt
			next := sess.fireBirdPos
			next[0] += sess.fireBirdDir[0] * step
			next[2] += sess.fireBirdDir[1] * step
			if levelHasNavmesh(level) && !navmeshOnMesh(level, next[0], next[2], next[1]) {
				sess.fireBirdOffRun += step
			} else {
				sess.fireBirdOffRun = 0
			}
			if sess.fireBirdOffRun >= fireBirdCollideRun {
				sess.fireBirdLanded = true // hit the environment: hover where it stopped
			} else {
				sess.fireBirdPos = next
				if fireBirdTouched(room, next, radius) {
					sess.fireBirdLanded = true
				}
			}
			if sess.fireBirdLanded {
				s.hostFireBirdStun(room, sess, sess.fireBirdPos, radius)
				sess.fireBirdLastTick = time.Time{}
			}
			continue
		}
		if now.Sub(sess.fireBirdLastTick) < fireBirdDotTickInterval {
			continue
		}
		sess.fireBirdLastTick = now
		dotDmg := int32(fireBirdDotDamage * powerRatingForLevel(fireBirdLevel(sess)))
		s.hostAreaDamageNpcs(room, sess.avatarBase, sess.fireBirdPos, radius, dotDmg, elementFire, true)
	}
}

func (s *EnetServer) hostFireBirdRefillShield(room *Room, sess *ClientSession) {
	refillPct := effectValue(sess.hostChar, "RefillShield")
	if refillPct <= 0 || sess.maxShield <= 0 {
		return
	}
	sess.shield += int32(float64(sess.maxShield) * refillPct)
	if sess.shield > sess.maxShield {
		sess.shield = sess.maxShield
	}
	sess.shieldRegenBaseline = sess.shield
	s.hostSendRPC(room, sess.avatarBase, "RPC_RequestAllLifeAndShield", []any{
		map[int]any{0: int32(1), 1: sess.avatarBase, 2: playerLifeTable(sess.hp, sess.maxHP, sess.shield, sess.maxShield)},
	})
}

func (s *EnetServer) hostAbsorbFireBird(room *Room, sess *ClientSession) {
	sess.fireBirdActive = false
	sess.skillCooldownEndAt = time.Now().Add(s.fireBirdFlybackCooldown())
	s.hostFireBirdRefillShield(room, sess)
	s.hostResetSkillPhaseAfterTimeout(room, sess)
	log.Printf("[HOST] actor view=%d FireBird absorbed room=%s", sess.avatarBase, room.Name)
}

func (s *EnetServer) hostDetonateFireBird(room *Room, sess *ClientSession) {
	if !sess.fireBirdActive {
		return
	}
	sess.fireBirdActive = false
	cd := time.Duration(float64(s.fireBirdCooldown()) * (1 + effectValue(sess.hostChar, "FirebirdExplodeCooldown")))
	sess.skillCooldownEndAt = time.Now().Add(cd)
	center := sess.fireBirdPos
	dmg := fireBirdExplosionDamageFor(sess)
	radius := fireBirdExplosionRadiusFor(sess)
	s.hostAreaDamageNpcs(room, sess.avatarBase, center, radius, dmg, elementFire, false)
	s.hostFireBirdRefillShield(room, sess)
	chainNum := int(effectValue(sess.hostChar, "FirebirdExplodeChainNum"))
	for i := 0; i < chainNum && i < len(sess.fireBirdAngles); i++ {
		ang := float64(sess.fireBirdAngles[i]) * math.Pi / 180
		room.pendingFireChains = append(room.pendingFireChains, pendingFireChain{
			sess: sess,
			pos: [3]float32{
				center[0] + float32(math.Sin(ang))*fireBirdChainRadius,
				center[1],
				center[2] + float32(math.Cos(ang))*fireBirdChainRadius,
			},
			at:  time.Now().Add(fireBirdChainInterval * time.Duration(i+1)),
			dmg: dmg,
		})
	}
	log.Printf("[HOST] actor view=%d detonated FireBird dmg=%d chain=%d room=%s", sess.avatarBase, dmg, chainNum, room.Name)
}

func (s *EnetServer) hostRunPendingFireChains(room *Room, now time.Time) {
	if len(room.pendingFireChains) == 0 {
		return
	}
	remaining := room.pendingFireChains[:0]
	for _, c := range room.pendingFireChains {
		if now.Before(c.at) {
			remaining = append(remaining, c)
			continue
		}
		s.hostAreaDamageNpcs(room, c.sess.avatarBase, c.pos, fireBirdExplosionRadiusFor(c.sess), c.dmg, elementFire, false)
	}
	room.pendingFireChains = remaining
}

func (s *EnetServer) hostFireBirdPlayerDown(sess *ClientSession) {
	if !sess.fireBirdActive {
		return
	}
	sess.fireBirdActive = false
	sess.skillCooldownEndAt = time.Now().Add(time.Duration(float64(s.fireBirdCooldown()) * (1 + effectValue(sess.hostChar, "FirebirdExplodeCooldown"))))
}
