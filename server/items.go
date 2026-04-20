package main

// ItemDef defines a usable inventory item that plays a .gani animation.
type ItemDef struct {
	ID          string
	Name        string
	Gani        string
	AnimState   string
	Description string
}

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

func inventoryItemFromDef(def *ItemDef, qty int) InventoryItem {
	return InventoryItem{
		ID:       def.ID,
		Name:     def.Name,
		Gani:     def.Gani,
		Quantity: qty,
	}
}

