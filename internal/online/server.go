package online

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"eridian/internal/db"
)

// HandlerFunc processes a client request and returns response data.
type HandlerFunc func(s *ClientSession, data map[string]any) (map[string]any, error)

// OnlineServer handles TCP connections for game services.
type OnlineServer struct {
	host       string
	port       int
	maxConns   int
	PhotonHost string // Photon server advertised host
	PhotonPort int    // Photon server port
	handlers   map[ServiceCode]HandlerFunc

	mu      sync.Mutex
	clients map[*ClientSession]bool
	wg      sync.WaitGroup
}

// ClientSession represents an authenticated game client connection.
type ClientSession struct {
	conn      net.Conn
	server    *OnlineServer // reference to parent server
	AccountID int64         `json:"account_id"`
	CharID    int64         `json:"char_id"`
	Username  string        `json:"username"`
	buffer    PacketBuffer

	mu sync.Mutex
}

// NewServer creates a new OnlineServer.
func NewServer(host string, port int, maxConns int) *OnlineServer {
	s := &OnlineServer{
		host:     host,
		port:     port,
		maxConns: maxConns,
		handlers: make(map[ServiceCode]HandlerFunc),
		clients:  make(map[*ClientSession]bool),
	}
	s.registerDefaults()
	return s
}

// Handle registers a handler for the given service code.
func (s *OnlineServer) Handle(code ServiceCode, fn HandlerFunc) {
	s.handlers[code] = fn
}

func (s *OnlineServer) registerDefaults() {
	s.Handle(ServiceAuthValidate, handleAuthValidate)
	s.Handle(ServiceAuthGetTicket, handleAuthGetTicket)
	s.Handle(ServiceSessionPing, handleSessionPing)
	s.Handle(ServiceSessionPhotonAddr, handleSessionPhotonAddr)
	s.Handle(ServiceSessionServerList, handleSessionServerList)
}

// Serve starts the TCP listener and begins accepting connections.
func (s *OnlineServer) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("online: listen %s: %w", addr, err)
	}
	defer listener.Close()

	log.Printf("[ONLINE] listening on %s (max %d clients)", addr, s.maxConns)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("[ONLINE] accept error: %v", err)
			continue
		}

		s.mu.Lock()
		if len(s.clients) >= s.maxConns {
			s.mu.Unlock()
			conn.Close()
			log.Printf("[ONLINE] rejected connection: max clients (%d) reached", s.maxConns)
			continue
		}
		s.mu.Unlock()

		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// Shutdown gracefully closes all client connections and stops the server.
func (s *OnlineServer) Shutdown() {
	s.mu.Lock()
	for c := range s.clients {
		c.conn.Close()
	}
	s.clients = nil
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *OnlineServer) handleConnection(conn net.Conn) {
	defer s.wg.Done()

	if db.IsIPBanned(conn.RemoteAddr().String()) {
		log.Printf("[ONLINE] connection from banned IP %s dropped", conn.RemoteAddr())
		conn.Close()
		return
	}

	client := &ClientSession{
		conn:   conn,
		server: s,
	}

	s.mu.Lock()
	s.clients[client] = true
	clientCount := len(s.clients)
	s.mu.Unlock()

	log.Printf("[ONLINE] client connected: %s (%d active)", conn.RemoteAddr(), clientCount)

	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.clients, client)
		remaining := len(s.clients)
		s.mu.Unlock()
		log.Printf("[ONLINE] client disconnected: %s (%d active)", conn.RemoteAddr(), remaining)
	}()

	for {
		pkt, err := client.buffer.ReadPacket(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
				log.Printf("[ONLINE] read error from %s: %v", conn.RemoteAddr(), err)
			}
			return
		}
		if pkt == nil {
			continue // need more data
		}

		s.dispatch(client, pkt)
	}
}

func (s *OnlineServer) dispatch(c *ClientSession, pkt *Packet) {
	fn, ok := s.handlers[pkt.Service]
	if !ok {
		log.Printf("[ONLINE] unknown service 0x%02X from %s", pkt.Service, c.conn.RemoteAddr())
		c.Send(pkt.Service, ResultError, map[string]any{"error": "unknown_service"})
		return
	}

	data, err := fn(c, pkt.Data)
	if err != nil {
		log.Printf("[ONLINE] handler error for service 0x%02X: %v", pkt.Service, err)
		c.Send(pkt.Service, ResultError, map[string]any{"error": err.Error()})
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	c.Send(pkt.Service, ResultOK, data)
}

// Send writes a packet to the client connection.
func (c *ClientSession) Send(service ServiceCode, result ResultCode, data map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pkt, err := EncodePacket(service, result, data)
	if err != nil {
		log.Printf("[ONLINE] encode error: %v", err)
		return
	}
	if _, err := c.conn.Write(pkt); err != nil {
		log.Printf("[ONLINE] write error to %s: %v", c.conn.RemoteAddr(), err)
	}
}

// SendRaw writes raw JSON (already encoded) to the client.
func (c *ClientSession) SendRaw(service ServiceCode, result ResultCode, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	payload := map[string]any{
		"service": int32(service),
		"result":  int32(result),
		"data":    v,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[ONLINE] marshal error: %v", err)
		return
	}
	pkt := make([]byte, 4+len(body))
	pkt[0] = byte(len(body) >> 24)
	pkt[1] = byte(len(body) >> 16)
	pkt[2] = byte(len(body) >> 8)
	pkt[3] = byte(len(body))
	copy(pkt[4:], body)

	if _, err := c.conn.Write(pkt); err != nil {
		log.Printf("[ONLINE] write error to %s: %v", c.conn.RemoteAddr(), err)
	}
}

func handleAuthValidate(s *ClientSession, data map[string]any) (map[string]any, error) {
	username, _ := data["username"].(string)
	password, _ := data["password"].(string)

	// Normalize: also check "user" / "pass" keys
	if username == "" {
		if u, ok := data["user"].(string); ok {
			username = u
		}
	}
	if password == "" {
		if p, ok := data["pass"].(string); ok {
			password = p
		}
	}

	if username == "" {
		return nil, fmt.Errorf("username_required")
	}

	// GetOrCreateAccount: create account if it doesn't exist
	account, err := db.GetAccountByUsername(username)
	if err != nil {
		// Create new account
		id, createErr := db.CreateAccount(username, password, "")
		if createErr != nil {
			return nil, fmt.Errorf("create_account: %w", createErr)
		}
		account, err = db.GetAccountByID(id)
		if err != nil {
			return nil, fmt.Errorf("get_new_account: %w", err)
		}
	} else if account.IsBanned {
		return nil, fmt.Errorf("account_banned")
	} else if !account.CanPlay {
		return nil, fmt.Errorf("%s", account.CanPlayRejectMessage())
	}

	s.AccountID = account.AccountID
	s.Username = account.Username

	return map[string]any{
		"account_id": account.AccountID,
		"username":   account.Username,
		"token":      account.Token,
	}, nil
}

// handleAuthGetTicket issues a session ticket.
func handleAuthGetTicket(s *ClientSession, data map[string]any) (map[string]any, error) {
	// Simple ticket: just return a confirmation with the account info
	return map[string]any{
		"account_id": s.AccountID,
		"username":   s.Username,
		"ticket":     fmt.Sprintf("ticket_%d", s.AccountID),
	}, nil
}

// handleSessionPing responds to a keepalive ping.
func handleSessionPing(s *ClientSession, data map[string]any) (map[string]any, error) {
	return map[string]any{"pong": true}, nil
}

// handleSessionPhotonAddr returns the Photon server address.
func handleSessionPhotonAddr(s *ClientSession, data map[string]any) (map[string]any, error) {
	host := s.server.PhotonHost
	if host == "" {
		host = "127.0.0.1"
	}
	port := s.server.PhotonPort
	if port == 0 {
		port = 5056
	}
	return map[string]any{
		"host":     host,
		"port":     port,
		"protocol": "udp",
		"app_id":   "BorderlandsOnline",
	}, nil
}

// handleSessionServerList returns the available game servers.
func handleSessionServerList(s *ClientSession, data map[string]any) (map[string]any, error) {
	host := s.server.PhotonHost
	if host == "" {
		host = "127.0.0.1"
	}
	port := s.server.PhotonPort
	if port == 0 {
		port = 5056
	}
	return map[string]any{
		"servers": []map[string]any{
			{
				"name":   "Local Server",
				"host":   host,
				"port":   port,
				"region": "local",
				"online": true,
			},
		},
	}, nil
}
