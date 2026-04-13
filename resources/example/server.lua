-- ============================================================
-- Example resource — demonstrates the full Lua API
-- Start with: /start example  (admin only in-game)
-- ============================================================

print("Resource '" .. GetResourceName() .. "' loading...")

-- ── NPC Spawning ─────────────────────────────────────────────
-- CreateNPC(name, x, y [, npcType=0 [, dialog [, gMin [, gMax]]]])
-- npcType: 0=villager 1=merchant 2=guard 3=traveler 4=farmer

local guardId   = CreateNPC("Capitaine Léon", 200, 300, 2,
    "Halte ! Ce village est sous ma protection. Prends ces gralats, voyageur.", 2, 5)

local merchantId = CreateNPC("Marceline la Marchande", 400, 250, 1,
    "Bienvenue dans ma boutique ! Voici un petit cadeau de bienvenue.", 3, 8)

local villagerIds = {}
local villagerPositions = {
    {350, 400}, {500, 180}, {600, 350},
}
for i, pos in ipairs(villagerPositions) do
    local id = CreateNPC("Villageois " .. i, pos[1], pos[2], 0)
    villagerIds[i] = id
end

-- ── Timed events ─────────────────────────────────────────────
-- Announce every 5 minutes
SetInterval(5 * 60 * 1000, function()
    BroadcastMessage("Le marché du village est ouvert ! Parlez aux PNJ pour gagner des gralats.")
end)

-- Patrol: move the guard between two positions every 30 seconds
local patrolA = {200, 300}
local patrolB = {280, 300}
local patrolToggle = false

SetInterval(30 * 1000, function()
    patrolToggle = not patrolToggle
    if patrolToggle then
        SetNPCPosition(guardId, patrolB[1], patrolB[2])
    else
        SetNPCPosition(guardId, patrolA[1], patrolA[2])
    end
end)

-- ── Player Events ─────────────────────────────────────────────
AddEventHandler("onPlayerConnect", function(playerId, playerName)
    print("Player connected: " .. playerName .. " (" .. playerId .. ")")

    -- Welcome message after a short delay
    SetTimeout(2000, function()
        SendMessage(playerId,
            "Bienvenue " .. playerName .. " ! Parle aux PNJ pour gagner des gralats.")
    end)
end)

AddEventHandler("onPlayerDisconnect", function(playerId, playerName)
    print("Player disconnected: " .. playerName)
    BroadcastMessage(playerName .. " a quitté le monde.")
end)

AddEventHandler("onPlayerChat", function(playerId, playerName, msg)
    -- Simple !players command available to everyone
    if msg == "!players" then
        local players = GetPlayers()
        local names = {}
        for _, p in ipairs(players) do
            names[#names + 1] = p.name
        end
        SendMessage(playerId, "Joueurs en ligne (" .. #players .. "): " .. table.concat(names, ", "))
    end

    -- !pos: show own position
    if msg == "!pos" then
        local x, y = GetPlayerPos(playerId)
        if x then
            SendMessage(playerId, string.format("Position: %.0f, %.0f", x, y))
        end
    end
end)

-- ── Server lifecycle ──────────────────────────────────────────
AddEventHandler("onServerStart", function()
    print("Server started — all NPCs ready.")
    BroadcastMessage("Le serveur est prêt. Bienvenue !")
end)

AddEventHandler("onResourceStop", function()
    print("Resource stopping — NPCs will be removed automatically.")
end)

print("Resource '" .. GetResourceName() .. "' loaded successfully.")
