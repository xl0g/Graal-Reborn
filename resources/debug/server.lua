-- debug resource — commandes utiles en chat
-- !pos       → affiche ta position
-- !hello     → message de test dans le chat
-- !players   → liste les joueurs connectés

AddEventHandler("onPlayerChat", function(playerId, playerName, msg)

    if msg == "!pos" then
        local x, y = GetPlayerPos(playerId)
        if x then
            SendMessage(playerId, string.format("[debug] %s → x=%.1f  y=%.1f", playerName, x, y))
        end

    elseif msg == "!hello" then
        BroadcastChat("Serveur", "Hey from Lua ! " .. playerName .. " its me.")

    elseif msg == "!players" then
        local players = GetPlayers()
        local names = {}
        for _, p in ipairs(players) do
            names[#names + 1] = p.name .. string.format("(%.0f,%.0f)", p.x, p.y)
        end
        SendMessage(playerId, string.format("[debug] %d joueur(s): %s", #players, table.concat(names, ", ")))

    end
end)

print("debug resource prête — commandes: !pos !hello !players")
