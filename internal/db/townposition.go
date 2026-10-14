package db

// Last position inside a town, restored as the spawn point the next time the character enters that town.
// town_pos_playlist_id is the town it belongs to (0 = nothing saved).

type TownPosition struct {
	PlaylistID int32
	X, Y, Z    float64
	Yaw        float64
}

func SaveCharacterTownPosition(charID int64, p TownPosition) error {
	_, err := DB().Exec(`UPDATE characters SET town_pos_playlist_id=?, town_pos_x=?, town_pos_y=?, town_pos_z=?, town_pos_yaw=?
		WHERE char_id=?`, p.PlaylistID, p.X, p.Y, p.Z, p.Yaw, charID)
	return err
}

func GetCharacterTownPosition(charID int64) (TownPosition, error) {
	var p TownPosition
	err := DB().QueryRow(`SELECT town_pos_playlist_id, town_pos_x, town_pos_y, town_pos_z, town_pos_yaw
		FROM characters WHERE char_id=?`, charID).Scan(&p.PlaylistID, &p.X, &p.Y, &p.Z, &p.Yaw)
	return p, err
}
