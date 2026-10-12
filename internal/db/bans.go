package db

import "net"

func IsIPBanned(addr string) bool {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	var n int
	return DB().QueryRow("SELECT COUNT(*) FROM banned_ips WHERE ip = ?", addr).Scan(&n) == nil && n > 0
}

func BanIP(ip, reason string) error {
	_, err := DB().Exec("INSERT OR REPLACE INTO banned_ips (ip, reason) VALUES (?, ?)", ip, reason)
	return err
}

func UnbanIP(ip string) error {
	_, err := DB().Exec("DELETE FROM banned_ips WHERE ip = ?", ip)
	return err
}

type BannedIP struct {
	IP        string `json:"ip"`
	Reason    string `json:"reason"`
	CreatedAt string `json:"createdAt"`
}

func ListBannedIPs() ([]BannedIP, error) {
	rows, err := DB().Query("SELECT ip, reason, created_at FROM banned_ips ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []BannedIP{}
	for rows.Next() {
		var b BannedIP
		if err := rows.Scan(&b.IP, &b.Reason, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
