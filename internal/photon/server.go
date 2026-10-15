package photon
import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"eridian/internal/db"
	"eridian/internal/domain"
	lua "github.com/yuin/gopher-lua"
)
func msNow() uint32 {
	return uint32(time.Now().UnixMilli() & 0xFFFFFFFF)
}

type killRegen struct {
	speed float64
	until time.Time
}

type ClientSession struct {
	addr        string
	udpAddr     *net.UDPAddr
	challenge   uint32
	peerID      uint16
	appName     string
	userID      string
	accountID   int64
	dbAccountID int64
	accountUsername string
	guildTag       string
	authenticated  bool
	roomName       string
	selectedCharID int64
	seenReliableSeq map[byte]map[uint32]struct{}
	charSelectedAt  time.Time
	lastInputFrame  int32
	pickedWeapons   []int32
	equippedWeapons []weaponSlotEntity
	hp, maxHP       int32
	isDead          bool
	respawnPending  bool
	shield, maxShield int32
	lastDamageAt time.Time
	shieldRegenBaseline   int32
	lastHealthRegenTickAt time.Time
	killRegens           []killRegen
	killRegenLastTickAt time.Time
	lavaNextTickAt time.Time
	shieldDNA Custom
	shieldView          int32
	downTaskUID         int32
	skillTaskUID        int32
	stunTaskUID         int32
	skillPhaseNext      int32
	skillCooldownEndAt  time.Time
	fireBirdActive      bool
	fireBirdEndAt       time.Time
	fireBirdLastTick    time.Time
	fireBirdBaseDamage  float64
	fireBirdPos         [3]float32
	fireBirdDir         [2]float32
	fireBirdLanded      bool
	fireBirdOffRun      float32
	fireBirdStepAt      time.Time
	fireBirdAngles      [3]int32
	evadeUntil          time.Time
	reviveReadyAt       time.Time
	hunterDroidView     int32
	hunterDroidActive   bool
	hunterDroidEndAt    time.Time
	hunterDroidLastMark time.Time
	chargeShotArmed     bool
	ammoRegenCarry      float64
	turretLastRepelAt   time.Time
	berserkActive       bool
	berserkEndAt        time.Time
	berserkRageLeft     int
	berserkMeleeUsed    int
	berserkDmgTaken     int32
	turretView          int32
	turretActive        bool
	turretEndAt         time.Time
	turretLastFireAt    time.Time
	turretLastAOEAt     time.Time
	turretArmedAt       time.Time
	turretBaseDamage    float64
	downedAt            time.Time
	weaponSlotDNAs      []any
	activeWeaponSlot    byte
	weaponTypes         []int32
	lastSupplyAt        time.Time
	ammoEst             int
	grenadeCount        int32
	grenadeCap          int32
	equipAt             time.Time
	townPosSavedAt      time.Time
	weaponsReadyAt      time.Time
	equipSlot           byte
	lastEquipWeaponKey  int
	lastEquipWeaponAt   time.Time
	crypto              *DiffieHellmanCryptoProvider
	connectedAt         time.Time
	lastRecv            time.Time
	lastActivityAt time.Time
	charName        string
	charLevel       int32
	charExp         int32
	charCurrency    int32
	charClassType   int32
	charAvatarParts string
	charAvatarMats  string
	charAvatarID    int32
	groupID         int32
	lobbyID         int
	currentTown     int32
	playlistId      int32
	mapGen byte
	hostChar    *domain.Character
	playerState byte
	teamID          byte
	avatarBase      int32
	avatarSpawnedAt time.Time
	streamsSeen     bool
	realPosSeen     bool
	realActionsSeen bool
	lastMoveLog     time.Time
	lastYawDeg      float64
	lastPitch16     int16
	lastMoveFlg     byte
	navBlockedSince time.Time
	raycastPatched bool
	lastRaycastDamageAt time.Time
	broadcastPos [3]float32
	spawnFloorY  float32
	gatedSeq     int64
	lastSkillTreeSig    string
	lastWeaponStreamSig string
	sessionInfoSent    bool
	lastSessionInfoSig string
	statHits, statCriticalHits     int32
	statTotalDamage, statCritDmg   int32
	statKills, statCriticalKills   int32
	statBadassKills, statBossKills int32
	statDeaths                     int32
	statSkillUses                  int32
	statExtraExp, statExtraMoney   int32
	pvpKillCount                   int32
	mu               sync.Mutex
	outSeq           uint32
	outUnreliableSeq uint32
	pending          map[uint32]*pendingCmd
	srtt, rttvar, rto time.Duration
}
type pendingCmd struct {
	cmd        []byte
	sentAt     time.Time
	origSentAt time.Time
	attempts int
}
func (s *ClientSession) nextSeq() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outSeq++
	return s.outSeq
}
func (s *ClientSession) currentSeq() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outSeq
}
func (s *ClientSession) nextUnreliableSeq() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outUnreliableSeq++
	return s.outUnreliableSeq
}
func (s *ClientSession) streamGraceElapsed() bool {
	return !s.avatarSpawnedAt.IsZero() && time.Since(s.avatarSpawnedAt) >= avatarStreamGracePeriod
}
const (
	minRTO = 300 * time.Millisecond
	maxRTO = 2 * time.Second
)
func (s *ClientSession) updateRTOLocked(sample time.Duration) {
	if sample <= 0 {
		return
	}
	if s.srtt == 0 {
		s.srtt = sample
		s.rttvar = sample / 2
	} else {
		diff := s.srtt - sample
		if diff < 0 {
			diff = -diff
		}
		s.rttvar = s.rttvar + (diff-s.rttvar)/4
		s.srtt = s.srtt + (sample-s.srtt)/8
	}
	rto := s.srtt + 4*s.rttvar
	if rto < minRTO {
		rto = minRTO
	} else if rto > maxRTO {
		rto = maxRTO
	}
	s.rto = rto
}
const seenReliableSeqCap = 256
func pruneSeenReliableSeq(seen map[uint32]struct{}, newSeq uint32) {
	if len(seen) <= seenReliableSeqCap {
		return
	}
	const keepWindow = seenReliableSeqCap / 2
	for s := range seen {
		if s <= newSeq && newSeq-s > keepWindow {
			delete(seen, s)
		}
	}
}
type fragKey struct {
	addr     string
	channel  byte
	startSeq uint32
}
type fragBuf struct {
	data     []byte
	total    int
	got      map[uint32]struct{}
	created  time.Time
}
type EnetServer struct {
	host            string
	port            int
	AdvertisedHost  string
	MaxPlayersTotal int
	conn            *net.UDPConn
	mu       sync.RWMutex
	sessions map[string]*ClientSession
	rooms    map[string]*Room
	rconActions chan func()
	rconPassword string
	fragBufs     map[fragKey]*fragBuf
	peerCounter  uint16
	lobbies      map[int]*LobbyRecord
	lobbyCounter int
	debugSceneIdx map[int32]int32
	groups        map[int]*GroupRecord
	groupCounter  int
	pendingGroupRoom map[string]string
	activeGameSession map[string]*ClientSession
	accountMapGen map[int64]byte
	pendingSceneIdx map[int64]pendingScene
	pendingLobbyDifficulty map[int64]int32
	pendingLobbyID map[int64]int
	pendingLobbyTeam map[int64]byte
	soldItems map[int64][]*domain.InventoryItem
	AFKKickSeconds int
	UnreliableMovementSync bool
	TutorialFreeActions bool
	ServerVersionID string
	NpcDifficultyTiers map[int32]NpcTierMult
	NpcPerLevelGrowth  float32
	NpcBossHPTiers     map[int32]float32
	XPMultiplier float64
	NpcKillXPBase       float64
	NpcKillXPBadassMult float64
	NpcKillXPBossMult   float64
	PlayerMeleeDamageRatio float64
	FireBirdCooldownSeconds    float64
	BerserkCooldownSeconds     float64
	TurretCooldownShortSeconds float64
	TurretCooldownLongSeconds  float64
	HunterCooldownShortSeconds float64
	HunterCooldownLongSeconds  float64
	PvPMatchDurationSeconds int
	PvPKillTarget           int32
	ClaptrapRoundSeconds int
	ClaptrapTargetScore  int32
	GameHandler *GameHandlers
	tickCount    int
	tickTotalDur time.Duration
	tickMaxDur   time.Duration
	PerfLog bool
}
func NewServer(host string, port int) *EnetServer {
	return &EnetServer{
		host:                   host,
		port:                   port,
		sessions:               make(map[string]*ClientSession),
		rooms:                  make(map[string]*Room),
		fragBufs:               make(map[fragKey]*fragBuf),
		lobbies:                make(map[int]*LobbyRecord),
		groups:                 make(map[int]*GroupRecord),
		pendingGroupRoom:       make(map[string]string),
		activeGameSession:      make(map[string]*ClientSession),
		accountMapGen:          make(map[int64]byte),
		pendingSceneIdx:        make(map[int64]pendingScene),
		pendingLobbyDifficulty: make(map[int64]int32),
		pendingLobbyID:         make(map[int64]int),
		soldItems:              make(map[int64][]*domain.InventoryItem),
		pendingLobbyTeam:       make(map[int64]byte),
		peerCounter:            1,
		rconActions:            make(chan func(), 8),
	}
}
func (s *EnetServer) WithGPK(enable bool) *EnetServer { return s }
func (s *EnetServer) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("photon: resolve %s: %w", addr, err)
	}
	s.conn, err = net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("photon: listen %s: %w", addr, err)
	}
	defer s.conn.Close()
	_ = s.conn.SetReadBuffer(4 << 20)
	_ = s.conn.SetWriteBuffer(4 << 20)
	log.Printf("[PHOTON] ENet UDP listening on %s", addr)
	go s.sessionWatchdog()
	go s.retransmitLoop()
	type udpPkt struct {
		remote *net.UDPAddr
		data   []byte
	}
	pkts := make(chan udpPkt, 4096)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, remote, err := s.conn.ReadFromUDP(buf)
			if err != nil {
				if s.conn == nil {
					close(pkts)
					return
				}
				log.Printf("[PHOTON] read error: %v", err)
				continue
			}
			data := make([]byte, n)
			copy(data, buf[:n])
			pkts <- udpPkt{remote, data}
		}
	}()
	adminCmds := make(chan string, 16)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			adminCmds <- scanner.Text()
		}
		close(adminCmds)
	}()
	const npcTickInterval = 66 * time.Millisecond
	ticker := time.NewTicker(npcTickInterval)
	defer ticker.Stop()
	dt := npcTickInterval.Seconds()
	const housekeepInterval = 15 * time.Second
	housekeepTicker := time.NewTicker(housekeepInterval)
	defer housekeepTicker.Stop()
	for {
		select {
		case p, ok := <-pkts:
			if !ok {
				return nil
			}
			s.handlePacketRecoverable(p.remote, p.data)
		case fn := <-s.rconActions:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[PANIC] rcon action panicked: %v\n%s", r, debug.Stack())
					}
				}()
				fn()
			}()
		case cmd, ok := <-adminCmds:
			if !ok {
				adminCmds = nil
				continue
			}
			if text, found := strings.CutPrefix(cmd, "say "); found {
				for _, room := range s.rooms {
					s.hostSendUIMessage(room, text, 5)
				}
				log.Printf("[ADMIN] sent UI message to %d room(s): %q", len(s.rooms), text)
			}
			if text, found := strings.CutPrefix(cmd, "announce "); found {
				const announcementTypeBroadcast = int32(1)
				online := 0
				for _, sess := range s.sessions {
					if sess.appName == "Online" {
						online++
					}
				}
				s.broadcastToAll(EvAnnouncements, map[byte]any{
					0: map[int]any{0: map[int]any{0: text, 1: announcementTypeBroadcast}},
				}, nil)
				log.Printf("[ADMIN] sent announcement to %d online session(s): %q", online, text)
			}
			if cmd == "reload" {
				for _, room := range s.rooms {
					s.scriptReload(room)
				}
				log.Printf("[ADMIN] reloaded scripts in %d room(s)", len(s.rooms))
				reloadLevelSpawns()
				reloadLevelClusterDefs()
				reloadLevelCovers()
				log.Printf("[ADMIN] reloaded level_spawns/level_clusters/level_covers caches (new rooms only)")
			}
			if rest, found := strings.CutPrefix(cmd, "debugscene "); found {
				fields := strings.Fields(rest)
				if fields[0] == "clear" {
					s.debugSceneIdx = nil
					log.Printf("[ADMIN] cleared all debugscene overrides")
				} else if len(fields) == 2 {
					pid, err1 := strconv.Atoi(fields[0])
					idx, err2 := strconv.Atoi(fields[1])
					if err1 != nil || err2 != nil {
						log.Printf("[ADMIN] debugscene: bad usage %q (want: debugscene <playlistId> <sceneIdx>)", cmd)
					} else if idx == 0 {
						delete(s.debugSceneIdx, int32(pid))
						log.Printf("[ADMIN] cleared debugscene override for playlist %d", pid)
					} else {
						if s.debugSceneIdx == nil {
							s.debugSceneIdx = map[int32]int32{}
						}
						s.debugSceneIdx[int32(pid)] = int32(idx)
						log.Printf("[ADMIN] playlist %d will now start at SceneIdx %d", pid, idx)
					}
				} else {
					log.Printf("[ADMIN] debugscene: bad usage %q (want: debugscene <playlistId> <sceneIdx>)", cmd)
				}
			}
			if rest, found := strings.CutPrefix(cmd, "firetrigger "); found {
				name := strings.TrimSpace(rest)
				fired := 0
				for _, room := range s.rooms {
					level := getCurScnFromProps(room.Name, room.Properties)
					objs := levelObjectEntries(level)
					for idx, obj := range objs {
						if obj.GO == name {
							s.hostOpenLevelObject(room, idx)
							s.hostClusterLevelTrigger(room, idx)
							fired++
						}
					}
				}
				log.Printf("[ADMIN] firetrigger %q: fired in %d room(s)", name, fired)
			}
			if cmd == "where" {
				for _, room := range s.rooms {
					for _, uid := range room.Players {
						target := s.sessionInRoom(uid, room.Name)
						if target == nil || target.avatarBase == 0 {
							continue
						}
						s.hostSpawnWeaponDrop(room, target, nil, -1)
					}
				}
			}
			if cmd == "perf" {
				s.logPerfStats()
			}
			if rest, found := strings.CutPrefix(cmd, "perf "); found {
				switch strings.TrimSpace(rest) {
				case "on":
					s.PerfLog = true
					log.Printf("[ADMIN] perf logging ON (every %s)", housekeepInterval)
				case "off":
					s.PerfLog = false
					log.Printf("[ADMIN] perf logging OFF")
				default:
					log.Printf("[ADMIN] perf: usage: perf | perf on | perf off")
				}
			}
		case <-ticker.C:
			tickStart := time.Now()
			for _, room := range s.rooms {
				s.hostTickRoomRecoverable(room, dt)
			}
			elapsed := time.Since(tickStart)
			s.tickCount++
			s.tickTotalDur += elapsed
			if elapsed > s.tickMaxDur {
				s.tickMaxDur = elapsed
			}
		case <-housekeepTicker.C:
			s.checkAFKSessions()
			s.reapStaleSessions()
			if s.PerfLog {
				s.logPerfStats()
			} else {
				s.tickCount, s.tickTotalDur, s.tickMaxDur = 0, 0, 0
			}
		}
	}
}
func (s *EnetServer) hostTickRoomRecoverable(room *Room, dt float64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PANIC] room=%s tick panicked, destroying instance: %v\n%s", room.Name, r, debug.Stack())
			s.hostDestroyCrashedRoom(room)
		}
	}()
	s.hostTickRoom(room, dt)
}
func (s *EnetServer) hostDestroyCrashedRoom(room *Room) {
	for actorNr, uid := range room.Players {
		sess := s.sessionInRoom(uid, room.Name)
		if sess == nil {
			continue
		}
		s.evictFromRoom(sess, actorNr, room)
	}
	delete(s.rooms, room.Name)
}
func (s *EnetServer) hostTickRoom(room *Room, dt float64) {
	gravityMoved := len(room.npcs) > 0 && s.hostTickNpcGravity(room, dt)
	aiActed := s.hostTickNpcAI(room, dt)
	walked := len(room.npcs) > 0 && s.hostTickNpcMovement(room, dt)
	gravityMoved = gravityMoved || walked || aiActed
	immediateSpawned := s.hostAdvanceImmediateNpcSpawns(room)
	wavesReleased := s.hostAdvanceWaves(room)
	clustersChanged := s.hostTickClusters(room)
	s.hostRetryActionGrants(room)
	s.hostRunPendingAvatarSpawns(room)
	s.hostRunPendingReplayEvents(room)
	s.hostRunDeferredDestroys(room)
	s.hostRunPendingVitalsResyncs(room)
	s.hostRunPendingReviveCancels(room)
	s.hostRunPendingPlayerRespawns(room)
	s.hostRunPendingPlayerSpawnFx(room)
	s.hostRunPendingPlayerUnstuns(room)
	s.hostRunPendingOutfitResyncs(room)
	s.hostRunPendingItemsResyncs(room)
	s.hostRunPendingShieldResyncs(room)
	s.hostRunPendingNpcVitalsResyncs(room)
	s.hostRunPendingLootSpawns(room)
	s.hostRunPendingWeaponClaims(room)
	s.hostRunPendingFireBirdSpawns(room)
	s.hostTickFireBirds(room)
	s.hostTickBerserk(room)
	s.hostTickElementalDOT(room)
	s.hostTickHurtZones(room)
	s.hostRunPendingHunterDroidSpawns(room)
	s.hostTickHunterDroids(room)
	s.hostRunPendingTurretSpawns(room)
	s.hostTickTurrets(room)
	s.hostTickPlayerDownTimeout(room)
	s.hostTickGrenades(room)
	s.hostTickSuicideBombs(room)
	s.hostTickNpcGrenades(room)
	s.hostTickPendingSpitDamages(room)
	s.hostTickPendingPoisonAttacks(room)
	s.hostTickPendingNpcAreaHits(room)
	s.hostTickSupplyStations(room)
	s.hostTickShieldRegen(room)
	s.hostTickHealthRegen(room)
	s.hostTickPendingEquips(room)
	s.hostTickTownPositionSave(room)
	s.hostTickMissionTimer(room)
	s.hostRunMissionTimerFail(room)
	s.hostTickPvPMatchTimer(room)
	s.hostTickClaptrapSoccer(room)
	s.scriptTick(room, dt)
	npcRefresh := len(room.npcs) > 0 && time.Since(room.lastNpcStream) >= time.Second
	playerActive := false
	for _, uid := range room.Players {
		if sess := s.sessionInRoom(uid, room.Name); sess != nil && (sess.lastMoveFlg != 0 || sess.isDead) {
			playerActive = true
			break
		}
	}
	playerRefresh := len(room.Players) > 0 && (playerActive || time.Since(room.lastPlayerStream) >= time.Second)
	if gravityMoved || immediateSpawned || wavesReleased || clustersChanged || npcRefresh || playerRefresh {
		s.hostSendSerialize(room)
		room.lastNpcStream = time.Now()
		room.lastPlayerStream = time.Now()
	}
}
func (s *EnetServer) logPerfStats() {
	npcs, players := 0, 0
	for _, room := range s.rooms {
		npcs += len(room.npcs)
		players += len(room.Players)
	}
	if s.tickCount == 0 {
		log.Printf("[PERF] rooms=%d npcs=%d players=%d (no tick samples yet)", len(s.rooms), npcs, players)
		return
	}
	avg := s.tickTotalDur / time.Duration(s.tickCount)
	log.Printf("[PERF] tick avg=%v max=%v samples=%d rooms=%d npcs=%d players=%d",
		avg, s.tickMaxDur, s.tickCount, len(s.rooms), npcs, players)
	s.tickCount, s.tickTotalDur, s.tickMaxDur = 0, 0, 0
}
func (s *EnetServer) checkAFKSessions() {
	if s.AFKKickSeconds <= 0 {
		return
	}
	timeout := time.Duration(s.AFKKickSeconds) * time.Second
	now := time.Now()
	for _, room := range s.rooms {
		for actorNr, userID := range room.Players {
			if actorNr == HostActorNr {
				continue
			}
			sess := s.sessionInRoom(userID, room.Name)
			if sess == nil || sess.avatarBase == 0 || sess.lastActivityAt.IsZero() {
				continue
			}
			if idle := now.Sub(sess.lastActivityAt); idle < timeout {
				continue
			}
			log.Printf("[PHOTON] AFK kick user=%s room=%s idle=%s", sess.userID, room.Name, now.Sub(sess.lastActivityAt).Round(time.Second))
			s.evictFromRoom(sess, actorNr, room)
		}
	}
}
const staleConnectionTimeout = 30 * time.Second
func (s *EnetServer) reapStaleSessions() {
	now := time.Now()
	for k, fb := range s.fragBufs {
		if now.Sub(fb.created) > staleConnectionTimeout {
			delete(s.fragBufs, k)
		}
	}
	for _, sess := range s.snapshotSessions() {
		if sess.lastRecv.IsZero() {
			if now.Sub(sess.connectedAt) < staleConnectionTimeout {
				continue
			}
			log.Printf("[PHOTON] reaping never-progressed connection addr=%s peerID=%d age=%s",
				sess.addr, sess.peerID, now.Sub(sess.connectedAt).Round(time.Second))
			s.handleDisconnect(sess)
			continue
		}
		if now.Sub(sess.lastRecv) < staleConnectionTimeout {
			continue
		}
		log.Printf("[PHOTON] reaping stale session addr=%s user=%s app=%q idle=%s",
			sess.addr, sess.userID, sess.appName, now.Sub(sess.lastRecv).Round(time.Second))
		s.handleDisconnect(sess)
	}
}
func (s *EnetServer) Shutdown() {
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}
func buildCmdAck(channel byte, seqNum, sentTime uint32) []byte {
	out := []byte{CTAck, channel, 0, 4}
	out = binary.BigEndian.AppendUint32(out, 20)
	out = binary.BigEndian.AppendUint32(out, 0)
	out = binary.BigEndian.AppendUint32(out, seqNum)
	out = binary.BigEndian.AppendUint32(out, sentTime)
	return out
}
func buildCmdVerifyConnect(peerID uint16, seqNum uint32) []byte {
	out := []byte{CTVerifyConnect, 0, 1, 4}
	out = binary.BigEndian.AppendUint32(out, 44)
	out = binary.BigEndian.AppendUint32(out, seqNum)
	body := make([]byte, 32)
	binary.BigEndian.PutUint16(body[0:], peerID)
	return append(out, body...)
}
func buildCmdSendReliable(channel byte, seqNum uint32, payload []byte) []byte {
	size := 12 + len(payload)
	out := []byte{CTSendReliable, channel, 1, 4}
	out = binary.BigEndian.AppendUint32(out, uint32(size))
	out = binary.BigEndian.AppendUint32(out, seqNum)
	return append(out, payload...)
}
func buildCmdSendUnreliable(channel byte, reliableSeq, unreliableSeq uint32, payload []byte) []byte {
	size := 16 + len(payload)
	out := []byte{CTSendUnreliable, channel, 1, 4}
	out = binary.BigEndian.AppendUint32(out, uint32(size))
	out = binary.BigEndian.AppendUint32(out, reliableSeq)
	out = binary.BigEndian.AppendUint32(out, unreliableSeq)
	return append(out, payload...)
}
const maxFragmentPayload = 1200
func buildCmdSendFragment(channel byte, seq, startSeq, fragCount, fragNumber, totalLen, fragOff uint32, fragData []byte) []byte {
	size := 12 + 20 + len(fragData)
	out := []byte{CTSendFragment, channel, 1, 4}
	out = binary.BigEndian.AppendUint32(out, uint32(size))
	out = binary.BigEndian.AppendUint32(out, seq)
	out = binary.BigEndian.AppendUint32(out, startSeq)
	out = binary.BigEndian.AppendUint32(out, fragCount)
	out = binary.BigEndian.AppendUint32(out, fragNumber)
	out = binary.BigEndian.AppendUint32(out, totalLen)
	out = binary.BigEndian.AppendUint32(out, fragOff)
	return append(out, fragData...)
}
func buildPacket(peerID uint16, challenge uint32, commands [][]byte) []byte {
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:], peerID)
	hdr[2] = 0
	hdr[3] = byte(len(commands))
	binary.BigEndian.PutUint32(hdr[4:], msNow())
	binary.BigEndian.PutUint32(hdr[8:], challenge)
	out := hdr
	for _, cmd := range commands {
		out = append(out, cmd...)
	}
	return out
}
func buildInitOK() []byte {
	return []byte{0xF3, MsgInitOK}
}
func buildOpResponse(opCode byte, returnCode int16, params map[byte]any) []byte {
	out := []byte{0xF3, MsgOpResponse, opCode}
	out = binary.BigEndian.AppendUint16(out, uint16(returnCode))
	out = append(out, TString)
	out = binary.BigEndian.AppendUint16(out, 0)
	enc, _ := EncodeParams(params)
	return append(out, enc...)
}
func buildEvent(eventCode byte, params map[byte]any) []byte {
	out := []byte{0xF3, MsgEvent, eventCode}
	enc, err := EncodeParams(params)
	if err != nil {
		log.Printf("[PHOTON] buildEvent: encode error event=%d: %v", eventCode, err)
		enc = []byte{0, 0}
	}
	return append(out, enc...)
}
func buildInternalOpResponse(opCode byte, returnCode int16, params map[byte]any) []byte {
	out := []byte{0xF3, MsgInternalRsp, opCode}
	out = binary.BigEndian.AppendUint16(out, uint16(returnCode))
	out = append(out, TString)
	out = binary.BigEndian.AppendUint16(out, 0)
	enc, _ := EncodeParams(params)
	return append(out, enc...)
}
func (s *EnetServer) sendRaw(addr *net.UDPAddr, data []byte) {
	if s.conn == nil {
		return
	}
	if _, err := s.conn.WriteTo(data, addr); err != nil {
		log.Printf("[PHOTON] send error to %s: %v", addr, err)
	}
}
func (s *EnetServer) sendPacket(sess *ClientSession, commands ...[]byte) {
	pkt := buildPacket(sess.peerID, sess.challenge, commands)
	s.sendRaw(sess.udpAddr, pkt)
}
func (s *EnetServer) reliableCmd(sess *ClientSession, payload []byte) []byte {
	seq := sess.nextSeq()
	cmd := buildCmdSendReliable(0, seq, payload)
	now := time.Now()
	sess.mu.Lock()
	if sess.pending == nil {
		sess.pending = make(map[uint32]*pendingCmd)
	}
	sess.pending[seq] = &pendingCmd{cmd: cmd, sentAt: now, origSentAt: now}
	sess.mu.Unlock()
	return cmd
}
func (s *EnetServer) reliableFragments(sess *ClientSession, payload []byte) [][]byte {
	fragCount := (len(payload) + maxFragmentPayload - 1) / maxFragmentPayload
	totalLen := uint32(len(payload))
	cmds := make([][]byte, 0, fragCount)
	var startSeq uint32
	for i := 0; i < fragCount; i++ {
		off := i * maxFragmentPayload
		end := off + maxFragmentPayload
		if end > len(payload) {
			end = len(payload)
		}
		seq := sess.nextSeq()
		if i == 0 {
			startSeq = seq
		}
		cmd := buildCmdSendFragment(0, seq, startSeq, uint32(fragCount), uint32(i), totalLen, uint32(off), payload[off:end])
		now := time.Now()
		sess.mu.Lock()
		if sess.pending == nil {
			sess.pending = make(map[uint32]*pendingCmd)
		}
		sess.pending[seq] = &pendingCmd{cmd: cmd, sentAt: now, origSentAt: now}
		sess.mu.Unlock()
		cmds = append(cmds, cmd)
	}
	return cmds
}
func (s *EnetServer) sendReliable(sess *ClientSession, payload []byte) {
	if len(payload) <= maxFragmentPayload {
		s.sendPacket(sess, s.reliableCmd(sess, payload))
		return
	}
	for _, cmd := range s.reliableFragments(sess, payload) {
		s.sendPacket(sess, cmd)
	}
}
func (s *EnetServer) sendUnreliable(sess *ClientSession, payload []byte) {
	cmd := buildCmdSendUnreliable(0, sess.currentSeq(), sess.nextUnreliableSeq(), payload)
	s.sendPacket(sess, cmd)
}
func (s *EnetServer) ackReliable(sess *ClientSession, seq uint32) {
	now := time.Now()
	sess.mu.Lock()
	if p, ok := sess.pending[seq]; ok && p.attempts == 0 {
		sess.updateRTOLocked(now.Sub(p.origSentAt))
	}
	delete(sess.pending, seq)
	sess.mu.Unlock()
}
func (s *EnetServer) retransmitLoop() {
	const (
		interval = 100 * time.Millisecond
		defaultRTO = minRTO
		giveupWindow = 120 * time.Second
	)
	for {
		time.Sleep(interval)
		s.retransmitTick(defaultRTO, giveupWindow)
	}
}
func (s *EnetServer) retransmitTick(defaultRTO time.Duration, giveupWindow time.Duration) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PANIC] retransmitLoop tick panicked, skipped: %v\n%s", r, debug.Stack())
		}
	}()
	now := time.Now()
	s.mu.RLock()
	sessions := make([]*ClientSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.RUnlock()
	for _, sess := range sessions {
		var due []struct {
			seq uint32
			cmd []byte
		}
		sess.mu.Lock()
		rto := sess.rto
		if rto == 0 {
			rto = defaultRTO
		}
		for seq, p := range sess.pending {
			if now.Sub(p.origSentAt) > giveupWindow {
				log.Printf("[PHOTON] giving up on reliable seq=%d user=%s age=%s", seq, sess.userID, now.Sub(p.origSentAt).Round(time.Millisecond))
				delete(sess.pending, seq)
				continue
			}
			backoff := rto * time.Duration(p.attempts+1)
			if backoff > maxRTO {
				backoff = maxRTO
			}
			if now.Sub(p.sentAt) < backoff {
				continue
			}
			p.attempts++
			p.sentAt = now
			due = append(due, struct {
				seq uint32
				cmd []byte
			}{seq, p.cmd})
		}
		sess.mu.Unlock()
		sort.Slice(due, func(i, j int) bool { return due[i].seq < due[j].seq })
		for _, d := range due {
			s.sendPacket(sess, d.cmd)
		}
	}
}
func (s *EnetServer) sendAck(sess *ClientSession, channel byte, seqNum, sentTime uint32) {
	s.sendPacket(sess, buildCmdAck(channel, seqNum, sentTime))
}
func (s *EnetServer) sendOpResponse(sess *ClientSession, opCode byte, returnCode int16, params map[byte]any) {
	switch opCode {
	case 84, 121, 186, 99, 196, 83, 80, 253, 230:
		log.Printf("[PHOTON] <<< op=%d user=%s returnCode=%d params=%v", opCode, sess.userID, returnCode, "<Omitted>")
	default:
		if returnCode != 0 {
			log.Printf("[PHOTON] <<< op=%d user=%s returnCode=%d params=%v", opCode, sess.userID, returnCode, params)
		}
	}
	payload := buildOpResponse(opCode, returnCode, params)
	s.sendReliable(sess, payload)
}
func (s *EnetServer) sendEventTo(sess *ClientSession, eventCode byte, params map[byte]any) {
	evt := buildEvent(eventCode, params)
	s.sendReliable(sess, evt)
}
func (s *EnetServer) broadcastEvent(room *Room, eventCode byte, params map[byte]any, exclude *ClientSession, targets []int32) {
	evt := buildEvent(eventCode, params)
	for nr, uid := range room.Players {
		if len(targets) > 0 {
			found := false
			for _, t := range targets {
				if t == nr {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target == exclude {
			continue
		}
		s.sendReliable(target, evt)
	}
}
func (s *EnetServer) broadcastEventUnreliable(room *Room, eventCode byte, params map[byte]any, exclude *ClientSession, targets []int32) {
	evt := buildEvent(eventCode, params)
	for nr, uid := range room.Players {
		if len(targets) > 0 {
			found := false
			for _, t := range targets {
				if t == nr {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		target := s.sessionInRoom(uid, room.Name)
		if target == nil || target == exclude {
			continue
		}
		s.sendUnreliable(target, evt)
	}
}
func (s *EnetServer) LivePlayerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[int64]struct{}, len(s.sessions))
	for _, sess := range s.sessions {
		if sess.authenticated && sess.dbAccountID != 0 {
			seen[sess.dbAccountID] = struct{}{}
		}
	}
	return len(seen)
}
func (s *EnetServer) sessionByUser(userID string) *ClientSession {
	for _, sess := range s.sessions {
		if sess.userID == userID {
			return sess
		}
	}
	return nil
}
func (s *EnetServer) sessionOnline(userID string) *ClientSession {
	for _, sess := range s.sessions {
		if sess.userID == userID && sess.appName == "Online" {
			return sess
		}
	}
	return nil
}
func (s *EnetServer) sessionInRoom(userID, roomName string) *ClientSession {
	var match, fallback *ClientSession
	for _, sess := range s.sessions {
		if sess.userID != userID {
			continue
		}
		if sess.roomName == roomName {
			if match == nil || sess.lastRecv.After(match.lastRecv) {
				match = sess
			}
			continue
		}
		if fallback == nil || sess.lastRecv.After(fallback.lastRecv) {
			fallback = sess
		}
	}
	if match != nil {
		return match
	}
	return fallback
}

// snapshotSessions copies the session set so callers can disconnect while iterating.
func (s *EnetServer) snapshotSessions() []*ClientSession {
	out := make([]*ClientSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, sess)
	}
	return out
}

func (s *EnetServer) migrateSession(sess *ClientSession, remote *net.UDPAddr) {
	oldKey := sess.addr
	newKey := remote.String()
	log.Printf("[PHOTON] session resumed from new address user=%s peerID=%d %s -> %s", sess.userID, sess.peerID, oldKey, newKey)
	s.mu.Lock()
	delete(s.sessions, oldKey)
	sess.addr = newKey
	sess.udpAddr = remote
	s.sessions[newKey] = sess
	s.mu.Unlock()
	for k, fb := range s.fragBufs {
		if k.addr == oldKey {
			delete(s.fragBufs, k)
			k.addr = newKey
			s.fragBufs[k] = fb
		}
	}
}

func (s *EnetServer) dropStaleRoomSessions(sess *ClientSession) {
	for _, other := range s.snapshotSessions() {
		if other == sess || other.userID != sess.userID || other.roomName == "" {
			continue
		}
		log.Printf("[PHOTON] dropping stale room membership user=%s room=%s oldAddr=%s", other.userID, other.roomName, other.addr)
		s.removeFromRoom(other)
	}
}
func (s *EnetServer) bumpMapGen(sess *ClientSession) {
	s.accountMapGen[sess.dbAccountID]++
	sess.mapGen = s.accountMapGen[sess.dbAccountID]
}
func (s *EnetServer) sessionInAnyRoom(dbAccountID int64) *ClientSession {
	for _, sess := range s.sessions {
		if sess.dbAccountID == dbAccountID && sess.roomName != "" && sess.hostChar != nil {
			return sess
		}
	}
	return nil
}
func (s *EnetServer) handlePacketRecoverable(remote *net.UDPAddr, data []byte) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PANIC] handlePacket from %s panicked, packet dropped: %v\n%s", remote, r, debug.Stack())
		}
	}()
	s.handlePacket(remote, data)
}
func (s *EnetServer) handlePacket(remote *net.UDPAddr, data []byte) {
	if len(data) < 13 {
		return
	}
	cmdCount := int(data[3])
	sentTime := binary.BigEndian.Uint32(data[4:])
	challenge := binary.BigEndian.Uint32(data[8:])
	addrKey := remote.String()
	sess := s.sessions[addrKey]
	if sess == nil && data[12] != CTConnect {
		peerID := binary.BigEndian.Uint16(data[0:])
		for _, cand := range s.sessions {
			if cand.peerID == peerID && cand.challenge == challenge && !db.IsIPBanned(remote.IP.String()) {
				s.migrateSession(cand, remote)
				sess = cand
				break
			}
		}
	}
	if sess != nil {
		sess.lastRecv = time.Now()
	}
	pos := 12
	for range cmdCount {
		if pos+12 > len(data) {
			break
		}
		cmdType := data[pos]
		cmdChan := data[pos+1]
		cmdSize := binary.BigEndian.Uint32(data[pos+4:])
		seqNum := binary.BigEndian.Uint32(data[pos+8:])
		cmdEnd := pos + int(cmdSize)
		payloadStart := pos + 12
		if int(cmdSize) < 12 || cmdEnd > len(data) {
			break
		}
		pos = cmdEnd
		if cmdType == CTAck {
			if sess != nil && payloadStart+4 <= cmdEnd {
				ackedSeq := binary.BigEndian.Uint32(data[payloadStart:])
				s.ackReliable(sess, ackedSeq)
			}
			continue
		}
		if cmdType == CTConnect {
			sess = s.handleConnect(remote, addrKey, challenge, sentTime, cmdChan, seqNum)
			continue
		}
		if sess == nil {
			log.Printf("[PHOTON] unknown session from %s, ignoring cmd %d", remote, cmdType)
			continue
		}
		sess.challenge = challenge
		if cmdType == CTPing || cmdType == CTEGServerTime {
			s.sendAck(sess, cmdChan, seqNum, sentTime)
			continue
		}
		if cmdType == CTDisconnect {
			log.Printf("[PHOTON] CT_DISCONNECT from %s (user=%s)", "", sess.userID)
			s.sendAck(sess, cmdChan, seqNum, sentTime)
			s.handleDisconnect(sess)
			continue
		}
		if cmdType == CTSendReliable || cmdType == CTSendUnreliable {
			if cmdType == CTSendUnreliable {
				payloadStart += 4
			}
			payload := data[payloadStart:cmdEnd]
			s.sendAck(sess, cmdChan, seqNum, sentTime)
			if cmdType == CTSendReliable {
				if sess.seenReliableSeq == nil {
					sess.seenReliableSeq = map[byte]map[uint32]struct{}{}
				}
				seen := sess.seenReliableSeq[cmdChan]
				if seen == nil {
					seen = map[uint32]struct{}{}
					sess.seenReliableSeq[cmdChan] = seen
				}
				if _, dup := seen[seqNum]; dup {
					continue
				}
				seen[seqNum] = struct{}{}
				pruneSeenReliableSeq(seen, seqNum)
			}
			s.handleMessage(sess, payload)
		} else if cmdType == CTSendFragment {
			s.sendAck(sess, cmdChan, seqNum, sentTime)
			s.handleFragment(sess, addrKey, data, payloadStart, cmdEnd, cmdChan)
		}
	}
}
func (s *EnetServer) handleConnect(remote *net.UDPAddr, addrKey string, challenge uint32, sentTime uint32, cmdChan byte, seqNum uint32) *ClientSession {
	sess := s.sessions[addrKey]
	if sess != nil && sess.challenge != challenge {
		log.Printf("[PHOTON] reconnect from %s with new challenge, resetting stale session user=%s", addrKey, sess.userID)
		s.handleDisconnect(sess)
		sess = nil
	}
	if sess == nil {
		if db.IsIPBanned(remote.IP.String()) {
			log.Printf("[PHOTON] connection from banned IP %s dropped", remote.IP)
			return nil
		}
		peerID := s.peerCounter
		s.peerCounter++
		sess = &ClientSession{
			addr:        addrKey,
			udpAddr:     remote,
			challenge:   challenge,
			peerID:      peerID,
			connectedAt: time.Now(),
			currentTown: -1,
		}
		s.mu.Lock()
		s.sessions[addrKey] = sess
		s.mu.Unlock()
		log.Printf("[PHOTON] new connection from %s peerID=%d", "", peerID)
	}
	ack := buildCmdAck(cmdChan, seqNum, sentTime)
	verify := buildCmdVerifyConnect(sess.peerID, 0)
	s.sendPacket(sess, ack, verify)
	return sess
}
func (s *EnetServer) handleDisconnect(sess *ClientSession) {
	log.Printf("[PHOTON] disconnect from %s (user=%s)", sess.addr, sess.userID)
	charID := sess.selectedCharID
	if charID == 0 && sess.hostChar != nil {
		charID = sess.hostChar.CharID
	}
	if charID != 0 {
		s.saveTownPosition(sess, s.rooms[sess.roomName])
		flushCharacterPlayTime(sess)
		if err := db.UpdateCharacterLastSeen(charID); err != nil {
			log.Printf("[PHOTON] disconnect: UpdateCharacterLastSeen charID=%d: %v", charID, err)
		}
	}
	if sess.roomName != "" {
		s.removeFromRoom(sess)
	}
	if sess.lobbyID != 0 {
		s.cleanLeaveLobby(sess)
	}
	if sess.groupID != 0 {
		s.cleanLeaveGroup(sess)
	}
	delete(s.pendingGroupRoom, sess.userID)
	if s.activeGameSession[sess.userID] == sess {
		delete(s.activeGameSession, sess.userID)
	}
	s.mu.Lock()
	if s.sessions[sess.addr] == sess {
		delete(s.sessions, sess.addr)
	}
	s.mu.Unlock()
	for k := range s.fragBufs {
		if k.addr == sess.addr {
			delete(s.fragBufs, k)
		}
	}
	sess.mu.Lock()
	sess.pending = nil
	sess.mu.Unlock()
	if charID != 0 {
		s.broadcastPresenceUpdate(charID)
	}
}
func (s *EnetServer) handleFragment(sess *ClientSession, addrKey string, data []byte, payloadStart, cmdEnd int, cmdChan byte) {
	if payloadStart+20 > cmdEnd {
		log.Printf("[PHOTON] fragment too short from %s", sess.addr)
		return
	}
	startSeq := binary.BigEndian.Uint32(data[payloadStart:])
	fragCount := int(binary.BigEndian.Uint32(data[payloadStart+4:]))
	fragNum := binary.BigEndian.Uint32(data[payloadStart+8:])
	totalLen := int(binary.BigEndian.Uint32(data[payloadStart+12:]))
	fragOff := int(binary.BigEndian.Uint32(data[payloadStart+16:]))
	fragData := data[payloadStart+20 : cmdEnd]
	if fragCount <= 0 || totalLen < 0 || totalLen > 8<<20 || fragOff < 0 || fragOff+len(fragData) > totalLen {
		log.Printf("[PHOTON] bad fragment from %s (count=%d total=%d off=%d len=%d)", sess.addr, fragCount, totalLen, fragOff, len(fragData))
		return
	}
	key := fragKey{addr: addrKey, channel: cmdChan, startSeq: startSeq}
	fb := s.fragBufs[key]
	if fb == nil {
		fb = &fragBuf{data: make([]byte, totalLen), total: fragCount, got: map[uint32]struct{}{}, created: time.Now()}
		s.fragBufs[key] = fb
	}
	if _, dup := fb.got[fragNum]; dup {
		return
	}
	fb.got[fragNum] = struct{}{}
	copy(fb.data[fragOff:], fragData)
	if len(fb.got) == fb.total {
		payload := fb.data
		delete(s.fragBufs, key)
		s.handleMessage(sess, payload)
	}
}
func (s *EnetServer) handleMessage(sess *ClientSession, payload []byte) {
	if len(payload) < 2 || payload[0] != 0xF3 {
		log.Printf("[PHOTON] non-0xF3 payload from %s: %x", sess.addr, payload[:minInt(4, len(payload))])
		return
	}
	encrypted := (payload[1] & 0x80) != 0
	if encrypted {
		if sess.crypto == nil {
			log.Printf("[PHOTON] encrypted message from %s but no crypto established", sess.addr)
			return
		}
		ciphertext := payload[2:]
		decrypted, err := sess.crypto.Decrypt(ciphertext)
		if err != nil {
			log.Printf("[PHOTON] decrypt error from %s: %v", "", err)
			return
		}
		log.Printf("[PHOTON] decrypt from %s: cipher=%x -> plain=%x", "", ciphertext[:minInt(32, len(ciphertext))], decrypted[:minInt(32, len(decrypted))])
		payload = append([]byte{0xF3, payload[1] & 0x7F}, decrypted...)
	}
	msgType := payload[1] & 0x7F
	switch msgType {
	case MsgInit:
		s.handleInit(sess, payload)
	case MsgOpRequest:
		s.handleOpRequest(sess, payload)
	case MsgInternalReq:
		s.handleInternalRequest(sess, payload)
	default:
		log.Printf("[PHOTON] unhandled msg type %d from %s", msgType, sess.addr)
	}
}
func (s *EnetServer) handleInit(sess *ClientSession, payload []byte) {
	if len(payload) >= 41 {
		appBytes := bytes.TrimRight(payload[9:41], "\x00")
		sess.appName = string(appBytes)
	}
	log.Printf("[PHOTON] INIT from %s appName=%q", "", sess.appName)
	cmd := s.reliableCmd(sess, buildInitOK())
	s.sendPacket(sess, cmd)
}
func (s *EnetServer) handleInternalRequest(sess *ClientSession, payload []byte) {
	if len(payload) < 3 {
		return
	}
	opCode := payload[2]
	if opCode == 0 {
		s.handleInitEncryption(sess, payload)
		return
	}
	resp := buildInternalOpResponse(opCode, -1, map[byte]any{})
	s.sendReliable(sess, resp)
}
func (s *EnetServer) handleInitEncryption(sess *ClientSession, payload []byte) {
	params, _, err := DecodeParams(payload, 3)
	if err != nil {
		log.Printf("[PHOTON] InitEncryption decode error: %v", err)
		resp := buildInternalOpResponse(0, -1, map[byte]any{})
		s.sendReliable(sess, resp)
		return
	}
	clientPubKey, ok := params[1].([]byte)
	if !ok || len(clientPubKey) == 0 {
		log.Printf("[PHOTON] InitEncryption missing client public key")
		resp := buildInternalOpResponse(0, -1, map[byte]any{})
		s.sendReliable(sess, resp)
		return
	}
	dh := NewDiffieHellmanCryptoProvider()
	dh.DeriveSharedKey(clientPubKey)
	sess.crypto = dh
	resp := buildInternalOpResponse(0, 0, map[byte]any{
		1: dh.PublicKey,
	})
	s.sendReliable(sess, resp)
}
var (
	clientDebugMu   sync.Mutex
	clientDebugSeen = map[string]int{}
)
func recordClientDebug(userID string, params map[byte]any) {
	msg := fmt.Sprintf("%v", params)
	clientDebugMu.Lock()
	defer clientDebugMu.Unlock()
	clientDebugSeen[msg]++
	if n := clientDebugSeen[msg]; n == 1 || n == 100 || n%1000 == 0 {
		f, err := os.OpenFile("logs/client_debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "%s user=%s count=%d %s\n\n", time.Now().Format(time.RFC3339), userID, n, msg)
	}
}
func (s *EnetServer) handleOpRequest(sess *ClientSession, payload []byte) {
	if len(payload) < 3 {
		return
	}
	opCode := payload[2]
	if opCode == OpRecordClientDebug {
		if params, _, err := DecodeParams(payload, 3); err == nil {
			recordClientDebug(sess.userID, params)
		}
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
		return
	}
	params := map[byte]any{}
	if len(payload) > 3 {
		var err error
		params, _, err = DecodeParams(payload, 3)
		if err != nil {
			log.Printf("[PHOTON] decode params op=%d: %v", opCode, err)
			log.Printf("[PHOTON] raw payload op=%d: %x", opCode, payload)
		}
	}
	if !(opCode == OpRaiseEvent && isNoisyRaiseEvent(params)) {
		log.Printf("[PHOTON] >>> op=%d user=%s params=%v", opCode, sess.userID, params)
	}
	if handler := s.opHandler(opCode); handler != nil {
		handler(sess, opCode, params)
	} else {
		log.Printf("[PHOTON] unknown op %d, returning OK", opCode)
		s.sendOpResponse(sess, opCode, 0, map[byte]any{})
	}
}
func (s *EnetServer) removeFromRoom(sess *ClientSession) {
	if sess.roomName == "" {
		return
	}
	room := s.rooms[sess.roomName]
	if room == nil {
		sess.roomName = ""
		return
	}
	s.saveTownPosition(sess, room)
	for _, other := range s.sessions {
		if other != sess && other.userID == sess.userID && other.roomName == sess.roomName && other.hostChar != nil {
			sess.roomName = ""
			sess.avatarBase = 0
			sess.hostChar = nil
			return
		}
	}
	actorNr := room.ActorNrFor(sess.userID)
	if actorNr != 0 {
		wasMaster := room.IsMasterActor(actorNr)
		room.RemovePlayer(actorNr)
		if sess.avatarBase != 0 {
			s.hostDestroyShieldEntity(room, sess)
			s.hostBroadcastJournaled(room, 204, map[byte]any{
				ParamPlayerNr:  HostActorNr,
				ParamEventData: map[any]any{byte(0): sess.avatarBase},
			}, true)
			for v := sess.avatarBase; v <= sess.avatarBase+5; v++ {
				delete(room.viewStreams, v)
			}
		}
		s.broadcastEvent(room, 254, map[byte]any{ParamPlayerNr: actorNr}, nil, nil)
		if newMaster := room.LowestActorNr(); wasMaster && newMaster != 0 {
			s.broadcastEvent(room, EvGameOwnerChanged, map[byte]any{ParamPlayerNr: newMaster}, nil, nil)
			log.Printf("[PHOTON] owner left room=%s ownership passed to actorNr=%d", room.Name, newMaster)
		}
		s.scriptFire(room, "OnPlayerLeave", lua.LNumber(actorNr))
	}
	if room.LowestActorNr() == 0 {
		room.scriptClose()
		delete(s.rooms, sess.roomName)
		log.Printf("[PHOTON] room=%s emptied, instance destroyed", sess.roomName)
	}
	sess.roomName = ""
	sess.avatarBase = 0
	sess.playerState = PlayerStateNone
	sess.hostChar = nil
	sess.gatedSeq = 0
	sess.streamsSeen = false
	sess.realPosSeen = false
	sess.realActionsSeen = false
}
func (s *EnetServer) sessionWatchdog() {
	for {
		time.Sleep(60 * time.Second)
		s.sessionWatchdogTick()
	}
}
func (s *EnetServer) sessionWatchdogTick() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PANIC] sessionWatchdog tick panicked, skipped: %v\n%s", r, debug.Stack())
		}
	}()
	s.mu.RLock()
	count := len(s.sessions)
	var detail strings.Builder
	for _, sess := range s.sessions {
		fmt.Fprintf(&detail, " [%s user=%s app=%q idle=%s]",
			sess.addr, sess.userID, sess.appName, time.Since(sess.lastRecv).Round(time.Second))
	}
	s.mu.RUnlock()
	if count > 0 {
		log.Printf("[PHOTON] active sessions: %d%s", count, detail.String())
	}
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
type EnetPeer struct {
	Addr            *net.UDPAddr
	PeerID          byte
	Protocol        uint16
	State           EnetPeerState
	Challenge       uint32
	MTU             uint16
	LastSeq         uint32
	ConnTime        time.Time
	AppVersion      string
	AppID           string
	Username        string
	AccountID       int64
	CharID          int64
	PlayerNr        int32
	GameMode        string
	RoomName        string
	MasterClient    bool
	IsOnlineService bool
	mu              sync.Mutex
	reliableQueue   []*CommandPacket
}
type EnetPeerState int
const (
	StateDisconnected  EnetPeerState = 0
	StateConnecting    EnetPeerState = 1
	StateConnected     EnetPeerState = 2
	StateDisconnecting EnetPeerState = 3
)
type CommandPacket struct {
	Type    byte
	Channel byte
	Flags   uint16
	SeqNum  uint32
	Body    []byte
}
func (p *EnetPeer) nextSeqNum() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.LastSeq++
	return p.LastSeq
}
type GameHandlers struct {
	AuthenticatePeer func(peer *EnetPeer, params map[byte]any) (map[byte]any, error)
	EventHandlers    map[byte]func(peer *EnetPeer, params map[byte]any, reliable bool) ([]*CommandPacket, error)
}
func (s *EnetServer) SendTo(addr *net.UDPAddr, data []byte) { s.sendRaw(addr, data) }
func (s *EnetServer) SendCommand(peer *EnetPeer, cmd *CommandPacket) {}
func (s *EnetServer) getPeerByAddr(addr *net.UDPAddr) *EnetPeer { return nil }
func (s *EnetServer) getPeerByID(id uint16) *EnetPeer { return nil }
func (s *EnetServer) removePeer(peer *EnetPeer) {}
