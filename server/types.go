package main

// PlayerState is the synchronized state of a connected player.
type PlayerState struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Dir       int     `json:"dir"`
	Moving    bool    `json:"moving"`
	Body      string  `json:"body,omitempty"`
	Head      string  `json:"head,omitempty"`
	Hat       string  `json:"hat,omitempty"`
	Shield    string  `json:"shield,omitempty"`
	Sword     string  `json:"sword,omitempty"`
	Gralats   int     `json:"gralats,omitempty"`
	Playtime  int     `json:"playtime,omitempty"` // total seconds played (including current session)
	AnimState string  `json:"anim,omitempty"`
	Mounted   bool    `json:"mounted,omitempty"`
	HP        int     `json:"hp,omitempty"`
	MaxHP     int     `json:"maxHp,omitempty"`
}

// NPCState is the synchronized state of an NPC.
type NPCState struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Dir       int     `json:"dir"`
	Moving    bool    `json:"moving"`
	NPCType   int     `json:"npcType"`
	HP        int     `json:"hp"`
	MaxHP     int     `json:"maxHp"`
	MountedBy string  `json:"mountedBy,omitempty"`
	AnimState string  `json:"anim,omitempty"`
}

// GralatPickup is a collectable coin in the world.
type GralatPickup struct {
	ID    string  `json:"id"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Value int     `json:"value"` // 1 | 5 | 30 | 100
}
