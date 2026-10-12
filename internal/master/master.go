package master

import (
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)


const challengeTimeout = 5 * time.Second

const defaultStaleServerTimeout = 90 * time.Second

const maxPruneInterval = 15 * time.Second

type registeredServer struct {
	info      ServerInfo
	lastSeen  time.Time
	firstSeen time.Time // set once, on first registration. is not bumped by later heartbeat renewals
}

type pendingChallenge struct {
	challenge string
	sentAt    time.Time
}

type Master struct {
	conn *net.UDPConn
	// magicValue is the shared secret every heartbeat/infoResponse must carry
	magicValue string

	staleTimeout time.Duration

	mu         sync.Mutex
	servers    map[string]*registeredServer // addr.String() server
	challenges map[string]*pendingChallenge // addr.String() challenge awaiting infoResponse
}

func New(host string, port int, magicValue string, staleTimeout time.Duration) (*Master, error) {
	addr := &net.UDPAddr{IP: net.ParseIP(host), Port: port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	if staleTimeout <= 0 {
		staleTimeout = defaultStaleServerTimeout
	}
	return &Master{
		conn:         conn,
		magicValue:   magicValue,
		staleTimeout: staleTimeout,
		servers:      map[string]*registeredServer{},
		challenges:   map[string]*pendingChallenge{},
	}, nil
}

func (m *Master) Serve() error {
	go m.pruneLoop()

	buf := make([]byte, 2048)
	for {
		n, addr, err := m.conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		go m.handlePacket(pkt, addr)
	}
}

func (m *Master) Close() error {
	return m.conn.Close()
}

func (m *Master) Addr() *net.UDPAddr {
	return m.conn.LocalAddr().(*net.UDPAddr)
}

func (m *Master) handlePacket(pkt []byte, addr *net.UDPAddr) {
	msg, rest, ok := stripPrefix(pkt)
	if !ok {
		return
	}
	switch msg {
	case msgHeartbeat:
		m.onHeartbeat(rest, addr)
	case msgInfoResponse:
		m.onInfoResponse(rest, addr)
	case msgGetServers:
		m.onGetServers(rest, addr)
	}
}

func (m *Master) onHeartbeat(body string, addr *net.UDPAddr) {
	kv := parseKV(strings.TrimLeft(body, "\n"))
	if m.magicValue != "" && kv["magic"] != m.magicValue {
		return
	}
	challenge, err := newChallenge()
	if err != nil {
		log.Printf("[MASTER] challenge generation failed: %v", err)
		return
	}
	m.mu.Lock()
	m.challenges[addr.String()] = &pendingChallenge{challenge: challenge, sentAt: time.Now()}
	m.mu.Unlock()
	if _, err := m.conn.WriteToUDP(buildGetInfo(challenge), addr); err != nil {
		log.Printf("[MASTER] getinfo send to %s failed: %v", addr, err)
	}
}

func (m *Master) onInfoResponse(body string, addr *net.UDPAddr) {
	kv := parseKV(strings.TrimLeft(body, "\n"))
	if m.magicValue != "" && kv["magic"] != m.magicValue {
		return
	}
	key := addr.String()

	m.mu.Lock()
	pending, ok := m.challenges[key]
	if !ok || time.Since(pending.sentAt) > challengeTimeout || kv["challenge"] != pending.challenge {
		m.mu.Unlock()
		return // no outstanding (or expired, or mismatched) challenge, ignore, matches real DPMasters antispoof behavior
	}
	delete(m.challenges, key)
	players, _ := strconv.Atoi(kv["clients"])
	maxPlayers, _ := strconv.Atoi(kv["sv_maxclients"])

	port := addr.Port
	if p, err := strconv.Atoi(kv["port"]); err == nil {
		port = p
	}
	info := ServerInfo{
		IP:         addr.IP.String(),
		Port:       port,
		Name:       kv["hostname"],
		Players:    players,
		MaxPlayers: maxPlayers,
	}
	firstSeen := time.Now()
	if existing, ok := m.servers[key]; ok {
		firstSeen = existing.firstSeen
	}
	m.servers[key] = &registeredServer{info: info, lastSeen: time.Now(), firstSeen: firstSeen}
	m.mu.Unlock()
	log.Printf("[MASTER] registered server %s (%q) %d/%d players", key, info.Name, info.Players, info.MaxPlayers)
}

func (m *Master) onGetServers(rest string, addr *net.UDPAddr) {
	if m.magicValue != "" {
		parts := strings.Fields(rest)
		magic := ""
		if len(parts) >= 2 {
			magic = parts[1]
		}
		if magic != m.magicValue {
			return
		}
	}

	now := time.Now()
	m.mu.Lock()
	list := make([]ServerInfo, 0, len(m.servers))
	for _, s := range m.servers {
		info := s.info
		info.UptimeSeconds = int(now.Sub(s.firstSeen).Seconds())
		list = append(list, info)
	}
	m.mu.Unlock()
	if _, err := m.conn.WriteToUDP(buildGetServersResponse(list), addr); err != nil {
		log.Printf("[MASTER] getserversResponse send to %s failed: %v", addr, err)
	}
}

func (m *Master) pruneLoop() {
	interval := m.staleTimeout / 3
	if interval > maxPruneInterval {
		interval = maxPruneInterval
	}
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		m.pruneOnce(time.Now())
	}
}


func (m *Master) pruneOnce(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, s := range m.servers {
		if now.Sub(s.lastSeen) > m.staleTimeout {
			delete(m.servers, key)
			log.Printf("[MASTER] server %s (%q) timed out, removed", key, s.info.Name)
		}
	}
	for key, c := range m.challenges {
		if now.Sub(c.sentAt) > challengeTimeout {
			delete(m.challenges, key)
		}
	}
}
