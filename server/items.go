package main

// ItemDef defines a usable inventory item that plays a .gani animation.
// To add a new item: append an entry to allItemDefs with a unique ID.
type ItemDef struct {
	ID          string
	Name        string
	Gani        string // .gani filename (relative to GANITEMPLATE/res/ganis/)
	AnimState   string // animation state name broadcast to clients
	Description string
}

// allItemDefs is the master list of usable items.
// Easy to extend: just add a new entry here.
var allItemDefs = map[string]*ItemDef{
	"juggle": {
		ID:          "juggle",
		Name:        "Juggling Balls",
		Gani:        "classic_new_juggle.gani",
		AnimState:   "classic_juggle",
		Description: "Perform a classic juggling act!",
	},
	"hattrick": {
		ID:          "hattrick",
		Name:        "hat trick",
		Gani:        "ci_pompoms.gani",
		AnimState:   "hattrick",
		Description: "Perform a classic juggling act!",
	},
	"pompoms": {
		ID:          "pompoms",
		Name:        "Pompoms",
		Gani:        "ci_pompoms.gani",
		AnimState:   "pompoms",
		Description: "Cheer on the team with pompoms!",
	},
}

// inventoryItemFromDef builds an InventoryItem DTO from an ItemDef + quantity.
func inventoryItemFromDef(def *ItemDef, qty int) InventoryItem {
	return InventoryItem{
		ID:       def.ID,
		Name:     def.Name,
		Gani:     def.Gani,
		Quantity: qty,
	}
}
