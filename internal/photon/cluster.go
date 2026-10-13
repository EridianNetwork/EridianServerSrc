package photon


import (
	"encoding/binary"
	"encoding/json"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type clusterRef struct {
	Pid  int64  `json:"pid"`
	Name string `json:"name"`
}

type clusterCollider struct {
	Kind   string    `json:"kind"`
	Size   []float32 `json:"size"`
	Center []float32 `json:"center"`
	Radius float32   `json:"radius"`
}

type clusterCondDef struct {
	Pid           int64       `json:"pid"`
	Type          string      `json:"type"`
	Cluster       *clusterRef `json:"m_cluster"`
	Trigger       *clusterRef `json:"m_trigger"`
	Nest          *clusterRef `json:"m_nest"`
	Object        *clusterRef `json:"m_object"`
	Count         int         `json:"m_count"`
	RemainingNpc  int         `json:"m_remainingNpc"`
	NpcType       int         `json:"m_npcType"`
	AiState       string      `json:"m_aiState"`
	CheckPhase    bool        `json:"m_checkPhase"`
	CheckState    bool        `json:"m_checkState"`
	Phase         int         `json:"m_phase"`
	ThresholdType int         `json:"m_thresholdType"`
	CountToReach  int         `json:"m_countToReach"`
}

type clusterActDef struct {
	Type         string      `json:"type"`
	LevelObject  *clusterRef `json:"m_levelObject"`
	Activate     int         `json:"m_activate"`
	Actions      int32       `json:"m_actions"`
	EchoID       string      `json:"m_echoId"`
	ObjectiveID  string      `json:"m_objectiveId"`
	TitleID      string      `json:"m_titleId"`
	DetailID     string      `json:"m_detailId"`
	IsSuccess    bool        `json:"m_isSuccess"`
	Timer        *clusterRef `json:"m_timer"`
	TimerAction  int         `json:"m_action"`
	Cluster      *clusterRef `json:"m_cluster"`
	StopCluster  *clusterRef `json:"m_stopCluster"`
	QuestID      int         `json:"m_questId"`
	StepID       int         `json:"m_stepId"`
	NavmeshLayer string      `json:"m_navmeshLayer"`
	SetAction    int         `json:"m_setAction"`
	ExtendedDuration float64 `json:"m_extendedDuration"`
	Difficulty       int     `json:"m_difficulty"`
	Intensity int `json:"m_intensity"`
	TaskType    int         `json:"m_taskType"`
	MoveTo      *clusterRef `json:"m_moveToDestination"`
	TutorAction int         `json:"m_tutorAction"`
	MoveSpeed   int         `json:"m_moveSpeed"`
	Message *clusterUIMessage `json:"m_message"`
	TargetScore    int     `json:"m_targetScore"`
	ResetSceneList []int64 `json:"m_resetSceneList"`
	OnlyRespawnDeadPlayer bool `json:"m_onlyRespawnDeadPlayer"`
}


type clusterUIMessage struct {
	Type             int     `json:"m_type"`
	Text             string  `json:"m_text"`
	Duration         float64 `json:"m_duration"`
	DisplayCountDown bool    `json:"m_displayCountDown"`
}

func (m *clusterUIMessage) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return nil
	}
	type plain clusterUIMessage
	return json.Unmarshal(b, (*plain)(m))
}

type clusterSpawnSlot struct {
	Npcs []struct {
		Npc string `json:"npc"`
	} `json:"npcs"`
	MinPlayers int `json:"minPlayers"`
}

type clusterSpawning struct {
	NextSpawnTime      float64            `json:"nextSpawnTime"`
	NextSpawnCondition int                `json:"nextSpawnCondition"`
	Slots              []clusterSpawnSlot `json:"slots"`
}



type clusterDef struct {
	Pid             int64            `json:"pid"`
	Type            string           `json:"type"`
	Name            string           `json:"name"`
	Pos             []float32        `json:"pos"`
	Collider        *clusterCollider `json:"collider"`
	ConditionLogic  string           `json:"conditionLogic"`
	MinDelay        int              `json:"minDelay"`
	MaxDelay        int              `json:"maxDelay"`
	Conditions      []clusterCondDef `json:"conditions"`
	OnActive        []clusterActDef  `json:"onActive"`
	Completion      []clusterActDef  `json:"completion"`
	Spawning        *clusterSpawning `json:"spawning"`
	Duration        float64          `json:"duration"`
	Cutscene        *clusterRef      `json:"cutscene"`
	NpcTasks        []clusterActDef  `json:"npcTasks"`
	ClosePrevLesson bool             `json:"closePreviousLesson"`
	PreviousLesson  *clusterRef      `json:"previousLesson"`
	AutoRespawn bool        `json:"autoRespawn"`
	RespawnTime float64     `json:"respawnTime"`
	TeamID      int         `json:"teamId"`
	ToReplace   *clusterRef `json:"playerSpawnClusterToReplace"`
	Rot []float32 `json:"rot"`
}


func (e *clusterDef) SetPos(x, y, z float32) { e.Pos = []float32{x, y, z} }

func (e *clusterDef) SetRot(x, y, z, w float32) { e.Rot = []float32{x, y, z, w} }

func (e *clusterDef) SetCollider(c clusterCollider) { e.Collider = &c }

type levelObjectEntry struct {
	Pid     int64     `json:"pid"`
	Type    string    `json:"type"`
	GO      string    `json:"go"`
	Initial int       `json:"initial"`
	Pos     []float32 `json:"pos"`
}

func (e *levelObjectEntry) SetPos(x, y, z float32) { e.Pos = []float32{x, y, z} }

var (
	clustersOnce  sync.Once
	levelClusters map[string][]clusterDef

	levelObjectsOnce sync.Once
	levelObjects     map[string][]levelObjectEntry
)

type posSetter interface {
	SetPos(x, y, z float32)
}

type rotSetter interface {
	SetRot(x, y, z, w float32)
}

type colliderSetter interface {
	SetCollider(c clusterCollider)
}

type overrideFile struct {
	Moved   map[string][3]float32      `json:"moved"`
	Rotated map[string][4]float32      `json:"rotated"`
	Resized map[string]clusterCollider `json:"resized"`
	Added   []json.RawMessage          `json:"added"`
	Removed []int                      `json:"removed"`
}

// overridesPathFor maps "data/level_spawns.json" / "data/level_overrides/level_spawns.json".
func overridesPathFor(basePath string) string {
	return filepath.Join(filepath.Dir(basePath), "level_overrides", filepath.Base(basePath))
}

func applyOverrides[T any](basePath string, parsed map[string][]T) {
	overridePath := overridesPathFor(basePath)
	raw, err := os.ReadFile(overridePath)
	if err != nil {
		return
	}
	var overrides map[string]overrideFile
	if err := json.Unmarshal(raw, &overrides); err != nil {
		log.Printf("[HOST] %s parse error: %v", overridePath, err)
		return
	}
	applied := 0
	for level, ov := range overrides {
		level = strings.ToLower(level)
		entries := parsed[level]

		for idxStr, pos := range ov.Moved {
			idx, err := strconv.Atoi(idxStr)
			if err != nil || idx < 0 || idx >= len(entries) {
				log.Printf("[HOST] %s: moved index %q out of range for %s (len=%d)", overridePath, idxStr, level, len(entries))
				continue
			}
			if ps, ok := any(&entries[idx]).(posSetter); ok {
				ps.SetPos(pos[0], pos[1], pos[2])
				applied++
			}
		}

		for idxStr, rot := range ov.Rotated {
			idx, err := strconv.Atoi(idxStr)
			if err != nil || idx < 0 || idx >= len(entries) {
				log.Printf("[HOST] %s: rotated index %q out of range for %s (len=%d)", overridePath, idxStr, level, len(entries))
				continue
			}
			if rs, ok := any(&entries[idx]).(rotSetter); ok {
				rs.SetRot(rot[0], rot[1], rot[2], rot[3])
				applied++
			}
		}

		for idxStr, col := range ov.Resized {
			idx, err := strconv.Atoi(idxStr)
			if err != nil || idx < 0 || idx >= len(entries) {
				log.Printf("[HOST] %s: resized index %q out of range for %s (len=%d)", overridePath, idxStr, level, len(entries))
				continue
			}
			if cs, ok := any(&entries[idx]).(colliderSetter); ok {
				cs.SetCollider(col)
				applied++
			}
		}

		if len(ov.Removed) > 0 {
			skip := make(map[int]bool, len(ov.Removed))
			for _, i := range ov.Removed {
				skip[i] = true
			}
			kept := make([]T, 0, len(entries))
			for i, e := range entries {
				if !skip[i] {
					kept = append(kept, e)
				}
			}
			entries = kept
			applied += len(ov.Removed)
		}

		for _, rawEntry := range ov.Added {
			var e T
			if err := json.Unmarshal(rawEntry, &e); err != nil {
				log.Printf("[HOST] %s: bad added entry for %s: %v", overridePath, level, err)
				continue
			}
			entries = append(entries, e)
			applied++
		}

		parsed[level] = entries
	}
	if applied > 0 {
		log.Printf("[HOST] applied %d override(s) from %s", applied, overridePath)
	}
}

func loadLowercaseJSON[T any](path string, dst *map[string][]T) {
	*dst = map[string][]T{}
	raw, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[HOST] %s not loaded: %v", path, err)
		return
	}
	parsed := map[string][]T{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		log.Printf("[HOST] %s parse error: %v", path, err)
		return
	}
	for k, v := range parsed {
		(*dst)[strings.ToLower(k)] = v
	}
	applyOverrides(path, *dst)
}

func levelClusterDefs(level string) []clusterDef {
	clustersOnce.Do(func() { loadLowercaseJSON("data/level_clusters.json", &levelClusters) })
	return levelClusters[strings.ToLower(level)]
}


func reloadLevelClusterDefs() {
	clustersOnce = sync.Once{}
	levelClusters = nil
}

func levelObjectEntries(level string) []levelObjectEntry {
	levelObjectsOnce.Do(func() { loadLowercaseJSON("data/level_objects.json", &levelObjects) })
	return levelObjects[strings.ToLower(level)]
}


func levelObjectIndex(level string, pid int64) (int, bool) {
	for i, e := range levelObjectEntries(level) {
		if e.Pid == pid {
			return i, true
		}
	}
	return 0, false
}

type clusterPhase int

const (
	clusterSleeping clusterPhase = iota
	clusterAwake
	clusterActive
	clusterCompleted
)

const (
	condFallbackDelay = 20 * time.Second

	clusterKillFallback = 20 * time.Minute

	lessonFallback = 12 * time.Second
	// hard cap of live NPCs a single Cluster may spawn
	maxClusterSpawns = 8
	clusterExtraSpawnSpread = 2.0
	navBlockGrace   = 2 * time.Second
	navEscapeWindow = 1500 * time.Millisecond
)

type clusterRT struct {
	def          *clusterDef
	phase        clusterPhase
	awakeUntil   time.Time
	completeAt   time.Time
	blockedSince time.Time
	fired        map[int64]bool
	everDone     bool
	spawnedViews []int32
	nextSpawnAt  time.Time
	slotCursor   int
	lessonMover *spawnedNpc
}

type clusterMachine struct {
	level     string
	list      []*clusterRT
	byPid     map[int64]*clusterRT
	lastDebug time.Time
}

func (s *EnetServer) hostStartClusters(room *Room) {
	if room.clusters != nil {
		return
	}
	level := getCurScnFromProps(room.Name, room.Properties)
	defs := levelClusterDefs(level)
	if len(defs) == 0 {
		return
	}
	m := &clusterMachine{level: level, byPid: map[int64]*clusterRT{}}
	for i := range defs {
		rt := &clusterRT{def: &defs[i], fired: map[int64]bool{}}
		m.list = append(m.list, rt)
		m.byPid[defs[i].Pid] = rt
	}
	room.clusters = m
	log.Printf("[CLUSTER] machine started room=%s level=%s clusters=%d", room.Name, level, len(m.list))
}

func (s *EnetServer) hostTickClusters(room *Room) bool {
	m := room.clusters
	if m == nil {
		return false
	}
	now := time.Now()
	if now.Sub(m.lastDebug) >= 15*time.Second {
		m.lastDebug = now
		var census [4]int
		for _, rt := range m.list {
			census[rt.phase]++
		}
		for _, uid := range room.Players {
			if sess := s.sessionInRoom(uid, room.Name); sess != nil && sess.avatarBase != 0 {
				if pos, ok := playerStreamPos(room, sess.avatarBase); ok {
					log.Printf("[CLUSTER] heartbeat room=%s player@[%.1f %.1f %.1f] sleeping=%d awake=%d active=%d completed=%d",
						room.Name, pos[0], pos[1], pos[2], census[0], census[1], census[2], census[3])
				} else {
					log.Printf("[CLUSTER] heartbeat room=%s player pos UNKNOWN (view %d, %d tracked) sleeping=%d awake=%d active=%d completed=%d",
						room.Name, sess.avatarBase+1, len(room.playerPos), census[0], census[1], census[2], census[3])
				}
			}
		}
	}
	changed := false
	for _, rt := range m.list {
		switch rt.phase {
		case clusterSleeping:
			if s.clusterConditionsMet(room, m, rt, now) {
				delay := (rt.def.MinDelay + rt.def.MaxDelay) / 2
				rt.awakeUntil = now.Add(time.Duration(delay) * time.Second)
				rt.phase = clusterAwake
			}
		case clusterAwake:
			if !now.Before(rt.awakeUntil) {
				changed = s.clusterActivate(room, m, rt, now) || changed
			}
		case clusterActive:
			if rt.def.Type == "Cluster" {
				changed = s.clusterTickSpawner(room, m, rt, now) || changed
				if s.clusterWaveCleared(room, rt) {
					s.clusterComplete(room, m, rt, now)
					changed = true
				}
			}

			if rt.def.Type == "LessonCluster" && rt.lessonMover != nil &&
				!rt.lessonMover.moving && rt.lessonMover.pendingTaskAction == 0 {
				s.clusterComplete(room, m, rt, now)
			}
			if !rt.completeAt.IsZero() && !now.Before(rt.completeAt) {
				s.clusterComplete(room, m, rt, now)
			}
		}
	}
	return changed
}

func (s *EnetServer) clusterActivate(room *Room, m *clusterMachine, rt *clusterRT, now time.Time) bool {
	rt.phase = clusterActive
	log.Printf("[CLUSTER] %s %q active room=%s", rt.def.Type, rt.def.Name, room.Name)
	for i := range rt.def.OnActive {
		s.clusterRunAction(room, m, rt, &rt.def.OnActive[i], now)
	}
	changed := false
	switch rt.def.Type {
	case "Scene", "PlayerSpawnCluster":
		s.clusterComplete(room, m, rt, now)
	case "Cluster":
		changed = s.clusterTickSpawner(room, m, rt, now)
		rt.completeAt = now.Add(clusterKillFallback)
	case "LessonCluster":
		rt.completeAt = now.Add(lessonFallback)
		s.hostRunTutorSequence(room, m, rt)
		if rt.def.ClosePrevLesson && rt.def.PreviousLesson != nil {
			if prev := m.byPid[rt.def.PreviousLesson.Pid]; prev != nil && prev.phase != clusterCompleted {
				prev.phase = clusterCompleted
			}
		}
	case "TimerCluster":
		rt.completeAt = now.Add(time.Duration(rt.def.Duration * float64(time.Second)))
	case "CutsceneCluster":
		rt.completeAt = now.Add(time.Duration(rt.def.Duration * float64(time.Second)))
		if rt.def.Cutscene != nil && rt.def.Name != "0185_PlayCutScene_C" {
			room.cutsceneActive = true
			s.hostSendRPC(room, sessionInfoViewID, "RPC_PlayCutscene", []any{rt.def.Cutscene.Name})
			log.Printf("[CLUSTER] play cutscene %q (cluster %q) room=%s", rt.def.Cutscene.Name, rt.def.Name, room.Name)
		}
	}
	return changed
}

func (s *EnetServer) clusterComplete(room *Room, m *clusterMachine, rt *clusterRT, now time.Time) {
	rt.phase = clusterCompleted
	rt.everDone = true
	rt.completeAt = time.Time{}
	log.Printf("[CLUSTER] %s %q completed room=%s", rt.def.Type, rt.def.Name, room.Name)
	if rt.def.Type == "CutsceneCluster" && rt.def.Cutscene != nil && rt.def.Name != "0185_PlayCutScene_C" {
		room.cutsceneActive = false
		s.hostSendRPC(room, sessionInfoViewID, "RPC_StopCutscene", []any{rt.def.Cutscene.Name})
		if rt.def.Name == "0181_PlayCutScene_B" {
			s.hostTeleportPlayersToLanceTurretZone2(room)
		}
	}
	for i := range rt.def.Completion {
		s.clusterRunAction(room, m, rt, &rt.def.Completion[i], now)
	}
	s.clusterDestroyDamagedObjects(room, rt)
}

func activeCheckpointPos(room *Room, anchor [3]float32) (pos [3]float32, rot [4]float32, rotOK, ok bool) {
	if room.clusters == nil {
		return pos, rot, false, false
	}
	const quadrantRadius = 100 * 100
	completed := map[int64]*clusterRT{}
	for _, rt := range room.clusters.list {
		if rt.def.Type != "PlayerSpawnCluster" || rt.phase != clusterCompleted || len(rt.def.Pos) != 3 {
			continue
		}
		dx := rt.def.Pos[0] - anchor[0]
		dz := rt.def.Pos[2] - anchor[2]
		if dx*dx+dz*dz > quadrantRadius {
			continue
		}
		completed[rt.def.Pid] = rt
	}
	if len(completed) == 0 {
		return pos, rot, false, false
	}
	replaced := map[int64]bool{}
	for _, rt := range completed {
		if rt.def.ToReplace != nil {
			replaced[rt.def.ToReplace.Pid] = true
		}
	}
	for pid, rt := range completed {
		if !replaced[pid] {
			pos = [3]float32{rt.def.Pos[0], rt.def.Pos[1], rt.def.Pos[2]}
			if len(rt.def.Rot) == 4 {
				rot, rotOK = [4]float32{rt.def.Rot[0], rt.def.Rot[1], rt.def.Rot[2], rt.def.Rot[3]}, true
			}
			return pos, rot, rotOK, true
		}
	}
	return pos, rot, false, false
}


func nearestStaticIdx(room *Room, pos []float32, name string) int {
	best, bestDist := -1, float32(math.MaxFloat32)
	for j := range room.statics {
		if room.statics[j].goName != name {
			continue
		}
		d := float32(0)
		if len(pos) == 3 {
			dx := room.statics[j].pos[0] - pos[0]
			dz := room.statics[j].pos[2] - pos[2]
			d = dx*dx + dz*dz
		}
		if best < 0 || d < bestDist {
			bestDist = d
			best = j
		}
	}
	return best
}

func (s *EnetServer) clusterDestroyDamagedObjects(room *Room, rt *clusterRT) {
	for i := range rt.def.Conditions {
		c := &rt.def.Conditions[i]
		if c.Type != "ClusterCondition_ObjectsDamaged" || c.Object == nil || c.Object.Name == "" {
			continue
		}
		best := nearestStaticIdx(room, rt.def.Pos, c.Object.Name)
		if best < 0 {
			continue
		}
		ent := room.statics[best]
		attacker := int32(0)
		for _, uid := range room.Players {
			if sess := s.sessionInRoom(uid, room.Name); sess != nil && sess.avatarBase != 0 {
				attacker = sess.avatarBase
				break
			}
		}
		s.hostKillEntity(room, attacker, ent.viewBase, 9999, elementNone)
		room.statics = append(room.statics[:best], room.statics[best+1:]...)
		log.Printf("[CLUSTER] killed static %q view=%d (cluster %q) room=%s", ent.goName, ent.viewBase, rt.def.Name, room.Name)
	}
}


func (s *EnetServer) clusterConditionsMet(room *Room, m *clusterMachine, rt *clusterRT, now time.Time) bool {
	if len(rt.def.Conditions) == 0 {
		return rt.def.ConditionLogic != "OR"
	}
	and := rt.def.ConditionLogic != "OR"
	met, unobserved := and, false
	for i := range rt.def.Conditions {
		c := &rt.def.Conditions[i]
		ok, observable := s.clusterCondState(room, m, rt, c)
		if !observable {
			unobserved = true
			continue // resolved by the fallback clock below
		}
		if and {
			met = met && ok
		} else {
			met = met || ok
		}
	}
	if (met && unobserved && and) || (!met && !and && unobserved) {

		if !s.anyPlayerNear(room, rt.def, 30) {
			rt.blockedSince = time.Time{}
			return false
		}
		if rt.blockedSince.IsZero() {
			rt.blockedSince = now
			return false
		}
		if now.Sub(rt.blockedSince) >= condFallbackDelay {
			log.Printf("[CLUSTER] %q fired by unobservable-condition fallback room=%s", rt.def.Name, room.Name)
			return true
		}
		return false
	}
	rt.blockedSince = time.Time{}
	return met
}

func (s *EnetServer) clusterCondState(room *Room, m *clusterMachine, rt *clusterRT, c *clusterCondDef) (bool, bool) {
	switch c.Type {
	case "ClusterCondition_ClusterCompletion":
		if c.Cluster == nil || c.Cluster.Pid == 0 {
			return false, true
		}
		t := m.byPid[c.Cluster.Pid]
		if t == nil {
			return false, true
		}
		return t.phase == clusterCompleted || t.everDone, true
	case "ClusterCondition_ClusterNestAlerted":

		if c.Nest == nil || c.Nest.Pid == 0 {
			return false, true
		}
		t := m.byPid[c.Nest.Pid]
		return t != nil && t.phase >= clusterActive, true
	case "ClusterCondition_TriggerZone":
		if gamemodeName(room) == "GamemodeClaptrapSoccer" && strings.Contains(rt.def.Name, "Score") {
			return rt.fired[c.Pid], true
		}
		if rt.def.Collider == nil || len(rt.def.Pos) != 3 {
			return false, false
		}
		if s.anyPlayerInZone(room, rt.def) {
			return true, true
		}
		return false, false
	case "ClusterCondition_LevelTrigger":
		if rt.fired[c.Pid] {
			return true, true
		}
		if c.Trigger != nil {
			for _, obj := range levelObjectEntries(m.level) {
				isInteractLike := obj.Type == "ZoneTrigger" || obj.Type == "InteractiveTrigger" || obj.Type == "TreasureBoxTrigger"
				if obj.Pid != c.Trigger.Pid || !isInteractLike || len(obj.Pos) != 3 {
					continue
				}
				fake := &clusterDef{Pos: obj.Pos}
				if s.anyPlayerNear(room, fake, 4) {
					return true, true
				}
				return false, false
			}
		}
		return false, true
	case "ClusterCondition_PickupItem":
		return rt.fired[c.Pid], true
	case "ClusterCondition_ObjectsDamaged":
		if c.Object == nil || c.Object.Name == "" {
			return false, false
		}
		if best := nearestStaticIdx(room, rt.def.Pos, c.Object.Name); best >= 0 {
			if room.firesNear[room.statics[best].viewBase] >= 3 {
				return true, true
			}
		}
		return false, false
	case "ClusterCondition_RemainingAmmo":
		for _, uid := range room.Players {
			if sess := s.sessionInRoom(uid, room.Name); sess != nil && sess.ammoEst <= c.Count {
				return true, true
			}
		}
		return false, false
	case "ClusterCondition_RemainingNpc":
		if c.Cluster == nil || c.Cluster.Pid == 0 {
			return false, true
		}
		t := m.byPid[c.Cluster.Pid]
		if t == nil {
			return false, true
		}
		alive := 0
		for _, vb := range t.spawnedViews {
			for i := range room.npcs {
				if room.npcs[i].viewBase == vb && room.npcs[i].hp > 0 {
					alive++
					break
				}
			}
		}
		return alive <= c.RemainingNpc, true
	case "ClusterCondition_AllPlayerDead":
		for _, uid := range room.Players {
			if sess := s.sessionInRoom(uid, room.Name); sess != nil && sess.hostChar != nil && !sess.isDead {
				return false, true
			}
		}
		return true, true
	case "ClusterCondition_NpcStatus":
		if !c.CheckPhase {
			return false, false
		}
		return room.bossPhase >= c.Phase, true
	case "ClusterCondition_ScoreReached":
		return false, true
	default:
		return false, false
	}
}

func (s *EnetServer) anyPlayerInZone(room *Room, def *clusterDef) bool {
	c := def.Collider
	cx, cy, cz := def.Pos[0], def.Pos[1], def.Pos[2]
	if len(c.Center) == 3 {
		cx, cy, cz = cx+c.Center[0], cy+c.Center[1], cz+c.Center[2]
	}

	hx, hy, hz := float32(8), float32(10), float32(8)
	if len(c.Size) == 3 {
		hx = maxf(c.Size[0]/2, hx)
		hy = maxf(c.Size[1]/2, hy)
		hz = maxf(c.Size[2]/2, hz)
	} else if c.Radius > 0 {
		hx = maxf(c.Radius, hx)
		hy = maxf(c.Radius, hy)
		hz = maxf(c.Radius, hz)
	}
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.avatarBase == 0 {
			continue
		}
		pos, ok := playerStreamPos(room, sess.avatarBase)
		if !ok {
			continue
		}
		dx, dy, dz := pos[0]-cx, pos[1]-cy, pos[2]-cz
		if dx >= -hx && dx <= hx && dy >= -hy && dy <= hy && dz >= -hz && dz <= hz {
			room.playerPos[sess.avatarBase+1] = [3]float32{
				(pos[0] + cx) / 2, pos[1], (pos[2] + cz) / 2,
			}
			return true
		}
	}
	return false
}

func (s *EnetServer) anyPlayerNear(room *Room, def *clusterDef, radius float32) bool {
	if len(def.Pos) != 3 {
		return false
	}
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.avatarBase == 0 {
			continue
		}
		pos, ok := playerStreamPos(room, sess.avatarBase)
		if !ok {
			continue
		}
		dx, dz := pos[0]-def.Pos[0], pos[2]-def.Pos[2]
		if dx*dx+dz*dz <= radius*radius {
			return true
		}
	}
	return false
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func clampf32(dy, maxStep float32) float32 {
	if dy > maxStep {
		return maxStep
	}
	if dy < -maxStep {
		return -maxStep
	}
	return dy
}

func playerStreamPos(room *Room, avatarBase int32) ([3]float32, bool) {
	pos, ok := room.playerPos[avatarBase+1]
	return pos, ok
}

const controllerSyncViewID = hostViewBase + 4


func movementSpeed(flags byte) float64 {

	const (
		runSpeed    = 4.0
		sprintSpeed = 5.1
		walkSpeed   = 2.0
		crouchSpeed = 1.5
	)

	switch {

	case flags&2 != 0:
		return 1.0

	case flags&64 != 0:
		return crouchSpeed

	case flags&128 != 0:
		return walkSpeed

	case flags&1 != 0:
		return sprintSpeed

	default:
		return runSpeed
	}
}

// modifier for move speed based on wep type
func weaponMoveModifier(weaponType int32, flags byte) float64 {
	// Run   Sprint Walk  Crouch
	table := [5][4]float64{
		{0.95, 0.95, 0.95, 0.95}, // SMG
		{0.85, 0.90, 0.85, 0.85}, // SniperRifle
		{0.85, 0.90, 0.85, 0.85}, // Shotguns
		{1.00, 1.00, 1.00, 1.00}, // RepeaterPistol
		{0.90, 0.90, 0.90, 0.90}, // CombatRifle
	}
	if weaponType < 0 || int(weaponType) >= len(table) {
		return 1.0
	}
	row := table[weaponType]
	switch {
	case flags&1 != 0:
		return row[1]
	case flags&(4|8|16|32) != 0:
		if flags&128 != 0 {
			return row[2]
		}
		return row[0]
	case flags&64 != 0:
		return row[3]
	}
	return 1.0
}


func (sess *ClientSession) currentWeaponType() int32 {
	if int(sess.activeWeaponSlot) < len(sess.weaponTypes) {
		return sess.weaponTypes[sess.activeWeaponSlot]
	}
	return -1
}


const fixedDelta = 1.0 / 30.0

func (s *EnetServer) applyInputFrames(room *Room, sess *ClientSession, data []byte) {

	const frameLen = 9

	if sess.avatarBase == 0 {
		return
	}

	key := sess.avatarBase + 1

	pos, ok := room.playerPos[key]
	if !ok {
		return
	}

	level := m2level(room)
	hasMesh := levelHasNavmesh(level)
	now := time.Now()

	for off := 0; off+frameLen <= len(data); off += frameLen {

		flags := data[off]

		yawDeg := float64(binary.LittleEndian.Uint16(data[off+1:])) / 100

		pitch := int16(binary.LittleEndian.Uint16(data[off+3:]))

		frameNum := int32(binary.LittleEndian.Uint32(data[off+5:]))

		if frameNum != 0 && sess.lastInputFrame != 0 && frameNum <= sess.lastInputFrame {
			if frameNum > sess.lastInputFrame-60 {
				continue
			}
			log.Printf("[MOVE] actor=%d input frame counter reset (%d → %d), resyncing", room.ActorNrFor(sess.userID), sess.lastInputFrame, frameNum)
			sess.lastInputFrame = 0
		}
		gapFrames := 1.0
		if sess.lastInputFrame != 0 && frameNum > sess.lastInputFrame+1 {
			if gap := frameNum - sess.lastInputFrame; gap <= 25 {
				gapFrames = float64(gap)
			}
		}

		if flags != 0 || yawDeg != sess.lastYawDeg || pitch != sess.lastPitch16 {
			sess.lastActivityAt = time.Now()
		}

		sess.lastInputFrame = frameNum
		sess.lastYawDeg = yawDeg
		sess.lastPitch16 = pitch

		var moveFlg byte

		moveFlg |= flags & (1 | 2 | 64 | 128)

		if flags&(4|8|16|32) != 0 {
			moveFlg |= 8
		}

		sess.lastMoveFlg = moveFlg

		// GetMovementDirection()

		var x, z float64

		if flags&16 != 0 {
			x--
		}

		if flags&32 != 0 {
			x++
		}

		if flags&4 != 0 {
			z++
		}

		if flags&8 != 0 {
			z--
		}

		if x == 0 && z == 0 {
			continue
		}

		// normalized

		n := math.Hypot(x, z)

		x /= n
		z /= n

		// transform.TransformDirection()

		yaw := yawDeg * math.Pi / 180

		sin := math.Sin(yaw)
		cos := math.Cos(yaw)

		worldX := cos*x + sin*z
		worldZ := -sin*x + cos*z

		// speed

		speed := movementSpeed(flags) * weaponMoveModifier(sess.currentWeaponType(), flags)

		velX := worldX * speed
		velZ := worldZ * speed

		// CharacterMotor.Move(dt)

		newX := pos[0] + float32(velX*fixedDelta*gapFrames)
		newZ := pos[2] + float32(velZ*fixedDelta*gapFrames)

		if !hasMesh {
			pos[0], pos[2] = newX, newZ
			// if there's no navmesh for the level, groundHints nearest static spawn point is the only
			// ground available. much better than Y staying frozen at spawn for the entire session
			if gy, ok := groundHint(level, newX, newZ, pos[1]); ok {
				pos[1] = gy
			}
			continue
		}
		if onMesh := navmeshOnMesh(level, newX, newZ, pos[1]); onMesh {
			pos[0], pos[2] = newX, newZ
			if gy, ok := navmeshGroundHeight(level, newX, newZ, pos[1]); ok {
				pos[1] = gy
			}
			sess.navBlockedSince = time.Time{}
			continue
		}
		if sess.navBlockedSince.IsZero() {
			sess.navBlockedSince = now
			continue
		}
		blockedFor := now.Sub(sess.navBlockedSince)
		switch {
		case blockedFor < navBlockGrace:
		case blockedFor < navBlockGrace+navEscapeWindow:
			pos[0], pos[2] = newX, newZ
			if gy, ok := groundHint(level, newX, newZ, pos[1]); ok {
				pos[1] = gy
			}
		default:
			sess.navBlockedSince = now
			log.Printf("[MOVE] actor=%d wall-pinned at [%.1f %.1f %.1f] (mesh rejects every step)", room.ActorNrFor(sess.userID), pos[0], pos[1], pos[2])
		}
	}

	room.playerPos[key] = pos
	sess.broadcastPos = pos

	if now.Sub(sess.lastMoveLog) > 2*time.Second {

		sess.lastMoveLog = now

		log.Printf(
			"[MOVE] actor=%d pos=[%.2f %.2f %.2f] frame=%d",
			room.ActorNrFor(sess.userID),
			pos[0],
			pos[1],
			pos[2],
			sess.lastInputFrame,
		)
	}
}

func decodeVector3(v any) ([3]float32, bool) {
	c, ok := v.(Custom)
	if !ok || c.Code != 86 || len(c.Data) != 12 {
		return [3]float32{}, false
	}
	return [3]float32{
		math.Float32frombits(binary.BigEndian.Uint32(c.Data[0:])),
		math.Float32frombits(binary.BigEndian.Uint32(c.Data[4:])),
		math.Float32frombits(binary.BigEndian.Uint32(c.Data[8:])),
	}, true
}

func asFloat32(v any) (float32, bool) {
	switch n := v.(type) {
	case float32:
		return n, true
	case float64:
		return float32(n), true
	}
	return 0, false
}

func (s *EnetServer) applyRealPosition(room *Room, sess *ClientSession, items []any) {
	if sess.avatarBase == 0 {
		return
	}
	posIdx := -1
	var pos [3]float32
	for i, item := range items {
		if p, ok := decodeVector3(item); ok {
			pos, posIdx = p, i
			break
		}
	}
	if posIdx == -1 {
		return
	}
	sess.lastActivityAt = time.Now()

	haveReal := false
	if len(items) >= posIdx+4 {
		if flags, ok := items[posIdx+1].(byte); ok {
			if yaw, ok := asFloat32(items[posIdx+2]); ok {
				if pitch, ok := asFloat32(items[posIdx+3]); ok {
					sess.lastMoveFlg = flags
					sess.lastYawDeg = float64(yaw)
					sess.lastPitch16 = int16(pitch * 100)
					haveReal = true
					if !sess.realActionsSeen {
						sess.realActionsSeen = true
						log.Printf("[MOVE] actor=%d user=%s sending real flags/yaw/pitch (patched client) flags=%08b yaw=%.1f pitch=%.1f", room.ActorNrFor(sess.userID), sess.userID, flags, yaw, pitch)
					}
				}
			}
		}
	}

	if !haveReal {
		if prev, ok := room.playerPos[sess.avatarBase+1]; ok {
			dx := float64(pos[0] - prev[0])
			dz := float64(pos[2] - prev[2])
			dy := float64(pos[1] - prev[1])
			var moveFlg byte
			if moved := math.Hypot(dx, dz); moved > 0.05 {
				sess.lastYawDeg = math.Atan2(dx, dz) * 180 / math.Pi
				moveFlg |= 8
			}
			if dy > 0.15 {
				moveFlg |= 2
			}
			sess.lastMoveFlg = moveFlg
		}
	}

	room.playerPos[sess.avatarBase+1] = pos
	sess.broadcastPos = pos
	if !sess.realPosSeen {
		sess.realPosSeen = true
		log.Printf("[MOVE] actor=%d user=%s sending a real position pos=[%.2f %.2f %.2f]", room.ActorNrFor(sess.userID), sess.userID, pos[0], pos[1], pos[2])
	}
}

func (s *EnetServer) hostClusterLevelTrigger(room *Room, levelObjectID int) {
	m := room.clusters
	if m == nil {
		return
	}
	objs := levelObjectEntries(m.level)
	if levelObjectID < 0 || levelObjectID >= len(objs) {
		log.Printf("[CLUSTER] level trigger id=%d out of extracted range (%d) room=%s", levelObjectID, len(objs), room.Name)
		return
	}
	pid := objs[levelObjectID].Pid
	log.Printf("[CLUSTER] level trigger id=%d → %q room=%s", levelObjectID, objs[levelObjectID].GO, room.Name)
	for _, rt := range m.list {
		if rt.phase == clusterCompleted {
			continue
		}
		for i := range rt.def.Conditions {
			c := &rt.def.Conditions[i]
			if c.Type == "ClusterCondition_LevelTrigger" && c.Trigger != nil && c.Trigger.Pid == pid {
				rt.fired[c.Pid] = true
			}
		}
	}
}

func (s *EnetServer) hostClusterPickup(room *Room) {
	m := room.clusters
	if m == nil {
		return
	}
	for _, rt := range m.list {
		if rt.phase == clusterCompleted {
			continue
		}
		for i := range rt.def.Conditions {
			c := &rt.def.Conditions[i]
			if c.Type == "ClusterCondition_PickupItem" {
				rt.fired[c.Pid] = true
			}
		}
	}
}


func (s *EnetServer) clusterRunAction(room *Room, m *clusterMachine, rt *clusterRT, a *clusterActDef, now time.Time) {
	switch a.Type {
	case "ClusterAction_LevelObjectActivation":
		if a.LevelObject == nil {
			return
		}
		idx, ok := levelObjectIndex(m.level, a.LevelObject.Pid)
		if !ok {
			log.Printf("[CLUSTER] no level-object index for %q (pid %d)", a.LevelObject.Name, a.LevelObject.Pid)
			return
		}
		if room.levelObjectStates == nil {
			room.levelObjectStates = map[int]bool{}
		}
		room.levelObjectStates[idx] = a.Activate == 1
		s.hostSpawnLevelObjectManager(room)
		s.hostSendLevelObjectState(room)
		log.Printf("[CLUSTER] level object %q id=%d activate=%d", a.LevelObject.Name, idx, a.Activate)
	case "ClusterAction_SetPlayerActions":
		if s.TutorialFreeActions && gamemodeName(room) == "GamemodeTutorial" {
			log.Printf("[CLUSTER] SetPlayerActions(%d) skipped, tutorial_free_actions (cluster %q) room=%s", a.Actions, rt.def.Name, room.Name)
			return
		}
		room.lastPlayerActions = a.Actions
		room.actionsGranted = true
		s.hostSendRPC(room, sessionInfoViewID, "RPC_RequestSetPlayerActions", []any{a.Actions})
		room.actionGrantAt = append(room.actionGrantAt, now.Add(2*time.Second), now.Add(6*time.Second))
	case "ClusterAction_DisplayEchoMessage":
		s.hostSendRPC(room, sessionInfoViewID, "RPC_RequestDisplayEchoMessage", []any{a.EchoID})
	case "ClusterAction_ObjectiveAdd":
		s.hostSendRPC(room, sessionInfoViewID, "RPC_AddObjectiveToClient", []any{a.ObjectiveID, a.TitleID, a.DetailID})
	case "ClusterAction_ObjectiveRemove":
		s.hostSendRPC(room, sessionInfoViewID, "RPC_RemoveObjectiveFromClient", []any{a.ObjectiveID, a.IsSuccess})
	case "ClusterAction_GiveAmmo":
		for _, uid := range room.Players {
			if sess := s.sessionInRoom(uid, room.Name); sess != nil && sess.avatarBase != 0 {
				s.hostSendRPC(room, sess.avatarBase+3, "RPC_OnReceiveAmmos", []any{ammoPouchesForSession(sess, true)})
				sess.ammoEst = 360
				log.Printf("[CLUSTER] GiveAmmo → refill sent to view=%d (cluster %q)", sess.avatarBase+3, rt.def.Name)
			}
		}
	case "ClusterAction_TimerControl":
		if a.Timer == nil {
			return
		}
		if t := m.byPid[a.Timer.Pid]; t != nil {
			delay := (t.def.MinDelay + t.def.MaxDelay) / 2
			t.phase = clusterAwake
			t.awakeUntil = now.Add(time.Duration(delay) * time.Second)
		}
	case "ClusterAction_KillAllNpcs":
		s.clusterStop(room, m, a.Cluster, true)
	case "ClusterAction_StopCluster":
		s.clusterStop(room, m, a.StopCluster, false)
	case "ClusterAction_SendToTown":
		s.hostSendRPC(room, sessionInfoViewID, "RPC_SendPlayerToTown", []any{})
	case "ClusterAction_MissionTimerControl":
		s.hostMissionTimerControl(room, a, rt.def.Name)
	case "ClusterAction_ResetTutorTask":
		s.hostRunTutorTask(room, m, rt, a)
	case "ClusterAction_ValidateQuestStep":
		s.hostValidateQuestStep(room, a.QuestID, a.StepID, rt.def.Name)
	case "ClusterAction_ShowUIMessage":
		if a.Message == nil {
			log.Printf("[CLUSTER] ShowUIMessage with no message payload (cluster %q), re-run tools/extract_uimessages.py?", rt.def.Name)
			return
		}
		s.hostSendRPC(room, sessionInfoViewID, "RPC_RequestDisplayUIMessage", []any{
			a.Message.DisplayCountDown,
			float32(0),
			float32(a.Message.Duration),
			a.Message.Text,
			byte(a.Message.Type),
		})
		log.Printf("[CLUSTER] UI message %q dur=%.0fs countdown=%v (cluster %q) room=%s",
			a.Message.Text, a.Message.Duration, a.Message.DisplayCountDown, rt.def.Name, room.Name)
	case "ClusterAction_ShakeCamera":
		s.hostSendRPC(room, sessionInfoViewID, "RPC_RequestShakeCamera", []any{int32(a.Intensity)})
		log.Printf("[CLUSTER] camera shake intensity=%d (cluster %q) room=%s", a.Intensity, rt.def.Name, room.Name)
	case "ClusterAction_NextMap":
		s.hostNextMap(room, rt.def.Name)
	case "ClusterAction_SetNavMeshLayer":
		if a.NavmeshLayer == "" {
			return
		}
		if room.navLayers == nil {
			room.navLayers = map[string]bool{}
		}
		enabled := a.SetAction == 0
		room.navLayers[a.NavmeshLayer] = enabled
		log.Printf("[CLUSTER] navmesh layer %q enabled=%v (cluster %q) room=%s", a.NavmeshLayer, enabled, rt.def.Name, room.Name)
	case "ClusterAction_ActivateBossNextPhase":
		room.bossPhase++
		log.Printf("[CLUSTER] boss phase → %d (cluster %q) room=%s", room.bossPhase, rt.def.Name, room.Name)
	case "ClusterAction_CompleteInstance":
		playlistID := asInt32(room.Properties["PlaylistId"])
		for _, uid := range room.Players {
			psess := s.sessionInRoom(uid, room.Name)
			if psess == nil || psess.hostChar == nil {
				continue
			}
			consumeEnergyForInstance(room, psess.hostChar.CharID)
			s.grantInstanceRewards(room, psess)
			s.hostAdvanceInstanceQuestTasks(psess, psess.hostChar.CharID, playlistID)
		}
		log.Printf("[CLUSTER] instance complete (cluster %q) room=%s", rt.def.Name, room.Name)
	case "ClusterAction_RespawnPlayers":
		for actorNr, uid := range room.Players {
			psess := s.sessionInRoom(uid, room.Name)
			if psess == nil || (a.OnlyRespawnDeadPlayer && !psess.isDead) {
				continue
			}
			s.hostSpawnAvatar(room, psess, actorNr, nil)
			psess.playerState = PlayerStateSpawned
		}
		s.hostSendSerialize(room)
	case "ClusterAction_EndRound":
		if gamemodeName(room) == "GamemodeClaptrapSoccer" {
			isFinal := room.claptrapTeamScore[room.claptrapLastScoreTeam] >= s.claptrapTargetScore()
			s.hostSendClaptrapRoundResult(room, isFinal)
		}
		log.Printf("[CLUSTER] round ended (cluster %q) room=%s", rt.def.Name, room.Name)
	case "ClusterAction_NextRound":
		for _, pid := range a.ResetSceneList {
			if t := m.byPid[pid]; t != nil {
				t.phase = clusterSleeping
				t.everDone = false
				t.fired = map[int64]bool{}
				t.blockedSince = time.Time{}
			}
		}
		if a.Timer != nil {
			if t := m.byPid[a.Timer.Pid]; t != nil {
				delay := (t.def.MinDelay + t.def.MaxDelay) / 2
				t.phase = clusterAwake
				t.awakeUntil = now.Add(time.Duration(delay) * time.Second)
			}
		}
		log.Printf("[CLUSTER] next round targetScore=%d totals=%v room=%s", a.TargetScore, room.claptrapTeamScore, room.Name)

		s.hostSendRPC(room, sessionInfoViewID, "RPC_CloseRoundResultToClient", []any{})
		if a.TargetScore > 0 && !room.pvpMatchEnded {
			for team, score := range room.claptrapTeamScore {
				if score >= int32(a.TargetScore) {
					room.pvpMatchEnded = true
					log.Printf("[CLUSTER] match won by team=%d score=%d (target=%d) room=%s", team, score, a.TargetScore, room.Name)
					s.hostEndPvPMatchToWrapup(room)
					break
				}
			}
		}
	default:
		log.Printf("[CLUSTER] action %s not implemented (cluster %q)", a.Type, rt.def.Name)
	}
}

func (s *EnetServer) hostMissionTimerControl(room *Room, a *clusterActDef, clusterName string) {
	if int32(a.Difficulty) != asInt32(room.Properties["Difficulty"]) {
		return
	}
	now := float64(msNow()) / 1000
	switch a.TimerAction {
	case 4:
		room.mtActive, room.mtRunning, room.mtTimedOut = true, true, false
		room.mtStart, room.mtDuration, room.mtPause = now, a.ExtendedDuration, 0
	case 0:
		if !room.mtActive || !room.mtRunning {
			return
		}
		room.mtRunning, room.mtPause = false, now
		s.hostSendRPC(room, sessionInfoViewID, "RPC_SendMissionTimerEvent", []any{int32(1), Double(0)})
	case 1: // Resume
		if !room.mtActive || room.mtRunning {
			return
		}
		room.mtDuration -= room.mtPause - room.mtStart
		room.mtStart, room.mtRunning = now, true
	case 2:
		if !room.mtActive {
			return
		}
		room.mtStart, room.mtPause = now, now
	case 3:
		if !room.mtActive {
			return
		}
		room.mtDuration += a.ExtendedDuration
		s.hostSendRPC(room, sessionInfoViewID, "RPC_SendMissionTimerEvent", []any{int32(0), Double(a.ExtendedDuration)})
	default:
		log.Printf("[CLUSTER] MissionTimerControl: unknown action %d (cluster %q)", a.TimerAction, clusterName)
		return
	}
	s.hostSyncMissionTimer(room, nil)
	log.Printf("[CLUSTER] mission timer action=%d duration=%.0fs running=%v (cluster %q) room=%s",
		a.TimerAction, room.mtDuration, room.mtRunning, clusterName, room.Name)
}

func (s *EnetServer) hostSyncMissionTimer(room *Room, sess *ClientSession) {
	args := []any{room.mtRunning, Double(room.mtStart), Double(room.mtDuration), Double(room.mtPause)}
	if sess != nil {
		s.hostSendRPCTo(sess, sessionInfoViewID, "RPC_SendSyncMissionTimer", args)
		return
	}
	s.hostSendRPC(room, sessionInfoViewID, "RPC_SendSyncMissionTimer", args)
}

func (s *EnetServer) hostTickMissionTimer(room *Room) {
	if !room.mtActive || !room.mtRunning || room.mtTimedOut {
		return
	}
	if float64(msNow())/1000 < room.mtStart+room.mtDuration {
		return
	}
	room.mtTimedOut, room.mtRunning = true, false
	room.mtPause = room.mtStart + room.mtDuration
	s.hostSendRPC(room, sessionInfoViewID, "RPC_SendMissionTimerEvent", []any{int32(2), Double(0)})
	s.hostSyncMissionTimer(room, nil)
	// give player a few seconds to actually see the failed HUD event
	room.mtSendToTownAt = time.Now().Add(4 * time.Second)
	log.Printf("[CLUSTER] mission timer TIMEOUT room=%s, sending to Town in 4s (objective not completed)", room.Name)
}

// fires hostTickMissionTimers scheduled objective not complete trip to town
// reuses the same ClusterAction_SendToTown wire call RPC_SendPlayerToTown broadcast to whole room
// matching how level scripts already send everyone home on other triggers
func (s *EnetServer) hostRunMissionTimerFail(room *Room) {
	if room.mtSendToTownAt.IsZero() || time.Now().Before(room.mtSendToTownAt) {
		return
	}
	room.mtSendToTownAt = time.Time{}
	log.Printf("[CLUSTER] mission timer failed, sending room=%s to Town", room.Name)
	s.hostSendRPC(room, sessionInfoViewID, "RPC_SendPlayerToTown", []any{})
}

const (
	defaultPvPMatchDuration = 10 * time.Minute
	defaultPvPKillTarget    = 50
)

// how long a PvP match runs before auto ending From config/server.json game.pvp.match_duration_seconds
func (s *EnetServer) pvpMatchDuration() time.Duration {
	if s.PvPMatchDurationSeconds <= 0 {
		return defaultPvPMatchDuration
	}
	return time.Duration(s.PvPMatchDurationSeconds) * time.Second
}

func (s *EnetServer) pvpMatchTimeLimit(room *Room) int32 {
	remaining := s.pvpMatchDuration()
	if !room.pvpMatchEndAt.IsZero() {
		remaining = time.Until(room.pvpMatchEndAt)
		if remaining < 0 {
			remaining = 0
		}
	}
	return int32(time.Since(room.CreatedAt).Seconds()) + int32(remaining.Seconds())
}

func (s *EnetServer) hostTickPvPMatchTimer(room *Room) {
	if room.pvpMatchEnded {
		return
	}
	switch gamemodeName(room) {
	case "GameModePvP", "GamemodeTeamDeathmatch", "GamemodeClaptrapSoccer":
	default:
		return
	}
	if room.pvpMatchEndAt.IsZero() {
		room.pvpMatchEndAt = time.Now().Add(s.pvpMatchDuration())
		return
	}
	if time.Now().Before(room.pvpMatchEndAt) {
		return
	}
	room.pvpMatchEnded = true
	log.Printf("[CLUSTER] PvP match ended (time limit), sending room=%s to Town", room.Name)
	s.hostFinishPvPMatch(room)
}

func (s *EnetServer) pvpKillTarget() int32 {
	if s.PvPKillTarget <= 0 {
		return defaultPvPKillTarget
	}
	return s.PvPKillTarget
}

func (s *EnetServer) hostCheckPvPKillTarget(room *Room) {
	if room.pvpMatchEnded {
		return
	}
	switch gamemodeName(room) {
	case "GameModePvP", "GamemodeTeamDeathmatch":
	default:
		return
	}
	target := s.pvpKillTarget()
	if isTeamGameMode(asInt32(room.Properties["GameMode"])) {
		teamKills := map[byte]int32{}
		for _, uid := range room.Players {
			if sess := s.sessionInRoom(uid, room.Name); sess != nil {
				teamKills[sess.teamID] += sess.pvpKillCount
			}
		}
		for team, kills := range teamKills {
			if kills >= target {
				room.pvpMatchEnded = true
				log.Printf("[CLUSTER] PvP match won by team=%d kills=%d (target=%d) room=%s", team, kills, target, room.Name)
				s.hostFinishPvPMatch(room)
				return
			}
		}
		return
	}
	for _, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil || sess.pvpKillCount < target {
			continue
		}
		room.pvpMatchEnded = true
		log.Printf("[CLUSTER] PvP match won by user=%s kills=%d (target=%d) room=%s", sess.userID, sess.pvpKillCount, target, room.Name)
		s.hostFinishPvPMatch(room)
		return
	}
}

func (s *EnetServer) hostFinishPvPMatch(room *Room) {
	for _, uid := range room.Players {
		if psess := s.sessionInRoom(uid, room.Name); psess != nil && psess.hostChar != nil {
			s.hostAdvancePvPQuestTasks(psess, psess.hostChar.CharID)
		}
	}
	// MUST run BEFORE the GameMode flip below, not after
	s.hostEndPvPMatchToWrapup(room)

	s.hostRecordPvPMatchResults(room)

	room.Properties["GameMode"] = gameModeNumberByName["GameModeTown"]
}

func (s *EnetServer) hostEndPvPMatchToWrapup(room *Room) {
	payload := s.sessionStatsPayload(room)
	for _, uid := range room.Players {
		if psess := s.sessionInRoom(uid, room.Name); psess != nil {
			s.hostSendRPCTo(psess, sessionInfoViewID, "RPC_RequestSendSessionStats", []any{payload})
			s.hostSendWrapupDestination(room, psess)
		}
	}
}

func (s *EnetServer) clusterStop(room *Room, m *clusterMachine, ref *clusterRef, kill bool) {
	if ref == nil {
		return
	}
	t := m.byPid[ref.Pid]
	if t == nil {
		return
	}
	t.phase = clusterCompleted
	t.completeAt = time.Time{}
	if !kill {
		return
	}
	doomed := map[int32]bool{}
	for _, vb := range t.spawnedViews {
		doomed[vb] = true
		s.hostBroadcastJournaled(room, 204, map[byte]any{
			ParamPlayerNr:  HostActorNr,
			ParamEventData: map[any]any{byte(0): vb},
		}, true)
	}
	t.spawnedViews = nil
	kept := room.npcs[:0]
	for _, npc := range room.npcs {
		if !doomed[npc.viewBase] {
			kept = append(kept, npc)
		}
	}
	room.npcs = kept
	log.Printf("[CLUSTER] killed %d NPCs of %q room=%s", len(doomed), ref.Name, room.Name)
}

func nearestTutor(room *Room, pos []float32) *spawnedNpc {
	if len(pos) != 3 {
		return nil
	}
	best := -1
	bestDist := float32(math.MaxFloat32)
	for i := range room.npcs {
		if room.npcs[i].kit != "ClapTrapTutor" || room.npcs[i].hp <= 0 {
			continue
		}
		dx, dz := room.npcs[i].pos[0]-pos[0], room.npcs[i].pos[2]-pos[2]
		if d := dx*dx + dz*dz; d < bestDist {
			bestDist = d
			best = i
		}
	}
	if best < 0 || bestDist > 200*200 {
		return nil
	}
	return &room.npcs[best]
}

func nearestClusterPosByName(m *clusterMachine, name string, anchor, fallback []float32) []float32 {
	best := fallback
	bestDist := float32(math.MaxFloat32)
	for _, rt := range m.list {
		if rt.def.Name != name || len(rt.def.Pos) != 3 {
			continue
		}
		if len(anchor) != 3 {
			return rt.def.Pos
		}
		dx, dz := rt.def.Pos[0]-anchor[0], rt.def.Pos[2]-anchor[2]
		if d := dx*dx + dz*dz; d < bestDist {
			bestDist = d
			best = rt.def.Pos
		}
	}
	return best
}

func (s *EnetServer) hostRunTutorSequence(room *Room, m *clusterMachine, rt *clusterRT) {
	var mover *spawnedNpc
	for i := range rt.def.NpcTasks {
		a := &rt.def.NpcTasks[i]
		if a.Type != "ClusterAction_ResetTutorTask" {
			continue
		}
		if mover == nil {
			mover = s.hostRunTutorTask(room, m, rt, a)
			continue
		}
		if a.TaskType == 4 {
			mover.pendingTaskAction = int32(a.TutorAction)
		}
	}
	rt.lessonMover = mover
}


func (s *EnetServer) hostRunTutorTask(room *Room, m *clusterMachine, rt *clusterRT, a *clusterActDef) *spawnedNpc {
	switch a.TaskType {
	case 1:
		dest, destName := rt.def.Pos, rt.def.Name
		if a.MoveTo != nil && a.MoveTo.Name != "" {
			dest, destName = nearestClusterPosByName(m, a.MoveTo.Name, rt.def.Pos, dest), a.MoveTo.Name
		}
		npc := nearestTutor(room, dest)
		if npc == nil {
			return nil
		}
		speed := int32(a.MoveSpeed)
		if speed != 0 && speed != 2 {
			speed = 0
		}
		if wps, ok := navmeshFindPath(m.level, npc.pos[0], npc.pos[2], dest[0], dest[2], npc.pos[1]); ok {
			npc.path = wps
		} else {
			npc.path = [][3]float32{{dest[0], dest[1], dest[2]}}
		}
		npc.moving = true
		npc.pendingTaskAction = 0
		npcSetTask(npc, bhvTaskMoveTo, -1, [3]float32{dest[0], dest[1], dest[2]}, speed)
		log.Printf("[CLUSTER] tutor %d walking to %q @[%.1f %.1f %.1f] via %d waypoint(s) (cluster %q)",
			npc.viewBase, destName, dest[0], dest[1], dest[2], len(npc.path), rt.def.Name)
		return npc
	case 2, 3:
		npc := nearestTutor(room, rt.def.Pos)
		if npc == nil {
			return nil
		}
		npc.moving, npc.path = false, nil
		npcSetTask(npc, bhvTaskIdle, -1, npc.pos, -1)
		return npc
	case 4:
		npc := nearestTutor(room, rt.def.Pos)
		if npc == nil {
			return nil
		}
		npc.moving, npc.path = false, nil
		npcSetTutorialAction(npc, int32(a.TutorAction))
		log.Printf("[CLUSTER] tutor %d action=%d (cluster %q)", npc.viewBase, a.TutorAction, rt.def.Name)
		return npc
	}
	return nil
}


func (s *EnetServer) hostTickNpcMovement(room *Room, dt float64) bool {
	moved := false
	for i := range room.npcs {
		npc := &room.npcs[i]
		if !npc.moving {
			continue
		}
		if len(npc.path) == 0 {
			npc.moving = false
			continue
		}
		waypoint := npc.path[0]
		dx := float64(waypoint[0] - npc.pos[0])
		dz := float64(waypoint[2] - npc.pos[2])
		dist := math.Sqrt(dx*dx + dz*dz)
		step := npcMoveSpeed(npc) * dt
		if dist <= step {
			npc.pos[0], npc.pos[2] = waypoint[0], waypoint[2]
			npc.path = npc.path[1:]
			if len(npc.path) == 0 {
				npc.moving = false
				if npc.taskType == bhvTaskMoveTo {
					if npc.pendingTaskAction != 0 {
						npcSetTutorialAction(npc, npc.pendingTaskAction)
						log.Printf("[CLUSTER] tutor %d arrived → action=%d", npc.viewBase, npc.pendingTaskAction)
						npc.pendingTaskAction = 0
					} else {
						npcSetTask(npc, bhvTaskIdle, -1, npc.pos, -1)
					}
				} else if npc.taskType == bhvTaskChargeAttack {
					npcSetTask(npc, bhvTaskChargeAttackHit, npc.chargeTargetView, npc.pos, 0)
					npc.attackBusyUntil = time.Now().Add(npcMeleeSwingLock)
				}
			}
		} else {
			nx := npc.pos[0] + float32(dx/dist*step)
			nz := npc.pos[2] + float32(dz/dist*step)
			npc.pos[0], npc.pos[2] = nx, nz
			npc.yawDeg = float32(math.Mod(math.Atan2(dx, dz)*180/math.Pi+360, 360))
		}
		npcSyncGround(room, npc, dt)
		moved = true
	}
	return moved
}

// anchors npc.pos[1] toward the navmesh floor at its current XZ, rate limited by
// npcGravityDropPerSec. so a correction reads as a settle/climb rather than a teleport. this needs work in future.
func npcSyncGround(room *Room, npc *spawnedNpc, dt float64) bool {
	gy, ok := groundHint(m2level(room), npc.pos[0], npc.pos[2], npc.pos[1])
	if !ok || gy == npc.pos[1] {
		return false
	}
	npc.groundY = gy
	if npc.pos[1] > gy {
		npc.pos[1] -= float32(npcGravityDropPerSec * dt)
		if npc.pos[1] < gy {
			npc.pos[1] = gy
		}
	} else {
		npc.pos[1] += float32(npcGravityDropPerSec * dt)
		if npc.pos[1] > gy {
			npc.pos[1] = gy
		}
	}
	return true
}

func m2level(room *Room) string {
	if room.clusters != nil {
		return room.clusters.level
	}
	return getCurScnFromProps(room.Name, room.Properties)
}


func clusterAliveSpawnedCount(room *Room, rt *clusterRT) int {
	alive := 0
	for _, vb := range rt.spawnedViews {
		for i := range room.npcs {
			if room.npcs[i].viewBase == vb && room.npcs[i].hp > 0 {
				alive++
				break
			}
		}
	}
	return alive
}


func (s *EnetServer) clusterTickSpawner(room *Room, m *clusterMachine, rt *clusterRT, now time.Time) bool {
	sp := rt.def.Spawning
	mult := npcWaveCountMult(room)
	spawnCap := maxClusterSpawns * mult
	if sp == nil || rt.slotCursor >= len(sp.Slots) || clusterAliveSpawnedCount(room, rt) >= spawnCap {
		return false
	}
	if !rt.nextSpawnAt.IsZero() && now.Before(rt.nextSpawnAt) {
		return false
	}
	playerCount := len(room.Players) - 1
	pos := clusterSpawnPos(m.level, rt.def)
	spawned := false
	for rt.slotCursor < len(sp.Slots) && clusterAliveSpawnedCount(room, rt) < spawnCap {
		slot := &sp.Slots[rt.slotCursor]
		rt.slotCursor++
		if slot.MinPlayers > playerCount {
			continue
		}
		kit := ""
		for _, p := range slot.Npcs {
			if p.Npc != "" && p.Npc != "None" {
				kit = strings.TrimPrefix(p.Npc, "CHR_")
				break
			}
		}
		if kit == "" {
			continue
		}
		count := mult
		if isMainBossKit(kit) {
			count = 1
		}
		for i := 0; i < count && clusterAliveSpawnedCount(room, rt) < spawnCap; i++ {
			spawnPos := pos
			if i > 0 {
				angle := float64(i) * (2 * math.Pi / float64(count))
				spawnPos[0] += float32(math.Cos(angle)) * clusterExtraSpawnSpread
				spawnPos[2] += float32(math.Sin(angle)) * clusterExtraSpawnSpread
			}
			vb := s.hostSpawnNpcAt(room, m.level, kit, spawnPos, nil, 0)
			rt.spawnedViews = append(rt.spawnedViews, vb)
			if npc := &room.npcs[len(room.npcs)-1]; npc.viewBase == vb {
				npcAssignDefaultWander(npc, kit, spawnPos, m.level)
			}
			spawned = true
		}
		break
	}
	interval := sp.NextSpawnTime
	if interval < 2 {
		interval = 2
	}
	rt.nextSpawnAt = now.Add(time.Duration(interval * float64(time.Second)))
	return spawned
}

func (s *EnetServer) clusterWaveCleared(room *Room, rt *clusterRT) bool {
	sp := rt.def.Spawning
	if sp != nil && rt.slotCursor < len(sp.Slots) {
		return false
	}
	for _, vb := range rt.spawnedViews {
		for i := range room.npcs {
			if room.npcs[i].viewBase == vb && room.npcs[i].hp > 0 {
				return false
			}
		}
	}
	return true
}

func clusterSpawnPos(level string, def *clusterDef) [3]float32 {
	var best *spawnEntry
	bestDist := float32(math.MaxFloat32)
	for _, e := range levelSpawnEntries(level) {
		if e.Script != "SpawnSlotContainer" || e.GO != def.Name || len(e.Pos) != 3 {
			continue
		}
		d := float32(0)
		if len(def.Pos) == 3 {
			dx, dz := e.Pos[0]-def.Pos[0], e.Pos[2]-def.Pos[2]
			d = dx*dx + dz*dz
		}
		if best == nil || d < bestDist {
			bestDist = d
			ee := e
			best = &ee
		}
	}
	if best != nil {
		return [3]float32{best.Pos[0], best.Pos[1], best.Pos[2]}
	}
	if len(def.Pos) == 3 {
		return [3]float32{def.Pos[0], def.Pos[1], def.Pos[2]}
	}
	return [3]float32{}
}
