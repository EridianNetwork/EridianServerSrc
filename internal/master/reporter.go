package master

import (
	"log"
	"net"
	"time"
)


type StatsFunc func() (name string, players, maxPlayers int)

type Reporter struct {
	conn       *net.UDPConn
	masterAddr *net.UDPAddr
	magicValue string
	gamePort   int
	stats      StatsFunc
	interval   time.Duration
	stopCh     chan struct{}
}

func NewReporter(masterAddress, magicValue string, gamePort int, interval time.Duration, stats StatsFunc) (*Reporter, error) {
	masterAddr, err := net.ResolveUDPAddr("udp", masterAddress)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Reporter{
		conn: conn, masterAddr: masterAddr, magicValue: magicValue, gamePort: gamePort, stats: stats, interval: interval,
		stopCh: make(chan struct{}),
	}, nil
}


func (r *Reporter) Run() {
	go r.readLoop()

	r.sendHeartbeat()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.sendHeartbeat()
		case <-r.stopCh:
			return
		}
	}
}

func (r *Reporter) Stop() {
	close(r.stopCh)
	r.conn.Close()
}

func (r *Reporter) sendHeartbeat() {
	if _, err := r.conn.WriteToUDP(buildHeartbeat(r.magicValue), r.masterAddr); err != nil {
		log.Printf("[MASTER-REPORT] heartbeat send failed: %v", err)
	}
}

func (r *Reporter) readLoop() {
	buf := make([]byte, 512)
	for {
		n, addr, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		msg, rest, ok := stripPrefix(buf[:n])
		if !ok || msg != msgGetInfo {
			continue
		}
		challenge := rest
		name, players, maxPlayers := r.stats()
		resp := buildInfoResponse(challenge, r.magicValue, ServerInfo{Port: r.gamePort, Name: name, Players: players, MaxPlayers: maxPlayers})
		if _, err := r.conn.WriteToUDP(resp, addr); err != nil {
			log.Printf("[MASTER-REPORT] infoResponse send failed: %v", err)
		}
	}
}
