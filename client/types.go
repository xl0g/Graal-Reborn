package main

// PlayerState represents a player's synchronized state.
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
	Playtime  int     `json:"playtime,omitempty"`
	AnimState string  `json:"anim,omitempty"`
	Mounted   bool    `json:"mounted,omitempty"`
}

// NPCState represents an NPC's synchronized state.
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

// GralatPickup is a collectable gralat coin in the world.
type GralatPickup struct {
	ID    string  `json:"id"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Value int     `json:"value"` // 1, 5, 30, or 100
}

// ServerMessage is a general-purpose struct for unmarshaling server messages.
type ServerMessage struct {
	Type     string         `json:"type"`
	Players  []PlayerState  `json:"players,omitempty"`
	NPCs     []NPCState     `json:"npcs,omitempty"`
	Gralats  []GralatPickup `json:"gralats,omitempty"`
	From     string         `json:"from,omitempty"`
	Msg      string         `json:"msg,omitempty"`
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name,omitempty"`
	X        float64        `json:"x,omitempty"`
	Y        float64        `json:"y,omitempty"`
	GralatN  int            `json:"gralat_n,omitempty"`
	Playtime int            `json:"playtime,omitempty"`
	NPCID    string         `json:"npc_id,omitempty"`
	HP       int            `json:"hp"`
	Killed   bool           `json:"killed,omitempty"`
	Body     string         `json:"body,omitempty"`
	Head     string         `json:"head,omitempty"`
	Hat      string         `json:"hat,omitempty"`
	Shield   string         `json:"shield,omitempty"`
	Sword    string         `json:"sword,omitempty"`
	Damage   int            `json:"damage,omitempty"`
}
