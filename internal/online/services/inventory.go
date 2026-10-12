package services

import (
	"fmt"
	"log"

	"eridian/internal/db"
	"eridian/internal/domain"
	"eridian/internal/online"
)

// RegisterInventoryService registers inventory-related handlers.
func RegisterInventoryService(s *online.OnlineServer) {
	s.Handle(online.ServiceInvGet, handleInvGet)
	s.Handle(online.ServiceInvAdd, handleInvAdd)
	s.Handle(online.ServiceInvRemove, handleInvRemove)
	s.Handle(online.ServiceInvEquip, handleInvEquip)
	s.Handle(online.ServiceInvSwap, handleInvSwap)
	s.Handle(online.ServiceInvGetWarehouse, handleInvGetWarehouse)
}

func handleInvGet(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	charID := resolveCharID(c, data)
	if charID == 0 {
		return nil, fmt.Errorf("char_id_required")
	}
	verifyCharOwnership(c, charID)

	items, err := db.GetInventory(charID)
	if err != nil {
		return nil, fmt.Errorf("get_inventory: %w", err)
	}
	if items == nil {
		items = []*domain.InventoryItem{}
	}
	return map[string]any{"items": items}, nil
}

func handleInvAdd(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	charID := resolveCharID(c, data)
	if charID == 0 {
		return nil, fmt.Errorf("char_id_required")
	}
	verifyCharOwnership(c, charID)

	resourceID, ok := toInt(data["resource_id"])
	if !ok || resourceID == 0 {
		return nil, fmt.Errorf("resource_id_required")
	}
	quantity, _ := toInt(data["quantity"])
	if quantity <= 0 {
		quantity = 1
	}
	slot, _ := toInt(data["slot"])
	if slot < 0 {
		slot = 0
	}
	rarity, _ := toInt(data["rarity"])
	flags, _ := toInt(data["flags"])

	it := &domain.InventoryItem{
		CharID:     charID,
		Slot:       slot,
		ResourceID: resourceID,
		Quantity:   quantity,
		Flags:      flags,
		Rarity:     rarity,
	}

	id, err := db.AddInventoryItem(it)
	if err != nil {
		return nil, fmt.Errorf("add_item: %w", err)
	}
	it.ItemID = id

	log.Printf("[ONLINE] item added: resource=%d qty=%d char=%d", resourceID, quantity, charID)
	return map[string]any{"item": it}, nil
}

func handleInvRemove(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	itemID, ok := toInt64(data["item_id"])
	if !ok {
		return nil, fmt.Errorf("item_id_required")
	}

	if err := db.RemoveInventoryItem(itemID); err != nil {
		return nil, fmt.Errorf("remove_item: %w", err)
	}

	return map[string]any{"item_id": itemID}, nil
}

func handleInvEquip(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	itemID, ok := toInt64(data["item_id"])
	if !ok {
		return nil, fmt.Errorf("item_id_required")
	}
	equipped := true
	if v, ok := data["equipped"].(bool); ok {
		equipped = v
	}

	if err := db.EquipInventoryItem(itemID, equipped); err != nil {
		return nil, fmt.Errorf("equip_item: %w", err)
	}

	return map[string]any{"item_id": itemID, "equipped": equipped}, nil
}

func handleInvSwap(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	itemA, ok := toInt64(data["item_id_a"])
	if !ok {
		return nil, fmt.Errorf("item_id_a_required")
	}
	itemB, ok := toInt64(data["item_id_b"])
	if !ok {
		return nil, fmt.Errorf("item_id_b_required")
	}

	if err := db.SwapInventorySlots(itemA, itemB); err != nil {
		return nil, fmt.Errorf("swap_items: %w", err)
	}

	return map[string]any{"item_id_a": itemA, "item_id_b": itemB}, nil
}

func handleInvGetWarehouse(c *online.ClientSession, data map[string]any) (map[string]any, error) {
	if c.AccountID == 0 {
		return nil, fmt.Errorf("not_authenticated")
	}

	items, err := db.GetWarehouseFull(c.AccountID)
	if err != nil {
		return nil, fmt.Errorf("get_warehouse: %w", err)
	}
	if items == nil {
		items = []*domain.WarehouseItem{}
	}
	return map[string]any{"items": items}, nil
}
