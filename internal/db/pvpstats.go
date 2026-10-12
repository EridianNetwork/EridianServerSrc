package db

import (
	"database/sql"

	"eridian/internal/domain"
)


func GetPvPStats(charID int64) (*domain.PvPStats, error) {
	row := DB().QueryRow(`SELECT char_id, matches, wins, losses, kills, deaths, honor, rating FROM pvp_stats WHERE char_id = ?`, charID)
	var st domain.PvPStats
	if err := row.Scan(&st.CharID, &st.Matches, &st.Wins, &st.Losses, &st.Kills, &st.Deaths, &st.Honor, &st.Rating); err != nil {
		if err == sql.ErrNoRows {
			return &domain.PvPStats{CharID: charID}, nil
		}
		return nil, err
	}
	return &st, nil
}


func RecordPvPMatchResult(charID int64, won, lost bool, kills, deaths, honorDelta, ratingDelta int32) error {
	winInc, lossInc := 0, 0
	if won {
		winInc = 1
	}
	if lost {
		lossInc = 1
	}
	_, err := DB().Exec(`
		INSERT INTO pvp_stats (char_id, matches, wins, losses, kills, deaths, honor, rating)
		VALUES (?, 1, ?, ?, ?, ?, ?, MAX(0, 1000 + ?))
		ON CONFLICT(char_id) DO UPDATE SET
			matches = matches + 1,
			wins = wins + excluded.wins,
			losses = losses + excluded.losses,
			kills = kills + excluded.kills,
			deaths = deaths + excluded.deaths,
			honor = honor + excluded.honor,
			rating = MAX(0, rating + ?)`,
		charID, winInc, lossInc, kills, deaths, honorDelta, ratingDelta, ratingDelta)
	return err
}

func AddCharacterHonor(charID int64, delta int32) error {
	if delta >= 0 {
		_, err := DB().Exec("UPDATE pvp_stats SET honor = honor + ? WHERE char_id = ?", delta, charID)
		return err
	}
	res, err := DB().Exec("UPDATE pvp_stats SET honor = honor + ? WHERE char_id = ? AND honor >= ?", delta, charID, -delta)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInsufficientFunds
	}
	return nil
}
