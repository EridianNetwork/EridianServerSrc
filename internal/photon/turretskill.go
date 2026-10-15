package photon

import (
	"log"
	"math"
	"math/rand"
	"time"
)

const (
	turretSpawnDelay = 500 * time.Millisecond

	turretLifetime = 30 * time.Second

	turretArmDuration = 1 * time.Second

	turretFireRangeFallback    = 60.0
	turretFireIntervalFallback = 1.5
	turretDamageFallback       = int32(15)
	turretDamageRadiusFallback = 4.0

	turretBaseDamagePct = 0.45

	turretDeployExplodeDamage = 80

	turretAOERadius       = float32(4)
	turretAOETickInterval = 1 * time.Second

	turretDeployDistance = 3.0
)

func turretStats() (fireRange, fireInterval float64, damage int32, damageRadius float64) {
	fireRange, fireInterval, damage, damageRadius = turretFireRangeFallback, turretFireIntervalFallback, turretDamageFallback, turretDamageRadiusFallback
	if cfg := npcConfigFor("SoldierTurretBasic"); cfg != nil {
		if cfg.FireRange > 0 {
			fireRange = cfg.FireRange
		}
		if cfg.TurretFireInterval > 0 {
			fireInterval = cfg.TurretFireInterval
		}
		if cfg.TurretDamage > 0 {
			damage = cfg.TurretDamage
		}
		if cfg.TurretDamageRadius > 0 {
			damageRadius = cfg.TurretDamageRadius
		}
	}
	return
}

const (
	turretRepelInterval = 5 * time.Second
	turretRepelDistance = 3.0
	turretRepelStun     = 1500 * time.Millisecond
)

func (s *EnetServer) turretShieldReduction(room *Room, target *ClientSession) float64 {
	if target.avatarBase == 0 {
		return 0
	}
	pos, ok := playerStreamPos(room, target.avatarBase)
	if !ok {
		pos = room.playerPos[target.avatarBase+1]
	}
	best := 0.0
	r2 := turretAOERadius * turretAOERadius
	for _, uid := range room.Players {
		owner := s.sessionInRoom(uid, room.Name)
		if owner == nil || !owner.turretActive || owner.teamID != target.teamID {
			continue
		}
		shield := effectValue(owner.hostChar, "TurretShield")
		if shield <= best {
			continue
		}
		for i := range room.npcs {
			t := &room.npcs[i]
			if t.viewBase != owner.turretView || t.hp <= 0 {
				continue
			}
			dx, dz := pos[0]-t.pos[0], pos[2]-t.pos[2]
			if dx*dx+dz*dz <= r2 {
				best = shield
			}
			break
		}
	}
	if best > 0.9 {
		best = 0.9
	}
	return best
}

func (s *EnetServer) hostRepelNpcs(room *Room, center [3]float32, radius, dist float32, stun time.Duration) {
	r2 := radius * radius
	now := time.Now()
	for i := range room.npcs {
		npc := &room.npcs[i]
		if npc.hp <= 0 || npc.friendly {
			continue
		}
		dx, dz := npc.pos[0]-center[0], npc.pos[2]-center[2]
		d2 := dx*dx + dz*dz
		if d2 > r2 {
			continue
		}
		d := float32(math.Sqrt(float64(d2)))
		if d < 0.01 {
			dx, dz, d = 1, 0, 1
		}
		nx, nz := npc.pos[0]+dx/d*dist, npc.pos[2]+dz/d*dist
		if gy, ok := navmeshGroundHeight(m2level(room), nx, nz, npc.pos[1]); ok {
			npc.pos[0], npc.pos[1], npc.pos[2] = nx, gy, nz
		}
		npc.dazedUntil = now.Add(stun)
		npc.dazeEffectUID = (npc.dazeEffectUID + 1) % 126
		npc.dazeDurationMult = 1
		npc.moving, npc.path = false, nil
		npcSetTask(npc, bhvTaskIdle, -1, npc.pos, -1)
	}
}

func (s *EnetServer) hostScatterGrenades(room *Room, sess *ClientSession, pos [3]float32, count int, dmgMult float64) {
	now := time.Now()
	for i := 0; i < count; i++ {
		ang := rand.Float64() * 2 * math.Pi
		speed := float32(3 + rand.Float64()*3)
		room.pendingGrenades = append(room.pendingGrenades, pendingGrenade{
			sess: sess, ownerView: sess.avatarBase,
			spawnAt:   now,
			at:        now.Add(grenadeFuse),
			scattered: true,
			origin:    [3]float32{pos[0], pos[1] + 1.5, pos[2]},
			vel:       [3]float32{float32(math.Sin(ang)) * speed, 6, float32(math.Cos(ang)) * speed},
			dmgMult:   dmgMult,
		})
	}
}

func (s *EnetServer) turretNpcPos(room *Room, view int32) ([3]float32, bool) {
	for i := range room.npcs {
		if room.npcs[i].viewBase == view {
			return room.npcs[i].pos, true
		}
	}
	return [3]float32{}, false
}

func (s *EnetServer) hostTurretGone(room *Room, sess *ClientSession, pos [3]float32) {
	if count := int(effectValue(sess.hostChar, "TurretDestroyGrenade")); count > 0 {
		s.hostScatterGrenades(room, sess, pos, count, 1)
	}
}

func (s *EnetServer) hostSpawnTurret(room *Room, sess *ClientSession) {
	pos, ok := room.playerPos[sess.avatarBase+1]
	if !ok {
		return
	}

	deployDist := float32(turretDeployDistance * (1 + effectValue(sess.hostChar, "TurretDeployFar")))
	yaw := sess.lastYawDeg * math.Pi / 180
	spawnPos := [3]float32{pos[0] + deployDist*float32(math.Sin(yaw)), pos[1], pos[2] + deployDist*float32(math.Cos(yaw))}
	base := s.hostSpawnNpcAt(room, m2level(room), "SoldierTurretBasic", spawnPos, []any{int32(0), sess.avatarBase, int32(1)}, 3)
	if n := len(room.npcs); n > 0 && room.npcs[n-1].viewBase == base {
		room.npcs[n-1].friendly = true

		if hpScale := effectValue(sess.hostChar, "TurretHP"); hpScale > 0 {
			room.npcs[n-1].maxHP = int32(float64(room.npcs[n-1].maxHP) * (1 + hpScale))
			room.npcs[n-1].hp = room.npcs[n-1].maxHP
		}
	}

	if explodeBonus := effectValue(sess.hostChar, "TurretDeployExplode"); explodeBonus > 0 {
		dmg := int32(turretDeployExplodeDamage * (1 + explodeBonus))
		s.hostAreaDamageNpcs(room, sess.avatarBase, spawnPos, float32(turretDamageRadiusFallback), dmg, elementExplosive, false)
		s.hostRepelNpcs(room, spawnPos, float32(turretDamageRadiusFallback), turretRepelDistance, turretRepelStun)
		s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
			int32(1), sess.avatarBase, int32(skillEventTurretRepulseExplode),
			float32(0), vector3Custom(spawnPos[0], spawnPos[1], spawnPos[2]),
		})
	}
	sess.turretView = base
	sess.turretActive = true
	sess.turretEndAt = time.Now().Add(turretLifetime)
	sess.turretArmedAt = time.Now().Add(turretArmDuration)
	sess.turretLastFireAt = time.Time{}
	sess.turretBaseDamage = s.playerWeaponDamageAtLevel(sess)

	s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
		int32(1), sess.avatarBase, int32(skillEventTurretSpawned),
		float32(turretLifetime.Seconds()), vector3Custom(spawnPos[0], spawnPos[1], spawnPos[2]),
	})
	log.Printf("[HOST] actor view=%d deployed Turret view=%d room=%s", sess.avatarBase, base, room.Name)
}

func (s *EnetServer) hostRetrieveTurret(room *Room, sess *ClientSession, manual bool) {
	if !sess.turretActive {
		return
	}
	sess.turretActive = false
	cooldown := s.turretCooldownLong(sess.hostChar)
	if manual {
		cooldown = s.turretCooldownShort(sess.hostChar)
	} else {

		s.hostResetSkillPhaseAfterTimeout(room, sess)
	}
	sess.skillCooldownEndAt = time.Now().Add(cooldown)
	s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
		int32(0), sess.avatarBase, int32(skillEventTurretCooldownStart),
		float32(cooldown.Seconds()), vector3Custom(0, 0, 0),
	})
	if pos, ok := s.turretNpcPos(room, sess.turretView); ok {
		s.hostTurretGone(room, sess, pos)
	}
	if sess.turretView != 0 {
		for i := range room.npcs {
			if room.npcs[i].viewBase == sess.turretView {
				room.npcs[i].hp = 0
				break
			}
		}
		room.deferredDestroys = append(room.deferredDestroys, deferredDestroy{view: sess.turretView, at: time.Now()})
		sess.turretView = 0
	}
	log.Printf("[HOST] actor view=%d retrieved Turret room=%s", sess.avatarBase, room.Name)
}

func (s *EnetServer) hostTurretDestroyed(room *Room, sess *ClientSession) {
	if pos, ok := s.turretNpcPos(room, sess.turretView); ok {
		s.hostTurretGone(room, sess, pos)
	}
	sess.turretActive, sess.turretView = false, 0
	cooldown := s.turretCooldownLong(sess.hostChar)
	sess.skillCooldownEndAt = time.Now().Add(cooldown)
	s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
		int32(0), sess.avatarBase, int32(skillEventTurretCooldownStart),
		float32(cooldown.Seconds()), vector3Custom(0, 0, 0),
	})
	s.hostResetSkillPhaseAfterTimeout(room, sess)
	log.Printf("[HOST] actor view=%d Turret destroyed room=%s", sess.avatarBase, room.Name)
}

func (s *EnetServer) hostRunPendingTurretSpawns(room *Room) {
	if len(room.pendingTurretSpawns) == 0 {
		return
	}
	now := time.Now()
	remaining := room.pendingTurretSpawns[:0]
	for _, p := range room.pendingTurretSpawns {
		if now.Before(p.at) {
			remaining = append(remaining, p)
			continue
		}
		if p.sess.avatarBase != 0 && !p.sess.isDead {
			s.hostSpawnTurret(room, p.sess)
		}
	}
	room.pendingTurretSpawns = remaining
}

func (s *EnetServer) hostTickTurrets(room *Room) {
	now := time.Now()
	fireRange, fireInterval, _, damageRadius := turretStats()
	fireIntervalDur := time.Duration(fireInterval * float64(time.Second))
	r2 := float32(fireRange * fireRange)
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || !sess.turretActive {
			continue
		}
		if now.After(sess.turretEndAt) {
			s.hostRetrieveTurret(room, sess, false)
			continue
		}
		if now.Before(sess.turretArmedAt) {
			continue
		}
		var turret *spawnedNpc
		for i := range room.npcs {
			if room.npcs[i].viewBase == sess.turretView {
				turret = &room.npcs[i]
				break
			}
		}
		if turret == nil || turret.hp <= 0 {
			s.hostTurretDestroyed(room, sess)
			continue
		}

		if now.Sub(sess.turretLastAOEAt) >= turretAOETickInterval {
			healPct := effectValue(sess.hostChar, "HealthRegenAOE")
			ammoPct := effectValue(sess.hostChar, "AmmoRegenAOE")
			if healPct > 0 || ammoPct > 0 {
				sess.turretLastAOEAt = now
				s.hostTurretAOERegen(room, turret, healPct, ammoPct)
			}
		}
		if repel := effectValue(sess.hostChar, "TurretRepulseStr"); repel > 0 && now.Sub(sess.turretLastRepelAt) >= turretRepelInterval {
			sess.turretLastRepelAt = now
			s.hostRepelNpcs(room, turret.pos, turretAOERadius, float32(turretRepelDistance*(1+repel)), turretRepelStun)
			s.hostSendRPC(room, sessionInfoViewID, "RPC_H2C_SkillEvent", []any{
				int32(1), sess.avatarBase, int32(skillEventTurretRepulseExplode),
				float32(0), vector3Custom(turret.pos[0], turret.pos[1], turret.pos[2]),
			})
		}
		bestIdx, bestD := -1, r2
		for i := range room.npcs {
			cand := &room.npcs[i]
			if cand.hp <= 0 || cand.friendly {
				continue
			}
			dx, dz := cand.pos[0]-turret.pos[0], cand.pos[2]-turret.pos[2]
			if d := dx*dx + dz*dz; d <= bestD {
				bestD, bestIdx = d, i
			}
		}
		if bestIdx < 0 {

			npcSetTask(turret, bhvTaskIdle, -1, turret.pos, -1)
			continue
		}
		target := &room.npcs[bestIdx]
		npcSetTask(turret, bhvTaskRocketAttack, target.viewBase, turret.pos, -1)
		if now.Sub(sess.turretLastFireAt) < fireIntervalDur {
			continue
		}
		sess.turretLastFireAt = now
		dmg := int32(sess.turretBaseDamage * turretBaseDamagePct)

		dmg = int32(float64(dmg) * (1 + effectValue(sess.hostChar, "TurretRocket")))

		dmgElement := int32(elementExplosive)
		electroBonus := effectValue(sess.hostChar, "TurretElectroStr")
		if electroBonus > 0 || effectValue(sess.hostChar, "ElementDamage") > 0 {
			dmgElement = elementShock
			dmg = int32(float64(dmg) * (1 + electroBonus))
		}
		s.hostAreaDamageNpcs(room, sess.avatarBase, target.pos, float32(damageRadius), dmg, dmgElement, false)
	}
}

func (s *EnetServer) hostTurretAOERegen(room *Room, turret *spawnedNpc, healPct, ammoPct float64) {
	r2 := turretAOERadius * turretAOERadius
	for _, uid := range room.Players {
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target.avatarBase == 0 || target.isDead {
			continue
		}
		pos, ok := playerStreamPos(room, target.avatarBase)
		if !ok {
			pos = room.playerPos[target.avatarBase+1]
		}
		dx, dz := pos[0]-turret.pos[0], pos[2]-turret.pos[2]
		if dx*dx+dz*dz > r2 {
			continue
		}
		changed := false
		if healPct > 0 && target.maxHP > 0 && target.hp < target.maxHP {
			target.hp += int32(float64(target.maxHP) * healPct)
			if target.hp > target.maxHP {
				target.hp = target.maxHP
			}
			changed = true
		}
		if changed {
			s.hostSendRPC(room, target.avatarBase, "RPC_RequestAllLifeAndShield", []any{
				map[int]any{0: int32(1), 1: target.avatarBase, 2: playerLifeTable(target.hp, target.maxHP, target.shield, target.maxShield)},
			})
		}
		if ammoPct > 0 && target.ammoEst < 360 {
			target.ammoRegenCarry += ammoPct
			whole := int(target.ammoRegenCarry)
			target.ammoRegenCarry -= float64(whole)
			target.ammoEst += whole
			if target.ammoEst > 360 {
				target.ammoEst = 360
			}
			s.hostSendRPC(room, target.avatarBase+3, "RPC_OnReceiveAmmos", []any{ammoPouchesForSession(target, true)})
		}
	}
}
