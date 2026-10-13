package db

import (
	"database/sql"
	"errors"

	"eridian/internal/domain"
)

const whCols = `item_id, account_id, slot, resource_id, quantity, flags, rarity,
       damage, accuracy, fire_rate, reload_speed, magazine,
       element_type, element_damage, element_chance, parts, seed,
       is_weapon, item_kind, level, uid, item_name,
       generation_source, price, expiration, owner_id,
       part_a, part_b, part_c, part_d, part_e, part_f, description,
       star_level, star_experience, subparts, extra_effects`


func scanWhItem(row interface{ Scan(...any) error }) (*domain.InventoryItem, error) {
	it := &domain.InventoryItem{}
	var accountID int64
	err := row.Scan(
		&it.ItemID, &accountID, &it.Slot, &it.ResourceID, &it.Quantity, &it.Flags, &it.Rarity,
		&it.Damage, &it.Accuracy, &it.FireRate, &it.ReloadSpeed, &it.Magazine,
		&it.ElementType, &it.ElementDmg, &it.ElementChance, &it.Parts, &it.Seed,
		&it.IsWeapon, &it.ItemKind, &it.Level, &it.UID, &it.ItemName,
		&it.GenerationSource, &it.Price, &it.Expiration, &it.OwnerID,
		&it.PartA, &it.PartB, &it.PartC, &it.PartD, &it.PartE, &it.PartF, &it.Description,
		&it.StarLevel, &it.StarExperience, &it.Subparts, &it.ExtraEffects)
	if err != nil {
		return nil, err
	}
	return it, nil
}

func GetWarehouseItems(accountID int64) ([]*domain.InventoryItem, error) {
	rows, err := DB().Query(`SELECT `+whCols+` FROM warehouse WHERE account_id = ? ORDER BY slot`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*domain.InventoryItem
	for rows.Next() {
		it, err := scanWhItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}


func GetWarehouseItemAt(accountID int64, slot int) (*domain.InventoryItem, error) {
	row := DB().QueryRow(`SELECT `+whCols+` FROM warehouse WHERE account_id = ? AND slot = ?`, accountID, slot)
	it, err := scanWhItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrItemNotFound
	}
	return it, err
}

func AddWarehouseItemFull(accountID int64, slot int, it *domain.InventoryItem) (int64, error) {
	tx, err := DB().Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := txInsertWarehouseItem(tx, accountID, slot, it); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	added, err := GetWarehouseItemAt(accountID, slot)
	if err != nil {
		return 0, err
	}
	return added.ItemID, nil
}

func txInsertWarehouseItem(tx *sql.Tx, accountID int64, slot int, it *domain.InventoryItem) error {
	_, err := tx.Exec(`
		INSERT INTO warehouse (account_id, slot, resource_id, quantity, flags, rarity,
		                       damage, accuracy, fire_rate, reload_speed, magazine,
		                       element_type, element_damage, element_chance, parts, seed,
		                       is_weapon, item_kind, level, uid, item_name,
		                       generation_source, price, expiration, owner_id,
		                       part_a, part_b, part_c, part_d, part_e, part_f, description,
		                       star_level, star_experience, subparts, extra_effects)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		accountID, slot, it.ResourceID, it.Quantity, it.Flags, it.Rarity,
		it.Damage, it.Accuracy, it.FireRate, it.ReloadSpeed, it.Magazine,
		it.ElementType, it.ElementDmg, it.ElementChance, it.Parts, it.Seed,
		it.IsWeapon, it.ItemKind, it.Level, it.UID, it.ItemName,
		it.GenerationSource, it.Price, it.Expiration, it.OwnerID,
		it.PartA, it.PartB, it.PartC, it.PartD, it.PartE, it.PartF, it.Description,
		it.StarLevel, it.StarExperience, it.Subparts, it.ExtraEffects)
	return err
}

func txInsertInventoryItem(tx *sql.Tx, charID int64, slot int, it *domain.InventoryItem) error {
	_, err := tx.Exec(`
		INSERT INTO inventory (char_id, slot, resource_id, quantity, flags, rarity,
		                       damage, accuracy, fire_rate, reload_speed, magazine,
		                       element_type, element_damage, element_chance, parts, seed,
		                       is_equipped, is_weapon, item_kind, level, uid, item_name,
		                       generation_source, price, expiration, owner_id,
		                       part_a, part_b, part_c, part_d, part_e, part_f, description,
		                       star_level, star_experience, subparts, extra_effects)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		charID, slot, it.ResourceID, it.Quantity, it.Flags, it.Rarity,
		it.Damage, it.Accuracy, it.FireRate, it.ReloadSpeed, it.Magazine,
		it.ElementType, it.ElementDmg, it.ElementChance, it.Parts, it.Seed,
		false, it.IsWeapon, it.ItemKind, it.Level, it.UID, it.ItemName,
		it.GenerationSource, it.Price, it.Expiration, it.OwnerID,
		it.PartA, it.PartB, it.PartC, it.PartD, it.PartE, it.PartF, it.Description,
		it.StarLevel, it.StarExperience, it.Subparts, it.ExtraEffects)
	return err
}


func SwapWarehouseSlots(accountID int64, slotA, slotB byte) error {
	if slotA == slotB {
		return nil
	}
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	a, errA := GetWarehouseItemAt(accountID, int(slotA))
	if errA != nil && !errors.Is(errA, ErrItemNotFound) {
		return errA
	}
	b, errB := GetWarehouseItemAt(accountID, int(slotB))
	if errB != nil && !errors.Is(errB, ErrItemNotFound) {
		return errB
	}
	if a != nil {
		if _, err := tx.Exec("UPDATE warehouse SET slot = ? WHERE item_id = ?", slotB, a.ItemID); err != nil {
			return err
		}
	}
	if b != nil {
		if _, err := tx.Exec("UPDATE warehouse SET slot = ? WHERE item_id = ?", slotA, b.ItemID); err != nil {
			return err
		}
	}
	return tx.Commit()
}


func SwapInventoryWarehouseSlot(charID, accountID int64, invSlot, whSlot int) error {
	invItem, err := GetInventoryItemAt(charID, invSlot, false)
	if err != nil && !errors.Is(err, ErrItemNotFound) {
		return err
	}
	whItem, err := GetWarehouseItemAt(accountID, whSlot)
	if err != nil && !errors.Is(err, ErrItemNotFound) {
		return err
	}

	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if invItem != nil {
		if _, err := tx.Exec("DELETE FROM inventory WHERE item_id = ?", invItem.ItemID); err != nil {
			return err
		}
	}
	if whItem != nil {
		if _, err := tx.Exec("DELETE FROM warehouse WHERE item_id = ?", whItem.ItemID); err != nil {
			return err
		}
	}
	if invItem != nil {
		if err := txInsertWarehouseItem(tx, accountID, whSlot, invItem); err != nil {
			return err
		}
	}
	if whItem != nil {
		if err := txInsertInventoryItem(tx, charID, invSlot, whItem); err != nil {
			return err
		}
	}
	return tx.Commit()
}
