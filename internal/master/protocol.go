
package master

import (
	"crypto/rand"
	"encoding/base32"
	"strconv"
	"strings"
)

const packetPrefix = "\xFF\xFF\xFF\xFF"

const (
	msgHeartbeat          = "heartbeat"
	msgGetInfo            = "getinfo"
	msgInfoResponse       = "infoResponse"
	msgGetServers         = "getservers"
	msgGetServersResponse = "getserversResponse"
)

const GameName = "BorderlandsOnline"

type ServerInfo struct {
	IP            string
	Port          int
	Name          string
	Players       int
	MaxPlayers    int
	UptimeSeconds int
}

func newChallenge() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

func buildHeartbeat(magic string) []byte {
	var b strings.Builder
	b.WriteString(packetPrefix)
	b.WriteString(msgHeartbeat)
	b.WriteByte('\n')
	writeKV(&b, "gamename", GameName)
	writeKV(&b, "magic", magic)
	return []byte(b.String())
}

func buildGetInfo(challenge string) []byte {
	return []byte(packetPrefix + msgGetInfo + " " + challenge)
}

func buildInfoResponse(challenge, magic string, info ServerInfo) []byte {
	var b strings.Builder
	b.WriteString(packetPrefix)
	b.WriteString(msgInfoResponse)
	b.WriteByte('\n')
	writeKV(&b, "challenge", challenge)
	writeKV(&b, "magic", magic)
	writeKV(&b, "gamename", GameName)
	writeKV(&b, "port", strconv.Itoa(info.Port))
	writeKV(&b, "hostname", info.Name)
	writeKV(&b, "clients", strconv.Itoa(info.Players))
	writeKV(&b, "sv_maxclients", strconv.Itoa(info.MaxPlayers))
	return []byte(b.String())
}

func buildGetServers(magic string) []byte {
	return []byte(packetPrefix + msgGetServers + " " + GameName + " " + magic)
}


func buildGetServersResponse(servers []ServerInfo) []byte {
	var b strings.Builder
	b.WriteString(packetPrefix)
	b.WriteString(msgGetServersResponse)
	b.WriteByte('\n')
	for _, s := range servers {
		writeKV(&b, "ip", s.IP)
		writeKV(&b, "port", strconv.Itoa(s.Port))
		writeKV(&b, "hostname", s.Name)
		writeKV(&b, "clients", strconv.Itoa(s.Players))
		writeKV(&b, "sv_maxclients", strconv.Itoa(s.MaxPlayers))
		writeKV(&b, "uptime", strconv.Itoa(s.UptimeSeconds))
		b.WriteByte('\n')
	}
	b.WriteString(packetPrefix)
	b.WriteString("EOT")
	return []byte(b.String())
}

func writeKV(b *strings.Builder, key, val string) {
	b.WriteByte('\\')
	b.WriteString(key)
	b.WriteByte('\\')
	b.WriteString(val)
}

func parseKV(line string) map[string]string {
	line = strings.TrimPrefix(line, "\\")
	parts := strings.Split(line, "\\")
	out := make(map[string]string, len(parts)/2)
	for i := 0; i+1 < len(parts); i += 2 {
		out[parts[i]] = parts[i+1]
	}
	return out
}

func stripPrefix(pkt []byte) (msg, rest string, ok bool) {
	s := string(pkt)
	if !strings.HasPrefix(s, packetPrefix) {
		return "", "", false
	}
	s = strings.TrimPrefix(s, packetPrefix)
	if i := strings.IndexAny(s, " \n"); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", true
}
