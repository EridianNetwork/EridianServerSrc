package db

import "eridian/internal/domain"


func MaterialQuantity(charID int64, materialType int) (int, error) {
	var total int
	err := DB().QueryRow(
		`SELECT COALESCE(SUM(quantity), 0) FROM inventory WHERE char_id = ? AND item_kind = ? AND resource_id = ?`,
		charID, domain.ItemTypeMaterial, materialType,
	).Scan(&total)
	return total, err
}

func ConsumeMaterial(charID int64, materialType int, amount int) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT item_id, quantity FROM inventory WHERE char_id = ? AND item_kind = ? AND resource_id = ? ORDER BY slot`,
		charID, domain.ItemTypeMaterial, materialType,
	)
	if err != nil {
		return err
	}
	type stack struct {
		id  int64
		qty int
	}
	var stacks []stack
	for rows.Next() {
		var s stack
		if err := rows.Scan(&s.id, &s.qty); err != nil {
			rows.Close()
			return err
		}
		stacks = append(stacks, s)
	}
	rows.Close()

	for _, s := range stacks {
		if amount <= 0 {
			break
		}
		if s.qty <= amount {
			if _, err := tx.Exec("DELETE FROM inventory WHERE item_id = ?", s.id); err != nil {
				return err
			}
			amount -= s.qty
		} else {
			if _, err := tx.Exec("UPDATE inventory SET quantity = ? WHERE item_id = ?", s.qty-amount, s.id); err != nil {
				return err
			}
			amount = 0
		}
	}
	return tx.Commit()
}

// adds `amount` units of a material as new backpack stacks, can only stack 99 currently
func GrantMaterial(charID int64, materialType, amount, backpackSize int) error {
	for amount > 0 {
		qty := amount
		if qty > 99 {
			qty = 99
		}
		it := &domain.InventoryItem{
			CharID:     charID,
			ResourceID: materialType,
			Quantity:   qty,
			ItemKind:   domain.ItemTypeMaterial,
			UID:        newItemUID(),
		}
		if _, _, err := AddItemToBackpack(it, backpackSize); err != nil {
			return err
		}
		amount -= qty
	}
	return nil
}
