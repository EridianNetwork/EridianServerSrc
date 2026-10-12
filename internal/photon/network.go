package photon

import (
	"log"
	"net"
)

func GetLocalIP() string {
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		defer conn.Close()
		if udpAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok && !udpAddr.IP.IsLoopback() && !udpAddr.IP.IsLinkLocalUnicast() {
			return udpAddr.IP.String()
		}
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Printf("[NETWORK] failed to get interface addresses: %v", err)
		return "127.0.0.1"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && !ipnet.IP.IsLinkLocalUnicast() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	log.Printf("[NETWORK] no non-loopback IPv4 address found, using 127.0.0.1")
	return "127.0.0.1"
}
