// Package online implements the Borderlands Online game service protocol.
//
// Protocol: 4-byte big-endian length prefix + JSON payload (client ↔ server).
package online

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// ServiceCode identifies the game service operation.
type ServiceCode int32

const (
	// Auth / account
	ServiceAuthValidate  ServiceCode = 0x01
	ServiceAuthGetTicket ServiceCode = 0x02

	// Character service
	ServiceCharList   ServiceCode = 0x10
	ServiceCharCreate ServiceCode = 0x11
	ServiceCharSelect ServiceCode = 0x12
	ServiceCharDelete ServiceCode = 0x13
	ServiceCharSave   ServiceCode = 0x14
	ServiceCharGet    ServiceCode = 0x15

	// Inventory service
	ServiceInvGet          ServiceCode = 0x20
	ServiceInvAdd          ServiceCode = 0x21
	ServiceInvRemove       ServiceCode = 0x22
	ServiceInvEquip        ServiceCode = 0x23
	ServiceInvSwap         ServiceCode = 0x24
	ServiceInvGetWarehouse ServiceCode = 0x25

	// Lobby service (PvE)
	ServiceLobbyCreate ServiceCode = 0x30
	ServiceLobbyJoin   ServiceCode = 0x31
	ServiceLobbyLeave  ServiceCode = 0x32
	ServiceLobbyList   ServiceCode = 0x33
	ServiceLobbyGet    ServiceCode = 0x34
	ServiceLobbyInvite ServiceCode = 0x35
	ServiceLobbyStart  ServiceCode = 0x36

	// PvP room service
	ServicePvPCreate ServiceCode = 0x40
	ServicePvPJoin   ServiceCode = 0x41
	ServicePvPLeave  ServiceCode = 0x42
	ServicePvPList   ServiceCode = 0x43
	ServicePvPFind   ServiceCode = 0x44

	// Chat service
	ServiceChatSend      ServiceCode = 0x50
	ServiceChatGet       ServiceCode = 0x51
	ServiceChatChanJoin  ServiceCode = 0x52
	ServiceChatChanLeave ServiceCode = 0x53

	// Friend service
	ServiceFriendList   ServiceCode = 0x60
	ServiceFriendAdd    ServiceCode = 0x61
	ServiceFriendAccept ServiceCode = 0x62
	ServiceFriendRemove ServiceCode = 0x63

	// Session service
	ServiceSessionPing       ServiceCode = 0x70
	ServiceSessionPhotonAddr ServiceCode = 0x71
	ServiceSessionServerList ServiceCode = 0x72
)

// ResultCode indicates operation result status.
type ResultCode int32

const (
	ResultOK            ResultCode = 0
	ResultError         ResultCode = 1
	ResultInvalidParam  ResultCode = 2
	ResultNotFound      ResultCode = 3
	ResultFull          ResultCode = 4
	ResultUnauthorized  ResultCode = 5
	ResultAlreadyExists ResultCode = 6
	ResultNotAllowed    ResultCode = 7
)

// Packet is a parsed online service packet.
type Packet struct {
	Service ServiceCode
	Result  ResultCode
	Data    map[string]any
}

// EncodePacket serializes a service response: 4-byte length prefix + JSON.
func EncodePacket(service ServiceCode, result ResultCode, data map[string]any) ([]byte, error) {
	payload := map[string]any{
		"service": int32(service),
		"result":  int32(result),
	}
	for k, v := range data {
		payload[k] = v
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode packet: %w", err)
	}

	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(body)))

	out := make([]byte, 0, 4+len(body))
	out = append(out, header...)
	out = append(out, body...)
	return out, nil
}

func MustEncodePacket(service ServiceCode, result ResultCode, data map[string]any) []byte {
	b, err := EncodePacket(service, result, data)
	if err != nil {
		panic(err)
	}
	return b
}

// PacketBuffer handles length-prefixed reads from a TCP stream.
type PacketBuffer struct {
	header  [4]byte
	headerN int
	body    []byte
	bodyN   int
}

func (pb *PacketBuffer) ReadPacket(r io.Reader) (*Packet, error) {
	// Still reading the 4-byte header
	if pb.headerN < 4 {
		n, err := r.Read(pb.header[pb.headerN:])
		if err != nil {
			return nil, err
		}
		pb.headerN += n
		if pb.headerN < 4 {
			return nil, nil // need more header bytes
		}
		length := binary.BigEndian.Uint32(pb.header[:])
		if length > 1<<20 { // 1 MB max
			pb.Reset()
			return nil, fmt.Errorf("packet too large: %d bytes", length)
		}
		pb.body = make([]byte, length)
		pb.bodyN = 0
	}

	// Read the body
	if pb.bodyN < len(pb.body) {
		n, err := r.Read(pb.body[pb.bodyN:])
		if err != nil {
			return nil, err
		}
		pb.bodyN += n
		if pb.bodyN < len(pb.body) {
			return nil, nil // need more body bytes
		}
	}

	// Complete packet received
	pkt := &Packet{}
	if err := json.Unmarshal(pb.body, pkt); err != nil {
		pb.Reset()
		return nil, fmt.Errorf("decode packet: %w", err)
	}

	// Also decode into Data map
	var raw map[string]any
	if err := json.Unmarshal(pb.body, &raw); err == nil {
		pkt.Data = raw
	}

	pb.Reset()
	return pkt, nil
}

// Reset clears the buffer state for a new packet.
func (pb *PacketBuffer) Reset() {
	pb.headerN = 0
	pb.body = nil
	pb.bodyN = 0
}

// UnmarshalJSON implements json.Unmarshaler for Packet.
func (p *Packet) UnmarshalJSON(data []byte) error {
	var raw struct {
		Service int32          `json:"service"`
		Result  int32          `json:"result"`
		Extra   map[string]any `json:"-"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.Service = ServiceCode(raw.Service)
	p.Result = ResultCode(raw.Result)
	// Everything besides service/result goes into Data
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	delete(m, "service")
	delete(m, "result")
	p.Data = m
	return nil
}
