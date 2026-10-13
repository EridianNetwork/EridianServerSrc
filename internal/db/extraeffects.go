package db

import "fmt"

// ItemExtraRow is a weapon row that still needs its extra effects rolled.
type ItemExtraRow struct {
	ID     int64
	Rarity int
	Level  int
}

func extraTable(table string) (string, error) {
	if table != "inventory" && table != "warehouse" {
		return "", fmt.Errorf("db: unknown item table %q", table)
	}
	return table, nil
}

// ListWeaponsMissingExtras returns weapons of a rarity that carries extra effects but has none stored yet
// (items created before extra effects existed). rarities are ItemRarity values.
func ListWeaponsMissingExtras(table string, rarities []int) ([]ItemExtraRow, error) {
	table, err := extraTable(table)
	if err != nil {
		return nil, err
	}
	if len(rarities) == 0 {
		return nil, nil
	}
	in := ""
	args := make([]any, len(rarities))
	for i, r := range rarities {
		if i > 0 {
			in += ","
		}
		in += "?"
		args[i] = r
	}
	rows, err := DB().Query("SELECT item_id, rarity, level FROM "+table+
		" WHERE item_kind = 2 AND extra_effects = '' AND rarity IN ("+in+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemExtraRow
	for rows.Next() {
		var r ItemExtraRow
		if err := rows.Scan(&r.ID, &r.Rarity, &r.Level); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func SetItemExtraEffects(table string, itemID int64, extras string) error {
	table, err := extraTable(table)
	if err != nil {
		return err
	}
	_, err = DB().Exec("UPDATE "+table+" SET extra_effects = ? WHERE item_id = ?", extras, itemID)
	return err
}
