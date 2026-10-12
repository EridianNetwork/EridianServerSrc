package photon

import (
	"log"
	"strings"
	"time"
)


type hurtZoneAABB struct {
	centerX, centerZ         float32
	halfExtentX, halfExtentZ float32
	yTop float32
	levelObject string
	damageRatioPerSecond float32
}

const lavaYMargin = 1.5

const lavaActivableYMargin = 0.3

var levelHurtZones = map[string][]hurtZoneAABB{
	"lvl_arid_mine_forge": {
		{centerX: 0, centerZ: 0, halfExtentX: 5, halfExtentZ: 26.05, yTop: 0.078 + lavaYMargin, damageRatioPerSecond: 0.05},
		{centerX: 0.73, centerZ: 0, halfExtentX: 19.62, halfExtentZ: 26.05, yTop: 1.06 + lavaActivableYMargin, levelObject: "Shell_HurtZone_Activable", damageRatioPerSecond: 0.2},
		{centerX: 0.67, centerZ: 3.95, halfExtentX: 2.46, halfExtentZ: 2.46, yTop: 3.87 + lavaYMargin, damageRatioPerSecond: 0.05},
	},
}


const lavaTickInterval = 1 * time.Second

func (s *EnetServer) hostTickHurtZones(room *Room) {
	level := strings.ToLower(getCurScnFromProps(room.Name, room.Properties))
	zones := levelHurtZones[level]
	if len(zones) == 0 {
		return
	}
	now := time.Now()
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.avatarBase == 0 || sess.isDead || sess.hostChar == nil {
			continue
		}
		pos, ok := playerStreamPos(room, sess.avatarBase)
		if !ok {
			continue
		}
		zone, ok := anyHurtZoneContains(room, level, zones, pos)
		if !ok {
			continue
		}
		if now.Before(sess.lavaNextTickAt) {
			continue
		}
		sess.lavaNextTickAt = now.Add(lavaTickInterval)
		dmg := int32(float32(maxLifeForCharacter(sess.hostChar)) * zone.damageRatioPerSecond)
		if dmg < 1 {
			dmg = 1
		}
		if s.hostDamagePlayer(room, sess, 0, dmg, attackSubTypeLegacy, elementFire) {
			log.Printf("[HOST] actor view=%d died (lava) room=%s", sess.avatarBase, room.Name)
		}
	}
}

func anyHurtZoneContains(room *Room, level string, zones []hurtZoneAABB, pos [3]float32) (*hurtZoneAABB, bool) {
	for i := range zones {
		z := &zones[i]
		if z.levelObject != "" && !levelObjectActiveByName(room, level, z.levelObject) {
			continue
		}
		if pos[0] < z.centerX-z.halfExtentX || pos[0] > z.centerX+z.halfExtentX {
			continue
		}
		if pos[1] > z.yTop {
			continue
		}
		if pos[2] < z.centerZ-z.halfExtentZ || pos[2] > z.centerZ+z.halfExtentZ {
			continue
		}
		return z, true
	}
	return nil, false
}

func levelObjectActiveByName(room *Room, level, goName string) bool {
	for i, e := range levelObjectEntries(level) {
		if e.GO != goName {
			continue
		}
		if activated, ok := room.levelObjectStates[i]; ok {
			return activated
		}
		return e.Initial == 1
	}
	return false
}
