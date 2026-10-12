package db

import "eridian/internal/domain"

// this is pretty much useless since we don't award keys anywhere
func LuckyKeyQuantity(charID int64, keyRank int) (int, error) {
	var total int
	err := DB().QueryRow(
		`SELECT COALESCE(SUM(quantity), 0) FROM inventory WHERE char_id = ? AND item_kind = ? AND resource_id = ?`,
		charID, domain.ItemTypeLuckyKey, keyRank,
	).Scan(&total)
	return total, err
}

// same as above unless you ever wanna make something out of this.
func GrantLuckyKey(charID int64, keyRank, amount, backpackSize int) error {
	for amount > 0 {
		qty := min(amount, 99)
		it := &domain.InventoryItem{
			CharID:     charID,
			ResourceID: keyRank,
			Quantity:   qty,
			ItemKind:   domain.ItemTypeLuckyKey,
			UID:        newItemUID(),
		}
		if _, _, err := AddItemToBackpack(it, backpackSize); err != nil {
			return err
		}
		amount -= qty
	}
	return nil
}

// use on opening whichever tier
func ConsumeLuckyKey(charID int64, keyRank int) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var itemID int64
	var qty int
	err = tx.QueryRow(
		`SELECT item_id, quantity FROM inventory WHERE char_id = ? AND item_kind = ? AND resource_id = ? ORDER BY slot LIMIT 1`,
		charID, domain.ItemTypeLuckyKey, keyRank,
	).Scan(&itemID, &qty)
	if err != nil {
		return err
	}
	if qty <= 1 {
		if _, err := tx.Exec("DELETE FROM inventory WHERE item_id = ?", itemID); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec("UPDATE inventory SET quantity = ? WHERE item_id = ?", qty-1, itemID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
