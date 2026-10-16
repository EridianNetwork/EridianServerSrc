package photon

import (
	"time"

	"eridian/internal/domain"
)

const HostActorNr int32 = 1

const hostUserID = "$host"

type Room struct {
	Name       string
	Properties map[any]any
	Players    map[int32]string
	nextActor  int32
	viewIDCtr  int32
	CreatedAt  time.Time

	staticsSpawned          bool
	statics                 []staticEnt
	npcsSpawned             bool
	npcs                    []spawnedNpc
	npcViewCtr              int32
	lastNpcStream           time.Time
	lastPlayerStream        time.Time
	deferredDestroys        []deferredDestroy
	pendingVitalsResyncs    []pendingVitalsResync
	pendingReviveCancels    []pendingReviveCancel
	pendingPlayerRespawns   []pendingPlayerRespawn
	pendingShieldResyncs    []pendingShieldResync
	pendingNpcVitalsResyncs []pendingNpcVitalsResync
	pendingWaves            []pendingWave
	pendingImmediateSpawns  []spawnEntry
	pendingGrenades         []pendingGrenade
	pendingSuicideBombs     []pendingSuicideBomb
	pendingSpitDamages      []pendingSpitDamage
	pendingPoisonAttacks    []pendingPoisonAttack
	pendingNpcGrenades      []pendingNpcGrenade
	pendingPlayerUnstuns    []pendingPlayerUnstun
	pendingPlayerSpawnFx	[]pendingPlayerSpawnFx
	pendingNpcAreaHits      []pendingNpcAreaHit
	avatarViewCtr           int32

	lomSpawned        bool
	levelObjectStates map[int]bool

	navLayers map[string]bool

	coverOccupants map[int]int32

	firesNear map[int32]int

	dropPos map[int32][3]float32


	weaponElement map[int32]int32

	weaponType map[int32]int32

	weaponDamage map[int32]float64

	weaponFireRate map[int32]float64

	weaponPellets map[int32]float64

	dropItem map[int32]*domain.InventoryItem

	dropReward map[int32]dropReward

	pendingLootSpawns []pendingLootSpawn

	pendingWeaponClaims []pendingWeaponClaim

	pendingFireBirdSpawns []pendingFireBirdSpawn
	pendingFireChains     []pendingFireChain

	pendingHunterDroidSpawns []pendingHunterDroidSpawn

	markedNpcs map[int32]markedNpc
	markSent   map[int32]bool

	pendingTurretSpawns []pendingTurretSpawn

	actionGrantAt     []time.Time
	lastPlayerActions int32

	actionsGranted bool

	pendingAvatarSpawns []pendingAvatarSpawn

	pendingReplayEvents []pendingReplayEvent

	pendingOutfitResyncs []pendingOutfitResync

	pendingItemsResyncs []pendingItemsResync

	clusters *clusterMachine

	cutsceneActive bool

	bossPhase int

	mtActive   bool
	mtRunning  bool
	mtStart    float64
	mtDuration float64
	mtPause    float64
	mtTimedOut bool

	mtSendToTownAt time.Time

	pvpMatchEndAt time.Time
	pvpMatchEnded bool

	claptrapViewBase       int32
	claptrapCarrier        *ClientSession
	claptrapPos            [3]float32
	claptrapTeamScore      [2]int32
	claptrapLastScoreTeam  byte
	claptrapLastScoreValue byte

	claptrapLastZoneOwnerTeam byte

	claptrapPickupCooldownUntil time.Time

	luckyKeyGranted bool

	script       *roomScript
	scriptLoaded bool

	eventSeq           int64
	journal            []journaledEvent
	joinSeq            map[int32]int64
	viewStreams        map[int32]map[any]any
	playerPos          map[int32][3]float32
	streamDumps        int
	nonControllerDumps int
	moveSyncDumps      int
}

type spawnedNpc struct {
	viewBase int32
	// vitalsAt, when set, is when the deferred RPC_SendLifeAndShield is due (see hostQueueNpcVitals).
	vitalsAt time.Time
	kit      string
	pos      [3]float32
	groundY  float32
	grounded bool

	viewCount int32

	spawnedAt time.Time

	path   [][3]float32
	moving bool
	yawDeg float32

	hp    int32
	maxHP int32

	furyActive bool

	nextAtkAt time.Time

	nextGrenadeRollAt time.Time

	jumpCooldownUntil time.Time

	aggroRadiusScale float32
	pendingAggroAt   time.Time
	aggroEngaged     bool

	dazedUntil    time.Time
	confusedUntil time.Time

	dazeEffectUID    int32
	dazeDurationMult float32

	markSlowFrac  float32
	losAt         time.Time
	losBlocked    bool
	markSlowUntil time.Time

	forcedAggroView  int32
	forcedAggroUntil time.Time

	lastForcedAggroLogAt time.Time

	nextRangedAtkAt time.Time

	coverIdx         int
	nextCoverRollAt  time.Time
	inCover          bool
	exposed          bool
	nextCoverPhaseAt time.Time

	coverWallFailStreak int32
	avoidCoverIdx       int
	avoidCoverUntil     time.Time

	coverAggroView  int32
	coverAggroUntil time.Time

	visYaw   float32
	visSpeed float32
	visInit  bool

	combatIdle    bool
	settleUntil   time.Time
	stopHoldUntil time.Time
	moveHoldUntil time.Time
	dangerAt      time.Time
	noCoverUntil  time.Time

	nextRepathAt time.Time

	patrolPoints    [][3]float32
	patrolWaits     []float32
	patrolPolicy    string
	patrolIdx       int
	patrolling      bool
	patrolWaitUntil time.Time

	taskUID    int32
	taskType   int32
	taskTarget int32

	nextWarcryAt   time.Time
	warcriedEnrage bool

	attackBusyUntil time.Time
	taskDest        [3]float32
	taskSpeed       int32
	taskAction      int32

	taskVec1 [3]float32

	taskForce float32

	taskDieHeadshot bool

	pendingTaskAction int32

	weaponView int32

	lastEquipStreamSig string

	friendly bool

	jumping        bool
	jumpFrom       [3]float32
	jumpTo         [3]float32
	jumpAirStart   time.Time
	jumpAirEnd     time.Time
	jumpLandUntil  time.Time
	jumpDidDamage  bool
	jumpTargetView int32

	jumpArcHeight float32

	chargeHit        bool
	chargeTargetView int32

	poisonDashHit bool

	dotElement      int32
	dotAttackerView int32
	dotDPS          int32
	dotEndAt        time.Time
	dotNextTickAt   time.Time
}

func (npc *spawnedNpc) equipView() int32 {
	vc := npc.viewCount
	if vc <= 0 {
		vc = 4
	}
	return npc.viewBase + vc - 1
}

type deferredDestroy struct {
	view int32
	at   time.Time
}

type pendingVitalsResync struct {
	sess    *ClientSession
	base    int32
	maxLife int32
	at      time.Time
}

type pendingPlayerRespawn struct {
	sess    *ClientSession
	actorNr int32
	at      time.Time
}

type pendingReviveCancel struct {
	sess         *ClientSession
	avatarBase   int32
	downTaskUID  int32
	hasKilledNPC bool
	at           time.Time
}

type pendingPlayerSpawnFx struct {
	sess *ClientSession
	avatarBase int32
	at time.Time
}

type pendingPlayerUnstun struct {
	sess *ClientSession
	at   time.Time
}

type pendingShieldResync struct {
	sess *ClientSession
	base int32
	at   time.Time
}

type pendingLootSpawn struct {
	sess     *ClientSession
	level    string
	pid      int64
	chestPos []float32
	at       time.Time
}

type pendingFireBirdSpawn struct {
	sess *ClientSession
	at   time.Time
}

type pendingWeaponClaim struct {
	pickerView int32
	weaponView int32
	at         time.Time
	equip bool
}

type pendingHunterDroidSpawn struct {
	sess *ClientSession
	at   time.Time
}

type pendingTurretSpawn struct {
	sess *ClientSession
	at   time.Time
}

type markedNpc struct {
	until time.Time
}

type pendingNpcVitalsResync struct {
	view      int32
	equipView int32
	kit       string
	maxHP, hp int32
	at        time.Time
}

type pendingGrenade struct {
	view      int32
	pos       [3]float32
	ownerView int32
	at        time.Time
	spawnAt   time.Time
	spawned   bool
	sess      *ClientSession
	scattered bool
	origin    [3]float32
	vel       [3]float32
	dmgMult   float64
}

type pendingSuicideBomb struct {
	view    int32
	npcView int32
	pos     [3]float32
	at      time.Time
}

type pendingNpcGrenade struct {
	view    int32
	npcView int32
	pos     [3]float32
	at      time.Time
}

type pendingSpitDamage struct {
	sess         *ClientSession
	targetView   int32
	attackerView int32
	npcKit       string
	dmg          int32
	at           time.Time
}

type pendingNpcAreaHit struct {
	attackerView int32
	npcKit       string
	pos          [3]float32
	radius       float32
	dmg          int32
	hpFraction   float64
	element      int32
	at           time.Time
}

type pendingPoisonAttack struct {
	sess         *ClientSession
	targetView   int32
	attackerView int32
	npcKit       string
	aoe          bool
	center       [3]float32
	radius       float64
	dirX, dirZ   float64
	angle        float64
	dmg          int32
	at           time.Time
}

type staticEnt struct {
	viewBase     int32
	goName       string
	pos          [3]float32
	hp           int32
	maxHP        int32
	isBarrel     bool
	receiveShoot bool
	receiveMelee bool
	receiveArea  bool
}

type pendingWave struct {
	zone    string
	wave    int
	entries []spawnEntry
	dueAt   time.Time
}

type journaledEvent struct {
	seq     int64
	code    byte
	gated   bool
	payload map[byte]any
}

func (r *Room) JournalEvent(code byte, payload map[byte]any, gated bool) int64 {
	r.eventSeq++
	r.journal = append(r.journal, journaledEvent{seq: r.eventSeq, code: code, gated: gated, payload: payload})
	return r.eventSeq
}

func (r *Room) MissedEvents(actorNr int32, gatedSeq int64) []journaledEvent {
	joined := r.joinSeq[actorNr]
	var out []journaledEvent
	for _, e := range r.journal {
		if e.gated {
			if e.seq > gatedSeq {
				out = append(out, e)
			}
		} else if e.seq <= joined {
			out = append(out, e)
		}
	}
	return out
}

func (r *Room) NextAvatarViewBase() int32 {
	const lo, hi = HostActorNr*100000 + 50000, HostActorNr*100000 + 99990
	if r.avatarViewCtr < lo || r.avatarViewCtr >= hi {
		r.avatarViewCtr = lo
	}
	base := r.avatarViewCtr
	r.avatarViewCtr += 10
	return base
}

func newRoom(name string, properties map[any]any) *Room {
	if properties == nil {
		properties = make(map[any]any)
	}
	return &Room{
		Name:        name,
		Properties:  properties,
		Players:     map[int32]string{HostActorNr: hostUserID},
		nextActor:   HostActorNr + 1,
		joinSeq:     map[int32]int64{},
		viewStreams: map[int32]map[any]any{},
		CreatedAt:   time.Now(),
	}
}

func (r *Room) AddPlayer(userID string) int32 {
	nr := r.nextActor
	r.nextActor++
	r.Players[nr] = userID
	r.joinSeq[nr] = r.eventSeq
	return nr
}

func (r *Room) RemovePlayer(actorNr int32) {
	delete(r.Players, actorNr)
}

func (r *Room) ActorNrFor(userID string) int32 {
	for nr, uid := range r.Players {
		if uid == userID {
			return nr
		}
	}
	return 0
}

func (r *Room) IsMasterActor(actorNr int32) bool {
	for nr := range r.Players {
		if nr != HostActorNr && nr < actorNr {
			return false
		}
	}
	return true
}

func (r *Room) LowestActorNr() int32 {
	var lowest int32 = -1
	for nr := range r.Players {
		if nr == HostActorNr {
			continue
		}
		if lowest == -1 || nr < lowest {
			lowest = nr
		}
	}
	if lowest == -1 {
		return 0
	}
	return lowest
}

func (r *Room) NextViewID() int32 {
	r.viewIDCtr++
	if r.viewIDCtr < 9000 {
		r.viewIDCtr = 9000
	}
	return r.viewIDCtr
}

type pendingFireChain struct {
	sess *ClientSession
	pos  [3]float32
	at   time.Time
	dmg  int32
}
