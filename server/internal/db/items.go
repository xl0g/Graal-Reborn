package db

// InventoryRow is one row from the inventory table.
type InventoryRow struct {
	ItemID   string
	Quantity int
}

func GetInventory(userID int64) []InventoryRow {
	rows, err := conn.Query(
		`SELECT item_id, quantity FROM inventory WHERE user_id=$1`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []InventoryRow
	for rows.Next() {
		var r InventoryRow
		if rows.Scan(&r.ItemID, &r.Quantity) == nil {
			result = append(result, r)
		}
	}
	return result
}

func GiveItem(userID int64, itemID string, qty int) {
	conn.Exec(`
		INSERT INTO inventory (user_id, item_id, quantity) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, item_id) DO UPDATE SET quantity = inventory.quantity + $3`,
		userID, itemID, qty)
}

func RemoveItem(userID int64, itemID string) bool {
	var qty int
	if conn.QueryRow(
		`SELECT quantity FROM inventory WHERE user_id=$1 AND item_id=$2`,
		userID, itemID,
	).Scan(&qty) != nil || qty <= 0 {
		return false
	}
	if qty == 1 {
		conn.Exec(`DELETE FROM inventory WHERE user_id=$1 AND item_id=$2`, userID, itemID)
	} else {
		conn.Exec(`UPDATE inventory SET quantity=quantity-1 WHERE user_id=$1 AND item_id=$2`, userID, itemID)
	}
	return true
}

// WorldItemRow represents an admin-spawned world item stored in the DB.
type WorldItemRow struct {
	ID, Name, SpritePath, ItemID, MapName string
	X, Y                                  float64
	Price                                 int
}

func LoadWorldItems() []WorldItemRow {
	rows, err := conn.Query(
		`SELECT id, name, sprite_path, x, y, price, item_id, map_name FROM world_items`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []WorldItemRow
	for rows.Next() {
		var w WorldItemRow
		if rows.Scan(&w.ID, &w.Name, &w.SpritePath, &w.X, &w.Y,
			&w.Price, &w.ItemID, &w.MapName) == nil {
			result = append(result, w)
		}
	}
	return result
}

func SaveWorldItem(w WorldItemRow) {
	conn.Exec(`
		INSERT INTO world_items (id, name, sprite_path, x, y, price, item_id, map_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			name=$2, sprite_path=$3, x=$4, y=$5, price=$6, item_id=$7, map_name=$8`,
		w.ID, w.Name, w.SpritePath, w.X, w.Y, w.Price, w.ItemID, w.MapName)
}

func RemoveWorldItem(id string) {
	conn.Exec(`DELETE FROM world_items WHERE id=$1`, id)
}
