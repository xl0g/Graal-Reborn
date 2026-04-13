-- ============================================================
-- world_events — random world events broadcast periodically
-- Demonstrates: timers, BroadcastMessage, GetPlayers, GiveGralats
-- ============================================================

print("world_events loading...")

local events = {
    {
        msg  = "⚡ Une tempête magique éclate ! Tous les joueurs reçoivent 5 gralats.",
        gralats = 5,
    },
    {
        msg  = "🌟 Une étoile filante traverse le ciel — 3 gralats tombent du ciel !",
        gralats = 3,
    },
    {
        msg  = "🎉 Festival du village ! Chaque joueur connecté reçoit 10 gralats.",
        gralats = 10,
    },
}

-- Fire a random world event every 10 minutes.
SetInterval(10 * 60 * 1000, function()
    local ev = events[RandomInt(1, #events)]
    BroadcastMessage(ev.msg)

    -- Give gralats to every connected player.
    local players = GetPlayers()
    for _, p in ipairs(players) do
        GiveGralats(p.id, ev.gralats)
    end

    print(string.format("World event fired: +%d gralats to %d player(s)", ev.gralats, #players))
end)

-- Hourly reminder
SetInterval(60 * 60 * 1000, function()
    BroadcastMessage("Le serveur tourne depuis une heure de plus. Merci d'être là !")
end)

print("world_events loaded.")
