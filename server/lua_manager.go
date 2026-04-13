package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

const resourcesDir = "resources"

// ─── Timer ───────────────────────────────────────────────────────────────────

// luaTimer represents a SetTimeout / SetInterval timer inside a resource.
type luaTimer struct {
	id       int
	interval float64 // seconds between fires
	elapsed  float64
	fn       *lua.LFunction
	repeat   bool
	dead     bool
}

// ─── Event ───────────────────────────────────────────────────────────────────

// luaEvent is a queued event to be dispatched to a resource during Tick.
// goArgs are raw Go values; they are converted to Lua values on dispatch so
// that creation is safe from any goroutine (Lua states are not thread-safe).
type luaEvent struct {
	name   string
	goArgs []interface{}
}

// ─── Resource ────────────────────────────────────────────────────────────────

// Resource is a running Lua scripting environment.
type Resource struct {
	name        string
	path        string
	L           *lua.LState
	timers      []*luaTimer
	timerSeq    int
	handlers    map[string][]*lua.LFunction // event name → registered callbacks
	eventCh     chan luaEvent               // cross-goroutine event queue
	running     bool
	spawnedNPCs []string // IDs of NPCs created by this resource (cleaned up on stop)
}

func newResource(name, path string) *Resource {
	return &Resource{
		name:     name,
		path:     path,
		handlers: make(map[string][]*lua.LFunction),
		eventCh:  make(chan luaEvent, 512),
	}
}

// ─── Manager ─────────────────────────────────────────────────────────────────

// LuaManager handles the complete lifecycle of all Lua resources.
type LuaManager struct {
	mu  sync.Mutex
	res map[string]*Resource
	hub *Hub
}

var globalLuaManager *LuaManager

// newLuaManager creates the manager, creates the resources directory if absent,
// and auto-starts every resource subdirectory found there.
func newLuaManager(hub *Hub) *LuaManager {
	lm := &LuaManager{
		res: make(map[string]*Resource),
		hub: hub,
	}
	globalLuaManager = lm
	if err := os.MkdirAll(resourcesDir, 0755); err != nil {
		log.Printf("[LUA] Cannot create resources dir: %v", err)
		return lm
	}
	lm.autoStart()
	return lm
}

// autoStart loads every subdirectory of resourcesDir as a resource.
func (lm *LuaManager) autoStart() {
	entries, err := os.ReadDir(resourcesDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		if err := lm.start(name); err != nil {
			log.Printf("[LUA] Auto-start '%s' failed: %v", name, err)
		}
	}
}

// ─── Public API (thread-safe) ────────────────────────────────────────────────

// Start loads and runs the named resource.
func (lm *LuaManager) Start(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.start(name)
}

// Stop shuts down the named resource and cleans up its spawned NPCs.
func (lm *LuaManager) Stop(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.stop(name)
}

// Restart stops then re-starts a resource atomically.
func (lm *LuaManager) Restart(name string) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	_ = lm.stop(name) // ignore "not running" error
	return lm.start(name)
}

// List returns the sorted names of currently running resources.
func (lm *LuaManager) List() []string {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	names := make([]string, 0, len(lm.res))
	for n := range lm.res {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// TriggerEvent broadcasts an event to all running resources.
// Safe to call from any goroutine; the event is processed during the next Tick.
func (lm *LuaManager) TriggerEvent(eventName string, args ...interface{}) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	for _, r := range lm.res {
		if !r.running {
			continue
		}
		select {
		case r.eventCh <- luaEvent{name: eventName, goArgs: args}:
		default:
			log.Printf("[LUA] Event queue full for '%s', dropping '%s'", r.name, eventName)
		}
	}
}

// ─── Game-loop integration ────────────────────────────────────────────────────

// Tick must be called once per game-loop frame from the game-loop goroutine.
// It drains queued events and advances timers for all running resources.
func (lm *LuaManager) Tick(dt float64) {
	lm.mu.Lock()
	active := make([]*Resource, 0, len(lm.res))
	for _, r := range lm.res {
		if r.running {
			active = append(active, r)
		}
	}
	lm.mu.Unlock()

	for _, r := range active {
		// Drain the event queue (non-blocking).
	drainLoop:
		for {
			select {
			case ev := <-r.eventCh:
				lm.callHandlers(r, ev.name, ev.goArgs...)
			default:
				break drainLoop
			}
		}

		// Advance and fire timers.
		for _, t := range r.timers {
			if t.dead {
				continue
			}
			t.elapsed += dt
			if t.elapsed >= t.interval {
				t.elapsed -= t.interval
				if err := r.L.CallByParam(lua.P{
					Fn:      t.fn,
					NRet:    0,
					Protect: true,
				}); err != nil {
					log.Printf("[LUA] Timer error in '%s': %v", r.name, err)
				}
				if !t.repeat {
					t.dead = true
				}
			}
		}
		// Purge dead timers.
		live := r.timers[:0]
		for _, t := range r.timers {
			if !t.dead {
				live = append(live, t)
			}
		}
		r.timers = live
	}
}

// ─── Internal (must be called with lm.mu held) ────────────────────────────────

func (lm *LuaManager) start(name string) error {
	if r, ok := lm.res[name]; ok && r.running {
		return fmt.Errorf("resource '%s' is already running", name)
	}
	path := filepath.Join(resourcesDir, name)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("resource '%s' not found in %s/", name, resourcesDir)
	}

	r := newResource(name, path)
	L := lua.NewState()
	r.L = L

	lm.registerBindings(r)

	scripts := lm.collectScripts(r)
	if len(scripts) == 0 {
		L.Close()
		return fmt.Errorf("resource '%s' has no .lua scripts", name)
	}
	for _, s := range scripts {
		if err := L.DoFile(s); err != nil {
			L.Close()
			return fmt.Errorf("resource '%s': %v", name, err)
		}
	}

	r.running = true
	lm.res[name] = r
	log.Printf("[LUA] Started '%s' (%d script(s))", name, len(scripts))

	// Queue the initial event so it fires on the next Tick.
	r.eventCh <- luaEvent{name: "onServerStart"}
	return nil
}

func (lm *LuaManager) stop(name string) error {
	r, ok := lm.res[name]
	if !ok || !r.running {
		return fmt.Errorf("resource '%s' is not running", name)
	}
	// Fire stop event synchronously so scripts can clean up.
	lm.callHandlers(r, "onResourceStop")
	// Clean up every NPC this resource spawned.
	for _, id := range r.spawnedNPCs {
		lm.hub.removeLuaNPC(id)
	}
	r.L.Close()
	r.running = false
	delete(lm.res, name)
	log.Printf("[LUA] Stopped '%s'", name)
	return nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// callHandlers invokes all Lua callbacks registered for an event inside r.
func (lm *LuaManager) callHandlers(r *Resource, event string, goArgs ...interface{}) {
	fns := r.handlers[event]
	if len(fns) == 0 {
		return
	}
	lArgs := make([]lua.LValue, len(goArgs))
	for i, a := range goArgs {
		lArgs[i] = goToLuaVal(r.L, a)
	}
	for _, fn := range fns {
		if err := r.L.CallByParam(lua.P{
			Fn:      fn,
			NRet:    0,
			Protect: true,
		}, lArgs...); err != nil {
			log.Printf("[LUA] Handler '%s' in '%s': %v", event, r.name, err)
		}
	}
}

// collectScripts returns the ordered list of .lua files to execute for a resource.
// Priority: __resource.lua manifest → server.lua → all *.lua (sorted).
func (lm *LuaManager) collectScripts(r *Resource) []string {
	manifest := filepath.Join(r.path, "__resource.lua")
	if _, err := os.Stat(manifest); err == nil {
		if scripts := parseManifest(manifest, r.path); len(scripts) > 0 {
			return scripts
		}
	}
	serverLua := filepath.Join(r.path, "server.lua")
	if _, err := os.Stat(serverLua); err == nil {
		return []string{serverLua}
	}
	// Fall back: every .lua file in the directory, sorted.
	var scripts []string
	if entries, err := os.ReadDir(r.path); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".lua") {
				scripts = append(scripts, filepath.Join(r.path, e.Name()))
			}
		}
	}
	return scripts
}

// parseManifest executes __resource.lua to extract the server_scripts list.
func parseManifest(manifestPath, resourcePath string) []string {
	L := lua.NewState()
	defer L.Close()
	var scripts []string
	L.SetGlobal("server_scripts", L.NewFunction(func(L *lua.LState) int {
		tbl := L.CheckTable(1)
		tbl.ForEach(func(_ lua.LValue, v lua.LValue) {
			if s, ok := v.(lua.LString); ok {
				p := filepath.Join(resourcePath, string(s))
				if _, err := os.Stat(p); err == nil {
					scripts = append(scripts, p)
				}
			}
		})
		return 0
	}))
	_ = L.DoFile(manifestPath)
	return scripts
}

// goToLuaVal converts a Go scalar to the equivalent Lua value.
func goToLuaVal(L *lua.LState, v interface{}) lua.LValue {
	if v == nil {
		return lua.LNil
	}
	switch v := v.(type) {
	case bool:
		return lua.LBool(v)
	case int:
		return lua.LNumber(v)
	case int64:
		return lua.LNumber(v)
	case float64:
		return lua.LNumber(v)
	case string:
		return lua.LString(v)
	default:
		return lua.LString(fmt.Sprintf("%v", v))
	}
}
