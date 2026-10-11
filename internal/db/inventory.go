package db

import (
	"database/sql"
	"errors"
	"math/rand"

	"eridian/internal/domain"
)


var ErrSlotOccupied = errors.New("destination slot occupied")
var ErrItemNotFound = errors.New("item not found")

// returned when there's no free backpack slot to place an item into
var ErrInventoryFull = errors.New("inventory full")

const invCols = `item_id, char_id, slot, resource_id, quantity, flags, rarity,
       damage, accuracy, fire_rate, reload_speed, magazine,
       element_type, element_damage, element_chance, parts, seed,
       is_equipped, is_weapon, item_kind, level, uid, item_name,
       generation_source, price, expiration, owner_id,
       part_a, part_b, part_c, part_d, part_e, part_f, description,
       star_level, star_experience, subparts`

func scanInvItem(row interface{ Scan(...any) error }) (*domain.InventoryItem, error) {
	it := &domain.InventoryItem{}
	err := row.Scan(
		&it.ItemID, &it.CharID, &it.Slot, &it.ResourceID, &it.Quantity, &it.Flags, &it.Rarity,
		&it.Damage, &it.Accuracy, &it.FireRate, &it.ReloadSpeed, &it.Magazine,
		&it.ElementType, &it.ElementDmg, &it.ElementChance, &it.Parts, &it.Seed,
		&it.IsEquipped, &it.IsWeapon, &it.ItemKind, &it.Level, &it.UID, &it.ItemName,
		&it.GenerationSource, &it.Price, &it.Expiration, &it.OwnerID,
		&it.PartA, &it.PartB, &it.PartC, &it.PartD, &it.PartE, &it.PartF, &it.Description,
		&it.StarLevel, &it.StarExperience, &it.Subparts)
	if err != nil {
		return nil, err
	}
	return it, nil
}

// GetInventory returns a characters complete inventory
func GetInventory(charID int64) ([]*domain.InventoryItem, error) {
	rows, err := DB().Query(`SELECT `+invCols+` FROM inventory WHERE char_id = ? ORDER BY slot`, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*domain.InventoryItem
	for rows.Next() {
		it, err := scanInvItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// returns the item at a char_id, slot, is_equipped) address or ErrItemNotFound
// Equipped weapon or outfit slots and backpack slots do share the same `slot` column if is_equipped is true
func GetInventoryItemAt(charID int64, slot int, equipped bool) (*domain.InventoryItem, error) {
	row := DB().QueryRow(`SELECT `+invCols+` FROM inventory WHERE char_id = ? AND slot = ? AND is_equipped = ?`, charID, slot, equipped)
	it, err := scanInvItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrItemNotFound
	}
	return it, err
}

func GetEquippedSingle(charID int64, itemKind int) (*domain.InventoryItem, error) {
	row := DB().QueryRow(`SELECT `+invCols+` FROM inventory WHERE char_id = ? AND item_kind = ? AND is_equipped = 1 LIMIT 1`, charID, itemKind)
	it, err := scanInvItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}


func GetEquippedItemAt(charID int64, slot int, itemKind int) (*domain.InventoryItem, error) {
	row := DB().QueryRow(`SELECT `+invCols+` FROM inventory WHERE char_id = ? AND slot = ? AND item_kind = ? AND is_equipped = 1`, charID, slot, itemKind)
	it, err := scanInvItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrItemNotFound
	}
	return it, err
}

// very self explanatory, will add an item to the players inventory/backpack
func AddInventoryItem(it *domain.InventoryItem) (int64, error) {
	res, err := DB().Exec(`
		INSERT INTO inventory (char_id, slot, resource_id, quantity, flags, rarity,
		                       damage, accuracy, fire_rate, reload_speed, magazine,
		                       element_type, element_damage, element_chance, parts, seed,
		                       is_equipped, is_weapon, item_kind, level, uid, item_name,
		                       generation_source, price, expiration, owner_id,
		                       part_a, part_b, part_c, part_d, part_e, part_f, description,
		                       star_level, star_experience, subparts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.CharID, it.Slot, it.ResourceID, it.Quantity, it.Flags, it.Rarity,
		it.Damage, it.Accuracy, it.FireRate, it.ReloadSpeed, it.Magazine,
		it.ElementType, it.ElementDmg, it.ElementChance, it.Parts, it.Seed,
		it.IsEquipped, it.IsWeapon, it.ItemKind, it.Level, it.UID, it.ItemName,
		it.GenerationSource, it.Price, it.Expiration, it.OwnerID,
		it.PartA, it.PartB, it.PartC, it.PartD, it.PartE, it.PartF, it.Description,
		it.StarLevel, it.StarExperience, it.Subparts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateWeaponStar(itemID int64, starLevel, starExperience int) error {
	_, err := DB().Exec("UPDATE inventory SET star_level = ?, star_experience = ? WHERE item_id = ?",
		starLevel, starExperience, itemID)
	return err
}

// gives a new created character the starter SMG. in future we either keep this or swap to a live generated starter.
// recommended to just keep it to avoid any issues with generation
func SeedStarterWeapon(charID int64) error {
	_, err := AddInventoryItem(&domain.InventoryItem{
		CharID:     charID,
		Slot:       0,
		ResourceID: 1,
		IsEquipped: true,
		IsWeapon:   true,
		ItemKind:   domain.ItemTypeWeapon,
		Level:      1,
		UID:        newItemUID(),
		ItemName:   "SMG",
	})
	return err
}

// deletes an item from the inventory
func RemoveInventoryItem(itemID int64) error {
	_, err := DB().Exec("DELETE FROM inventory WHERE item_id = ?", itemID)
	return err
}

// pdates slot and equipped state
func UpdateInventorySlot(itemID int64, slot int, equipped bool) error {
	_, err := DB().Exec("UPDATE inventory SET slot = ?, is_equipped = ? WHERE item_id = ?", slot, equipped, itemID)
	return err
}

// marks as equipped or unequipped
func EquipInventoryItem(itemID int64, equipped bool) error {
	_, err := DB().Exec("UPDATE inventory SET is_equipped = ? WHERE item_id = ?", equipped, itemID)
	return err
}

func moveSlot(tx *sql.Tx, itemID int64, slot int, equipped bool) error {
	_, err := tx.Exec("UPDATE inventory SET slot = ?, is_equipped = ? WHERE item_id = ?", slot, equipped, itemID)
	return err
}

// this is for swapping items between slots, you can have either slot be empty or do a switch out
func SwapBackpackSlots(charID int64, slotA, slotB byte) error {
	if slotA == slotB {
		return nil
	}
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	a, errA := GetInventoryItemAt(charID, int(slotA), false)
	if errA != nil && !errors.Is(errA, ErrItemNotFound) {
		return errA
	}
	b, errB := GetInventoryItemAt(charID, int(slotB), false)
	if errB != nil && !errors.Is(errB, ErrItemNotFound) {
		return errB
	}
	if a != nil {
		if err := moveSlot(tx, a.ItemID, int(slotB), false); err != nil {
			return err
		}
	}
	if b != nil {
		if err := moveSlot(tx, b.ItemID, int(slotA), false); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DropBackpackItem removes the item at a backpack slot (op 193)
func DropBackpackItem(charID int64, slot byte) error {
	_, err := DB().Exec("DELETE FROM inventory WHERE char_id = ? AND slot = ? AND is_equipped = 0", charID, int(slot))
	return err
}

// EquipWeaponFromBackpack moves a backpack item into a weapon slot
// (Inventory.method_9 / op 192) the item at backpackSlot MUST be a Weapon
func EquipWeaponFromBackpack(charID int64, backpackSlot, weaponSlot byte) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	newWeapon, err := GetInventoryItemAt(charID, int(backpackSlot), false)
	if err != nil {
		return err
	}
	if newWeapon.ItemKind != domain.ItemTypeWeapon {
		return errors.New("item at slot is not a weapon")
	}
	oldWeapon, err := GetEquippedItemAt(charID, int(weaponSlot), domain.ItemTypeWeapon)
	if err != nil && !errors.Is(err, ErrItemNotFound) {
		return err
	}
	if err := moveSlot(tx, newWeapon.ItemID, int(weaponSlot), true); err != nil {
		return err
	}
	if oldWeapon != nil {
		if err := moveSlot(tx, oldWeapon.ItemID, int(backpackSlot), false); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func UnequipWeaponToBackpack(charID int64, weaponSlot, backpackSlot byte) error {
	weapon, err := GetEquippedItemAt(charID, int(weaponSlot), domain.ItemTypeWeapon)
	if err != nil {
		return err
	}
	if _, err := GetInventoryItemAt(charID, int(backpackSlot), false); err == nil {
		return ErrSlotOccupied
	} else if !errors.Is(err, ErrItemNotFound) {
		return err
	}
	return UpdateInventorySlot(weapon.ItemID, int(backpackSlot), false)
}

func SwapWeaponSlots(charID int64, slotA, slotB byte) error {
	if slotA == slotB {
		return nil
	}
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	a, errA := GetEquippedItemAt(charID, int(slotA), domain.ItemTypeWeapon)
	if errA != nil && !errors.Is(errA, ErrItemNotFound) {
		return errA
	}
	b, errB := GetEquippedItemAt(charID, int(slotB), domain.ItemTypeWeapon)
	if errB != nil && !errors.Is(errB, ErrItemNotFound) {
		return errB
	}
	if a != nil {
		if err := moveSlot(tx, a.ItemID, int(slotB), true); err != nil {
			return err
		}
	}
	if b != nil {
		if err := moveSlot(tx, b.ItemID, int(slotA), true); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func SetActiveWeaponSlot(charID int64, weaponSlot byte) error {
	if _, err := GetEquippedItemAt(charID, int(weaponSlot), domain.ItemTypeWeapon); err != nil {
		return err
	}
	_, err := DB().Exec("UPDATE characters SET active_weapon_slot = ? WHERE char_id = ?", weaponSlot, charID)
	return err
}

func equipSingleFromBackpack(charID int64, slot byte, itemKind int) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	newItem, err := GetInventoryItemAt(charID, int(slot), false)
	if err != nil {
		return err
	}
	if newItem.ItemKind != itemKind {
		return errors.New("item at slot does not match expected kind")
	}
	oldItem, err := GetEquippedSingle(charID, itemKind)
	if err != nil {
		return err
	}
	if err := moveSlot(tx, newItem.ItemID, 0, true); err != nil {
		return err
	}
	if oldItem != nil {
		if err := moveSlot(tx, oldItem.ItemID, int(slot), false); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EquipShieldFromBackpack (op 135)
func EquipShieldFromBackpack(charID int64, slot byte) error {
	return equipSingleFromBackpack(charID, slot, domain.ItemTypeShield)
}

// EquipClassModFromBackpack (op 86)
func EquipClassModFromBackpack(charID int64, slot byte) error {
	return equipSingleFromBackpack(charID, slot, domain.ItemTypeClassMode)
}

func EquipOutfitFromBackpack(charID int64, slot byte) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	newItem, err := GetInventoryItemAt(charID, int(slot), false)
	if err != nil {
		return err
	}
	if newItem.ItemKind != domain.ItemTypeOutfit {
		return errors.New("item at slot is not an outfit piece")
	}
	subpart := newItem.PartB
	oldItem, err := GetEquippedItemAt(charID, subpart, domain.ItemTypeOutfit)
	if err != nil && !errors.Is(err, ErrItemNotFound) {
		return err
	}
	if err := moveSlot(tx, newItem.ItemID, subpart, true); err != nil {
		return err
	}
	if oldItem != nil {
		if err := moveSlot(tx, oldItem.ItemID, int(slot), false); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func UnequipOutfitPiece(charID int64, subpart byte, backpackSize int) error {
	item, err := GetEquippedItemAt(charID, int(subpart), domain.ItemTypeOutfit)
	if err != nil {
		return err
	}
	free, err := firstFreeBackpackSlot(charID, backpackSize)
	if err != nil {
		return err
	}
	if free < 0 {
		return errors.New("no room in backpack to unequip outfit")
	}
	return UpdateInventorySlot(item.ItemID, free, false)
}

// AddItemToBackpack inserts a purchased/rolled item into a characters
// first free slot (e.g. a shop purchase)
func AddItemToBackpack(it *domain.InventoryItem, backpackSize int) (int64, int, error) {
	slot, err := firstFreeBackpackSlot(it.CharID, backpackSize)
	if err != nil {
		return 0, -1, err
	}
	if slot < 0 {
		return 0, -1, ErrInventoryFull
	}
	it.Slot = slot
	it.IsEquipped = false
	id, err := AddInventoryItem(it)
	return id, slot, err
}

func firstFreeBackpackSlot(charID int64, backpackSize int) (int, error) {
	rows, err := DB().Query("SELECT slot FROM inventory WHERE char_id = ? AND is_equipped = 0", charID)
	if err != nil {
		return -1, err
	}
	defer rows.Close()
	taken := map[int]bool{}
	for rows.Next() {
		var s int
		if err := rows.Scan(&s); err != nil {
			return -1, err
		}
		taken[s] = true
	}
	if err := rows.Err(); err != nil {
		return -1, err
	}
	for i := 0; i < backpackSize; i++ {
		if !taken[i] {
			return i, nil
		}
	}
	return -1, nil
}

func SplitStack(charID int64, fromSlot, toSlot int, amount int) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	from, err := GetInventoryItemAt(charID, fromSlot, false)
	if err != nil {
		return err
	}
	if from.ItemKind != domain.ItemTypeMaterial || from.Quantity <= amount {
		return errors.New("invalid split")
	}
	if _, err := GetInventoryItemAt(charID, toSlot, false); err == nil {
		return ErrSlotOccupied
	} else if !errors.Is(err, ErrItemNotFound) {
		return err
	}
	if _, err := tx.Exec("UPDATE inventory SET quantity = quantity - ? WHERE item_id = ?", amount, from.ItemID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO inventory (char_id, slot, resource_id, quantity, item_kind, uid)
		VALUES (?, ?, ?, ?, ?, ?)`,
		charID, toSlot, from.ResourceID, amount, domain.ItemTypeMaterial, newItemUID()); err != nil {
		return err
	}
	return tx.Commit()
}


func MergeStack(charID int64, fromSlot, toSlot int) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	from, err := GetInventoryItemAt(charID, fromSlot, false)
	if err != nil {
		return err
	}
	to, err := GetInventoryItemAt(charID, toSlot, false)
	if err != nil {
		return err
	}
	if from.ItemKind != domain.ItemTypeMaterial || to.ItemKind != domain.ItemTypeMaterial || from.ResourceID != to.ResourceID {
		return errors.New("invalid merge")
	}
	total := from.Quantity + to.Quantity
	if total > 99 {
		if _, err := tx.Exec("UPDATE inventory SET quantity = 99 WHERE item_id = ?", to.ItemID); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE inventory SET quantity = ? WHERE item_id = ?", total-99, from.ItemID); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec("UPDATE inventory SET quantity = ? WHERE item_id = ?", total, to.ItemID); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM inventory WHERE item_id = ?", from.ItemID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// inventory sorting functionality
func TidyInventory(charID int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT `+invCols+` FROM inventory WHERE char_id = ? AND is_equipped = 0 ORDER BY item_kind, rarity DESC, level DESC, item_id`, charID)
	if err != nil {
		return err
	}
	items, err := scanAllInvRows(rows)
	if err != nil {
		return err
	}

	materials := map[int]int{}
	var keep []*domain.InventoryItem
	for _, it := range items {
		if it.ItemKind == domain.ItemTypeMaterial {
			materials[it.ResourceID] += it.Quantity
			continue
		}
		keep = append(keep, it)
	}

	if _, err := tx.Exec("DELETE FROM inventory WHERE char_id = ? AND is_equipped = 0 AND item_kind = ?", charID, domain.ItemTypeMaterial); err != nil {
		return err
	}

	slot := 0
	for _, it := range keep {
		if err := moveSlot(tx, it.ItemID, slot, false); err != nil {
			return err
		}
		slot++
	}
	for resID, total := range materials {
		for total > 0 {
			qty := total
			if qty > 99 {
				qty = 99
			}
			if _, err := tx.Exec(`
				INSERT INTO inventory (char_id, slot, resource_id, quantity, item_kind, uid)
				VALUES (?, ?, ?, ?, ?, ?)`,
				charID, slot, resID, qty, domain.ItemTypeMaterial, newItemUID()); err != nil {
				return err
			}
			slot++
			total -= qty
		}
	}
	return tx.Commit()
}

func scanAllInvRows(rows *sql.Rows) ([]*domain.InventoryItem, error) {
	defer rows.Close()
	var items []*domain.InventoryItem
	for rows.Next() {
		it, err := scanInvItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func newItemUID() string {
	const hex = "0123456789abcdef"
	b := make([]byte, 16)
	for i := range b {
		b[i] = hex[rand.Intn(len(hex))]
	}
	return "srv-" + string(b)
}

// GetWarehouse returns an accounts bank
func GetWarehouse(accountID int64) ([]*domain.WarehouseItem, error) {
	rows, err := DB().Query("SELECT item_id, account_id, slot, resource_id, quantity FROM warehouse WHERE account_id = ? ORDER BY slot", accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*domain.WarehouseItem
	for rows.Next() {
		it := &domain.WarehouseItem{}
		if err := rows.Scan(&it.ItemID, &it.AccountID, &it.Slot, &it.ResourceID, &it.Quantity); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// AddWarehouseItem adds an item to the bank
func AddWarehouseItem(it *domain.WarehouseItem) (int64, error) {
	res, err := DB().Exec("INSERT INTO warehouse (account_id, slot, resource_id, quantity) VALUES (?, ?, ?, ?)",
		it.AccountID, it.Slot, it.ResourceID, it.Quantity)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RemoveWarehouseItem deletes an item from the bank
func RemoveWarehouseItem(itemID int64) error {
	_, err := DB().Exec("DELETE FROM warehouse WHERE item_id = ?", itemID)
	return err
}

func SwapInventorySlots(itemA, itemB int64) error {
	tx, err := DB().Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var slotA, slotB int
	if err := tx.QueryRow("SELECT slot FROM inventory WHERE item_id = ?", itemA).Scan(&slotA); err != nil {
		return err
	}
	if err := tx.QueryRow("SELECT slot FROM inventory WHERE item_id = ?", itemB).Scan(&slotB); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE inventory SET slot = ? WHERE item_id = ?", slotB, itemA); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE inventory SET slot = ? WHERE item_id = ?", slotA, itemB); err != nil {
		return err
	}
	return tx.Commit()
}

func GetWarehouseFull(accountID int64) ([]*domain.WarehouseItem, error) {
	rows, err := DB().Query(`
		SELECT item_id, account_id, slot, resource_id, quantity, rarity,
		       damage, accuracy, fire_rate, reload_speed, magazine,
		       element_type, element_damage, element_chance, parts, seed,
		       is_weapon, created_at
		FROM warehouse WHERE account_id = ? ORDER BY slot`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*domain.WarehouseItem
	for rows.Next() {
		it := &domain.WarehouseItem{}
		if err := rows.Scan(
			&it.ItemID, &it.AccountID, &it.Slot, &it.ResourceID, &it.Quantity,
			&it.Rarity, &it.Damage, &it.Accuracy, &it.FireRate, &it.ReloadSpeed,
			&it.Magazine, &it.ElementType, &it.ElementDmg, &it.ElementChance,
			&it.Parts, &it.Seed, &it.IsWeapon, &it.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}
