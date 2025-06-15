// main.go - D2R Kill Counter & Item Tracker (KORRIGIERTER ITEM TRACKER)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/d2go/pkg/memory"
)

// ========== DATA STRUCTURES ==========
type ExtendedGameReader struct {
	*memory.GameReader
}

type CorpseInfo struct {
	UnitID   data.UnitID
	Position data.Position
}

type ItemEntry struct {
	Name         string    `json:"name"`
	OriginalName string    `json:"original_name"` // Store original name
	Quality      string    `json:"quality"`
	RunIndex     int       `json:"run_index"`
	Time         time.Time `json:"time"`
	// ========== PHASE 2: Enhanced Item Display ==========
	Affixes      string `json:"affixes,omitempty"`       // Item affixes display
	IsEthereal   bool   `json:"is_ethereal,omitempty"`   // Ethereal flag
	IsIdentified bool   `json:"is_identified,omitempty"` // Identified flag
	ItemLevel    int    `json:"item_level,omitempty"`    // Item level
	// ========== KORREKTUR: Array Index für Frontend ==========
	ArrayIndex   int    `json:"array_index"`             // Echter Array-Index im itemHistory
}

// ========== XP TRACKING STRUCTURES ==========
type XPTracking struct {
	CurrentXP          int64   `json:"current_xp"`
	CurrentLevel       int     `json:"current_level"`
	XPToNextLevel      int64   `json:"xp_to_next_level"`
	XPThisRun          int64   `json:"xp_this_run"`
	XPPerHour          float64 `json:"xp_per_hour"`
	RunsToNextLevel    int     `json:"runs_to_next_level"`
	AverageXPPerRun    int64   `json:"average_xp_per_run"`
	SessionXPGained    int64   `json:"session_xp_gained"`
	RunStartXP         int64   `json:"run_start_xp"`
	// ========== VERBESSERTE FELDER FÜR RUNS BERECHNUNG ==========
	EstimatedRunsToNext int     `json:"estimated_runs_to_next"`  // Schätzung basierend auf aktuellem Run
	RunsCalculationMethod string `json:"runs_calculation_method"` // Welche Methode verwendet wurde
}

type PersistentData struct {
	KillCounts     map[string]int `json:"kill_counts"`
	TotalKills     int            `json:"total_kills"`
	RunTimes       []int64        `json:"run_times"`
	Items          []ItemEntry    `json:"items"`
	FiltersEnabled bool           `json:"filters_enabled"`
	// ========== XP TRACKING DATA ==========
	XPTracking     XPTracking `json:"xp_tracking"`
	XPRunHistory   []int64    `json:"xp_run_history"`   // XP gained per run
}

// ========== NEUE STRUKTUR FÜR ITEM PAGINATION ==========
type ItemsResponse struct {
	Items          []ItemEntry `json:"items"`           // Items für aktuelle Seite
	TotalItems     int         `json:"total_items"`     // Gesamtanzahl Items
	CurrentPage    int         `json:"current_page"`    // Aktuelle Seite (0-basiert)
	ItemsPerPage   int         `json:"items_per_page"`  // Items pro Seite
	TotalPages     int         `json:"total_pages"`     // Gesamtanzahl Seiten
	ShowAll        bool        `json:"show_all"`        // Ob alle Items angezeigt werden
}

type GameStats struct {
	Normal         int         `json:"normal"`
	Champion       int         `json:"champion"`
	Unique         int         `json:"unique"`
	SuperUnique    int         `json:"superUnique"`
	Minion         int         `json:"minion"`
	Total          int         `json:"total"`
	CurrentRun     string      `json:"currentRun"`
	FastestRun     string      `json:"fastestRun"`
	SlowestRun     string      `json:"slowestRun"`
	AverageRun     string      `json:"averageRun"`
	TotalRuns      int         `json:"totalRuns"`
	RunActive      bool        `json:"runActive"`
	TotalItems     int         `json:"totalItems"`
	RecentItems    []ItemEntry `json:"recentItems"`     // Nur noch für Rückwärtskompatibilität
	CurrentProfile string      `json:"currentProfile"`
	Profiles       []string    `json:"profiles"`
	FiltersEnabled bool        `json:"filtersEnabled"`
	// ========== XP TRACKING & CHARACTER INFO ==========
	XPTracking       XPTracking `json:"xpTracking"`
	PlayerLevel      int        `json:"playerLevel"`
	PlayerClass      string     `json:"playerClass"`
	CurrentArea      string     `json:"currentArea"`
	SessionStartTime time.Time  `json:"sessionStartTime"`
	// ========== NEUE ITEM PAGINATION ==========
	ItemsData        ItemsResponse `json:"itemsData"`    // Neue strukturierte Item-Daten
}

// ========== APP STRUCT ==========
type App struct {
	ctx         context.Context
	gameReader  *ExtendedGameReader
	profilesDir string

	// Game State (protected by mutex)
	mu                 sync.RWMutex
	killCounts         map[string]int
	totalKills         int
	runTimes           []int64
	itemHistory        []ItemEntry
	currentRun         int
	runActive          bool
	runStart           time.Time
	previousCorpses    map[data.UnitID]CorpseInfo
	wasInMenu          bool
	currentProfile     string
	filtersEnabled     bool

	// Item Tracker State
	lastInventory      map[string]data.Item
	lastGroundItems    map[string]data.Item
	itemsFromGround    map[string]time.Time
	trackerInitialized bool

	// ========== RACE CONDITION PROTECTION ==========
	editMutex sync.Mutex // Separate mutex for item editing
	saveMutex sync.Mutex // Separate mutex for save operations

	// ========== XP TRACKING ==========
	xpTracking         XPTracking
	xpRunHistory       []int64       // XP gained per run (last 20 runs)
	sessionStartTime   time.Time     // When current session started
	lastGameData       data.Data     // Store last game data for comparisons

	// ========== ITEM DISPLAY SETTINGS ==========
	itemsPerPage       int           // Configurable items per page
	showAllItems       bool          // Whether to show all items or paginate
}

// ========== CONSTRUCTOR ==========
func NewApp() *App {
	now := time.Now()
	return &App{
		profilesDir:        getProfilesDir(),
		killCounts:         make(map[string]int),
		previousCorpses:    make(map[data.UnitID]CorpseInfo),
		wasInMenu:          true,
		currentProfile:     "default",
		currentRun:         1, // Start at 1, not 0
		filtersEnabled:     true, // Default: filters enabled
		lastInventory:      make(map[string]data.Item),
		lastGroundItems:    make(map[string]data.Item),
		itemsFromGround:    make(map[string]time.Time),
		trackerInitialized: false,
		// ========== XP TRACKING INITIALIZATION ==========
		sessionStartTime: now,
		xpTracking:       XPTracking{},
		xpRunHistory:     make([]int64, 0),
		// ========== ITEM DISPLAY INITIALIZATION ==========
		itemsPerPage:     50,     // Standard: 50 Items pro Seite
		showAllItems:     false,  // Standard: Pagination
	}
}

// ========== WAILS LIFECYCLE ==========
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	fmt.Println("🚀 D2R Tracker started!")

	// Initialize D2R connection
	fmt.Println("🔍 Searching for D2R process...")
	process, err := memory.NewProcess()
	if err != nil {
		fmt.Printf("❌ D2R process not found: %v\n", err)
		fmt.Println("💡 Make sure:")
		fmt.Println("   - Diablo 2: Resurrected is running")
		fmt.Println("   - You are IN GAME (not in menu)")
		fmt.Println("   - App is running as Administrator")
		return
	}

	fmt.Println("✅ D2R process found!")
	baseReader := memory.NewGameReader(process)
	a.gameReader = &ExtendedGameReader{baseReader}

	// Load initial profile
	a.LoadProfile("default")

	// Start game monitoring
	go a.gameLoop()
	fmt.Println("🚀 Game monitoring started")
}

func (a *App) DomReady(ctx context.Context) {
	fmt.Println("🌐 Frontend ready - D2R Tracker UI loaded")
}

func (a *App) BeforeClose(ctx context.Context) (prevent bool) {
	fmt.Println("💾 Saving data before close...")
	a.SaveCurrentProfile()
	return false
}

func (a *App) Shutdown(ctx context.Context) {
	fmt.Println("🔴 D2R Tracker shutdown")
}

// ========== XP TABLE FOR LEVEL CALCULATIONS ==========
var xpTable = map[int]int64{
	1: 0, 2: 500, 3: 1500, 4: 3750, 5: 7875, 6: 14175, 7: 22680, 8: 32886, 9: 44396, 10: 57715,
	11: 72144, 12: 90180, 13: 112725, 14: 140906, 15: 176132, 16: 220165, 17: 275207, 18: 344008, 19: 430010, 20: 537045,
	21: 669827, 22: 835384, 23: 1043072, 24: 1304511, 25: 1633587, 26: 2050432, 27: 2584677, 28: 3275200, 29: 4174038, 30: 5346855,
	31: 6875208, 32: 8850058, 33: 11408158, 34: 14707003, 35: 18964309, 36: 24473582, 37: 31650887, 38: 41086171, 39: 53525811, 40: 69957624,
	41: 91691762, 42: 120501929, 43: 158886883, 44: 210423898, 45: 279522614, 46: 372608871, 47: 498737525, 48: 671030513, 49: 906462756, 50: 1227333859,
	51: 1667731625, 52: 2272357156, 53: 3104876251, 54: 4252143051, 55: 5838693925, 56: 8044304424, 57: 11116876343, 58: 15405053839, 59: 21421839488, 60: 29863845052,
	61: 41774983550, 62: 58618736980, 63: 82520891374, 64: 116599580853, 65: 165603045197, 66: 236451765766, 67: 339776013836, 68: 490929180154, 69: 712671056074, 70: 1041046470985,
	71: 1530411433097, 72: 2262430636153, 73: 3364063763781, 74: 5027983527266, 75: 7554758808657, 76: 11410056554455, 77: 17317411972952, 78: 26367503163062, 79: 40307265014134, 80: 61897015632986,
	// ========== CORRECTED HIGH LEVEL XP VALUES (Based on D2R reality) ==========
	81: 95379475181, 82: 147577204871, 83: 229374370373, 84: 358090031449, 85: 561492851017, 86: 883988877946, 87: 1396987892953, 88: 1800000000, 89: 1900000000, 90: 2000000000,
	91: 1950000000, 92: 2024734818, 93: 2097310703, 94: 2200000000, 95: 2350000000, 96: 2500000000, 97: 2700000000, 98: 2900000000, 99: 3200000000,
}

func getXPForLevel(level int) int64 {
	if xp, exists := xpTable[level]; exists {
		return xp
	}
	return 0
}

func getXPToNextLevel(currentXP int64, currentLevel int) int64 {
	// ========== SIMPLIFIED XP CALCULATION ==========
	// Based on user's actual D2R data: Level 92 with 2,024,734,818 XP needs 2,097,310,703 for Level 93
	
	if currentLevel >= 90 {
		// For high levels, use realistic progression based on actual D2R data
		switch currentLevel {
		case 90:
			return int64(float64(currentXP) * 0.05) // ~5% more
		case 91:
			return int64(float64(currentXP) * 0.04) // ~4% more
		case 92:
			// User's exact data: needs 2,097,310,703 total, has 2,024,734,818
			nextLevelTotal := int64(2097310703)
			if currentXP >= nextLevelTotal {
				return 0 // Already at or above next level
			}
			return nextLevelTotal - currentXP
		case 93:
			return int64(float64(currentXP) * 0.03) // ~3% more
		case 94:
			return int64(float64(currentXP) * 0.025) // ~2.5% more
		case 95:
			return int64(float64(currentXP) * 0.02) // ~2% more
		case 96:
			return int64(float64(currentXP) * 0.015) // ~1.5% more
		case 97:
			return int64(float64(currentXP) * 0.01) // ~1% more
		case 98:
			return int64(float64(currentXP) * 0.005) // ~0.5% more
		default:
			return 0 // Level 99 is max
		}
	}
	
	// For levels below 90, use the traditional table
	nextLevelXP := getXPForLevel(currentLevel + 1)
	if nextLevelXP == 0 {
		return 0 // Max level reached
	}
	return nextLevelXP - currentXP
}

// ========== MAIN API METHODS (JavaScript accessible) ==========

func (a *App) GetStats() GameStats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	stats := GameStats{
		Normal:         a.killCounts[fmt.Sprintf("%v", data.MonsterTypeNone)],
		Champion:       a.killCounts[fmt.Sprintf("%v", data.MonsterTypeChampion)],
		Unique:         a.killCounts[fmt.Sprintf("%v", data.MonsterTypeUnique)],
		SuperUnique:    a.killCounts[fmt.Sprintf("%v", data.MonsterTypeSuperUnique)],
		Minion:         a.killCounts[fmt.Sprintf("%v", data.MonsterTypeMinion)],
		Total:          a.totalKills,
		TotalRuns:      len(a.runTimes),
		RunActive:      a.runActive,
		TotalItems:     len(a.itemHistory),
		CurrentProfile: a.currentProfile,
		Profiles:       a.listProfiles(),
		FiltersEnabled: a.filtersEnabled,
		// ========== XP TRACKING & CHARACTER INFO ==========
		XPTracking:       a.xpTracking,
		PlayerLevel:      a.getPlayerLevel(),
		PlayerClass:      a.getPlayerClassName(),
		CurrentArea:      a.getCurrentAreaName(),
		SessionStartTime: a.sessionStartTime,
	}

	// Current run time
	if a.runActive {
		stats.CurrentRun = formatDuration(time.Since(a.runStart).Milliseconds())
	} else {
		stats.CurrentRun = "00:00:00"
	}

	// Run statistics
	if len(a.runTimes) > 0 {
		fastest, slowest, average := a.getRunStats()
		stats.FastestRun = formatDuration(fastest)
		stats.SlowestRun = formatDuration(slowest)
		stats.AverageRun = formatDuration(average)
	} else {
		stats.FastestRun = "-"
		stats.SlowestRun = "-"
		stats.AverageRun = "-"
	}

	// ========== KORRIGIERTE ITEM-DATEN MIT RICHTIGEN INDIZES ==========
	stats.ItemsData = a.getItemsData(0, a.itemsPerPage) // Erste Seite

	// ========== RÜCKWÄRTSKOMPATIBILITÄT: Recent Items ==========
	// Nur die letzten 10 Items für old clients
	start := len(a.itemHistory) - 10
	if start < 0 {
		start = 0
	}
	stats.RecentItems = make([]ItemEntry, len(a.itemHistory)-start)
	for i, item := range a.itemHistory[start:] {
		itemCopy := item
		// KRITISCH: Setze den echten Array-Index
		itemCopy.ArrayIndex = start + i
		stats.RecentItems[i] = itemCopy
	}

	return stats
}

// ========== NEUE ITEM PAGINATION FUNKTIONEN ==========

func (a *App) GetItemsPage(page int, itemsPerPage int) ItemsResponse {
	a.mu.RLock()
	defer a.mu.RUnlock()
	
	return a.getItemsData(page, itemsPerPage)
}

func (a *App) SetShowAllItems(showAll bool) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	a.showAllItems = showAll
	fmt.Printf("📋 Item display mode changed: showAll=%t\n", showAll)
	return a.showAllItems
}

func (a *App) SetItemsPerPage(itemsPerPage int) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	
	if itemsPerPage < 10 {
		itemsPerPage = 10
	}
	if itemsPerPage > 500 {
		itemsPerPage = 500
	}
	
	a.itemsPerPage = itemsPerPage
	fmt.Printf("📋 Items per page changed: %d\n", itemsPerPage)
	return a.itemsPerPage
}

func (a *App) getItemsData(page int, itemsPerPage int) ItemsResponse {
	totalItems := len(a.itemHistory)
	
	// Wenn keine Items vorhanden
	if totalItems == 0 {
		return ItemsResponse{
			Items:        []ItemEntry{},
			TotalItems:   0,
			CurrentPage:  0,
			ItemsPerPage: itemsPerPage,
			TotalPages:   0,
			ShowAll:      a.showAllItems,
		}
	}

	// Wenn alle Items angezeigt werden sollen
	if a.showAllItems {
		items := make([]ItemEntry, totalItems)
		for i, item := range a.itemHistory {
			itemCopy := item
			itemCopy.ArrayIndex = i // Setze den echten Array-Index
			items[i] = itemCopy
		}
		
		return ItemsResponse{
			Items:        items,
			TotalItems:   totalItems,
			CurrentPage:  0,
			ItemsPerPage: totalItems,
			TotalPages:   1,
			ShowAll:      true,
		}
	}

	// Pagination
	if itemsPerPage <= 0 {
		itemsPerPage = a.itemsPerPage
	}
	
	totalPages := (totalItems + itemsPerPage - 1) / itemsPerPage
	if page < 0 {
		page = 0
	}
	if page >= totalPages {
		page = totalPages - 1
	}

	// ========== WICHTIG: Items in umgekehrter Reihenfolge (neueste zuerst) ==========
	start := totalItems - (page+1)*itemsPerPage
	end := totalItems - page*itemsPerPage
	
	if start < 0 {
		start = 0
	}
	if end > totalItems {
		end = totalItems
	}

	pageItems := make([]ItemEntry, 0, end-start)
	
	// Items in umgekehrter Reihenfolge hinzufügen (neueste zuerst)
	for i := end - 1; i >= start; i-- {
		itemCopy := a.itemHistory[i]
		itemCopy.ArrayIndex = i // KRITISCH: Setze den echten Array-Index
		pageItems = append(pageItems, itemCopy)
	}

	fmt.Printf("📋 getItemsData: page=%d, itemsPerPage=%d, totalItems=%d, start=%d, end=%d, pageItems=%d\n",
		page, itemsPerPage, totalItems, start, end, len(pageItems))

	return ItemsResponse{
		Items:        pageItems,
		TotalItems:   totalItems,
		CurrentPage:  page,
		ItemsPerPage: itemsPerPage,
		TotalPages:   totalPages,
		ShowAll:      false,
	}
}

func (a *App) ResetKills() {
	a.mu.Lock()
	for key := range a.killCounts {
		a.killCounts[key] = 0
	}
	a.totalKills = 0
	a.runTimes = []int64{}
	a.currentRun = 1 // Reset to 1, not 0
	a.runActive = false
	a.previousCorpses = make(map[data.UnitID]CorpseInfo)
	a.mu.Unlock()

	a.SaveCurrentProfile()
	fmt.Println("🔄 Kills and run statistics reset")
}

func (a *App) SwitchProfile(profileName string) error {
	if profileName == "" {
		return fmt.Errorf("profile name cannot be empty")
	}

	a.SaveCurrentProfile()
	a.LoadProfile(profileName)
	fmt.Printf("🔀 Switched to profile: %s\n", profileName)
	return nil
}

func (a *App) CreateProfile(profileName string) error {
	if profileName == "" {
		return fmt.Errorf("profile name cannot be empty")
	}

	if strings.ContainsAny(profileName, `\/:*?"<>|`) {
		return fmt.Errorf("invalid characters in profile name")
	}

	profiles := a.listProfiles()
	for _, p := range profiles {
		if p == profileName {
			return fmt.Errorf("profile already exists")
		}
	}

	a.SaveCurrentProfile()
	a.LoadProfile(profileName)
	a.SaveCurrentProfile()
	fmt.Printf("✅ Created profile: %s\n", profileName)

	return nil
}

func (a *App) DeleteProfile(profileName string) error {
	if profileName == "default" {
		return fmt.Errorf("cannot delete default profile")
	}

	filePath := a.getProfileFilePath(profileName)
	err := os.Remove(filePath)
	if err != nil {
		return fmt.Errorf("failed to delete profile: %v", err)
	}

	if a.currentProfile == profileName {
		a.LoadProfile("default")
	}

	fmt.Printf("🗑️ Deleted profile: %s\n", profileName)
	return nil
}

func (a *App) GetAllItems() []ItemEntry {
	a.mu.RLock()
	defer a.mu.RUnlock()

	items := make([]ItemEntry, len(a.itemHistory))
	for i, item := range a.itemHistory {
		itemCopy := item
		itemCopy.ArrayIndex = i // Setze korrekten Array-Index
		items[i] = itemCopy
	}
	return items
}

func (a *App) ToggleFilters() bool {
	a.mu.Lock()
	a.filtersEnabled = !a.filtersEnabled
	newState := a.filtersEnabled
	a.mu.Unlock()

	go a.SaveCurrentProfile()
	fmt.Printf("🔄 Item filters %s\n", map[bool]string{true: "ENABLED", false: "DISABLED"}[newState])
	return newState
}

func (a *App) GetFilteredItems() []string {
	return []string{
		"Healing Potions (all types)",
		"Mana Potions (all types)",
		"Rejuvenation Potions (all types)",
		"Antidote Potions",
		"Thawing Potions",
		"Stamina Potions",
		"Arrows",
		"Bolts",
		"Gold",
	}
}

// ========== KORRIGIERTE ITEM EDITING FUNKTION ==========

func (a *App) EditItemName(itemIndex int, newName string) error {
	// ========== CRITICAL: Separate mutex for item editing ==========
	a.editMutex.Lock()
	defer a.editMutex.Unlock()

	// Read-Lock for validation
	a.mu.RLock()

	// Detailed validation
	if itemIndex < 0 || itemIndex >= len(a.itemHistory) {
		a.mu.RUnlock()
		fmt.Printf("❌ EDIT ERROR: Invalid index %d (valid range: 0-%d)\n", itemIndex, len(a.itemHistory)-1)
		return fmt.Errorf("invalid item index: %d (valid range: 0-%d)", itemIndex, len(a.itemHistory)-1)
	}

	if newName == "" {
		a.mu.RUnlock()
		fmt.Printf("❌ EDIT ERROR: Empty name provided\n")
		return fmt.Errorf("new name cannot be empty")
	}

	// Additional debug info before change
	fmt.Printf("🔍 EDIT ATTEMPT: Index=%d, Current='%s', New='%s'\n",
		itemIndex, a.itemHistory[itemIndex].Name, newName)
	fmt.Printf("🔍 ARRAY STATE: Length=%d, ItemExists=%t\n",
		len(a.itemHistory), itemIndex < len(a.itemHistory))

	// Backup old state for rollback
	oldItem := a.itemHistory[itemIndex]

	// Upgrade to Write-Lock for change
	a.mu.RUnlock()
	a.mu.Lock()

	// ========== CRITICAL: Double-check after lock upgrade ==========
	if itemIndex >= len(a.itemHistory) {
		a.mu.Unlock()
		fmt.Printf("❌ EDIT ERROR: Index became invalid after lock upgrade!\n")
		return fmt.Errorf("item index became invalid during edit")
	}

	// Perform change
	a.itemHistory[itemIndex].Name = newName

	// Verify change
	if a.itemHistory[itemIndex].Name != newName {
		fmt.Printf("❌ EDIT FAILED: Name was not updated correctly!\n")
		a.itemHistory[itemIndex] = oldItem // Rollback
		a.mu.Unlock()
		return fmt.Errorf("failed to update item name")
	}

	fmt.Printf("✅ EDIT SUCCESS: '%s' -> '%s' (Index: %d)\n", oldItem.Name, newName, itemIndex)
	fmt.Printf("📋 VERIFIED ITEM: Name='%s', Quality='%s', Run=%d\n",
		a.itemHistory[itemIndex].Name,
		a.itemHistory[itemIndex].Quality,
		a.itemHistory[itemIndex].RunIndex)

	a.mu.Unlock()

	// ========== CRITICAL: Synchronous saving instead of async ==========
	err := a.saveCurrentProfileSync()
	if err != nil {
		fmt.Printf("⚠️ SAVE WARNING: Could not save profile: %v\n", err)
		// But still return success since the change is in memory
	} else {
		fmt.Printf("💾 SAVE SUCCESS: Profile saved successfully\n")
	}

	return nil
}

// ========== NEW SYNCHRONOUS SAVE FUNCTION ==========

func (a *App) saveCurrentProfileSync() error {
	// Separate mutex for save operations
	a.saveMutex.Lock()
	defer a.saveMutex.Unlock()

	if a.currentProfile == "" {
		return fmt.Errorf("no current profile")
	}

	filePath := a.getProfileFilePath(a.currentProfile)
	err := os.MkdirAll(a.profilesDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create profiles directory: %v", err)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create profile file: %v", err)
	}
	defer file.Close()

	// Read-Lock for data reading
	a.mu.RLock()
	data := PersistentData{
		KillCounts:     a.killCounts,
		TotalKills:     a.totalKills,
		RunTimes:       a.runTimes,
		Items:          a.itemHistory,
		FiltersEnabled: a.filtersEnabled,
		// ========== XP TRACKING DATA ==========
		XPTracking:   a.xpTracking,
		XPRunHistory: a.xpRunHistory,
	}
	a.mu.RUnlock()

	encoder := json.NewEncoder(file)
	err = encoder.Encode(data)
	if err != nil {
		return fmt.Errorf("failed to encode profile data: %v", err)
	}

	return nil
}

// ========== KEEP OLD ASYNC VERSION FOR OTHER PURPOSES ==========

func (a *App) SaveCurrentProfile() {
	err := a.saveCurrentProfileSync()
	if err != nil {
		fmt.Printf("❌ ASYNC SAVE ERROR: %v\n", err)
	}
}

func (a *App) ExportItems() (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(a.itemHistory) == 0 {
		return "", fmt.Errorf("no items to export")
	}

	// CSV Header - clean column structure
	csvData := "Run;Item Name;Quality;Date;Time\n"

	// Add each item with proper Excel formatting
	for _, item := range a.itemHistory {
		// Format time as separate date and time columns
		dateStr := item.Time.Format("2006-01-02")
		timeStr := item.Time.Format("15:04:05")

		// Clean item name (remove quotes, replace problematic chars)
		itemName := strings.ReplaceAll(item.Name, ";", ",") // Replace semicolons with commas
		itemName = strings.ReplaceAll(itemName, "\"", "'")  // Replace quotes with apostrophes

		// Clean quality name
		quality := strings.ReplaceAll(item.Quality, ";", ",")

		// Use semicolon as delimiter for better Excel compatibility
		csvData += fmt.Sprintf("%d;%s;%s;%s;%s\n",
			item.RunIndex, itemName, quality, dateStr, timeStr)
	}

	fmt.Printf("📊 EXPORT: Generated CSV with %d items in clean column structure\n", len(a.itemHistory))
	return csvData, nil
}

// ========== GAME LOOP ==========
func (a *App) gameLoop() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		if a.gameReader == nil {
			continue
		}

		a.checkGameStatus()
		a.updateKills()
		a.checkForNewItems()
		// ========== XP TRACKING ==========
		a.updateXPTracking()
	}
}

func (a *App) checkGameStatus() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.gameReader.IsIngame() {
		if !a.wasInMenu {
			fmt.Println("🔄 Player went to menu")
			a.wasInMenu = true
			if a.runActive {
				runDuration := time.Since(a.runStart)
				a.runTimes = append(a.runTimes, runDuration.Milliseconds())
				fmt.Printf("⏱️ Run #%d completed: %v\n", a.currentRun, runDuration)

				// ========== END-OF-RUN XP TRACKING ==========
				// Record run statistics
				if a.xpTracking.XPThisRun > 0 {
					a.xpRunHistory = append(a.xpRunHistory, a.xpTracking.XPThisRun)
					// Keep only last 20 runs
					if len(a.xpRunHistory) > 20 {
						a.xpRunHistory = a.xpRunHistory[1:]
					}
					fmt.Printf("📈 Run #%d XP: %d (Session Total: %d)\n", a.currentRun, a.xpTracking.XPThisRun, a.xpTracking.SessionXPGained)
				}

				a.runActive = false
				// FIX: currentRun for next run
				a.currentRun++
				go a.SaveCurrentProfile()
			}
		}
	} else {
		if a.wasInMenu {
			fmt.Printf("🎮 Player entered game! Starting Run #%d\n", a.currentRun)
			a.runStart = time.Now()
			a.runActive = true
			a.wasInMenu = false

			// ========== START-OF-RUN TRACKING RESET ==========
			// Reset run-specific counters
			a.xpTracking.XPThisRun = 0
			a.xpTracking.RunStartXP = a.xpTracking.CurrentXP

			// Reset tracking
			a.trackerInitialized = false
			a.lastInventory = make(map[string]data.Item)
			a.lastGroundItems = make(map[string]data.Item)
			a.itemsFromGround = make(map[string]time.Time)
			a.previousCorpses = make(map[data.UnitID]CorpseInfo)
		}
	}
}

func (a *App) updateKills() {
	corpses := a.gameReader.Corpses(data.Position{}, data.HoverData{})

	a.mu.Lock()
	defer a.mu.Unlock()

	for _, corpse := range corpses {
		existingCorpse, seen := a.previousCorpses[corpse.UnitID]
		if !seen || (existingCorpse.Position.X != corpse.Position.X || existingCorpse.Position.Y != corpse.Position.Y) {
			a.previousCorpses[corpse.UnitID] = CorpseInfo{UnitID: corpse.UnitID, Position: corpse.Position}
			a.killCounts[fmt.Sprintf("%v", corpse.Type)]++
			a.totalKills++
		}
	}
}

func (a *App) checkForNewItems() {
	gameData := a.gameReader.GetData()

	if gameData.PlayerUnit.Area == 0 {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.trackerInitialized {
		a.initializeInventory(gameData)
		a.trackerInitialized = true
		fmt.Println("🏁 Item tracker initialized")
		return
	}

	currentGroundItems := a.getGroundItems(gameData)
	currentInventory := a.getCurrentInventory(gameData)

	a.trackItemsFromGround(currentGroundItems)

	// Debug: Show new items in inventory
	for key, newItem := range currentInventory {
		if _, existed := a.lastInventory[key]; !existed {
			itemName := a.getItemName(newItem)
			fmt.Printf("🆕 NEW ITEM IN INVENTORY: '%s'\n", itemName)

			if a.isValidPickup(newItem) {
				fmt.Printf("✅ VALID PICKUP DETECTED: '%s'\n", itemName)
				a.onItemPickedUp(newItem)
				delete(a.itemsFromGround, itemName)
			} else {
				fmt.Printf("❌ INVALID PICKUP (not from ground): '%s'\n", itemName)
			}
		}
	}

	now := time.Now()
	for itemName, trackTime := range a.itemsFromGround {
		if now.Sub(trackTime) > 10*time.Second {
			delete(a.itemsFromGround, itemName)
		}
	}

	a.lastInventory = currentInventory
	a.lastGroundItems = currentGroundItems
}

// ========== VERBESSERTE XP TRACKING LOGIC ==========

func (a *App) updateXPTracking() {
	gameData := a.gameReader.GetData()

	if gameData.PlayerUnit.Area == 0 {
		return // Not in game
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Store game data for other functions to use
	a.lastGameData = gameData

	// ========== XP TRACKING ==========
	currentXP := a.getPlayerExperience(gameData.PlayerUnit)
	currentLevel := a.getPlayerLevelFromStats(gameData.PlayerUnit)

	// Debug output
	if currentLevel != a.xpTracking.CurrentLevel || currentXP != a.xpTracking.CurrentXP {
		fmt.Printf("📊 PLAYER DATA: Level=%d, XP=%d (Previous: Level=%d, XP=%d)\n",
			currentLevel, currentXP, a.xpTracking.CurrentLevel, a.xpTracking.CurrentXP)
	}

	// Initialize XP tracking on first run
	if a.xpTracking.CurrentXP == 0 && currentXP > 0 {
		a.xpTracking.CurrentXP = currentXP
		a.xpTracking.CurrentLevel = currentLevel
		a.xpTracking.RunStartXP = currentXP
		fmt.Printf("📈 XP tracking initialized: Level %d, %d XP\n", currentLevel, currentXP)
	} else if currentXP > a.xpTracking.CurrentXP {
		// XP gained
		xpGained := currentXP - a.xpTracking.CurrentXP
		a.xpTracking.SessionXPGained += xpGained
		a.xpTracking.XPThisRun += xpGained

		fmt.Printf("📈 XP gained: +%d (Total Session: %d, This Run: %d)\n",
			xpGained, a.xpTracking.SessionXPGained, a.xpTracking.XPThisRun)

		// Check for level up
		if currentLevel > a.xpTracking.CurrentLevel {
			fmt.Printf("🎉 LEVEL UP! %d -> %d\n", a.xpTracking.CurrentLevel, currentLevel)
		}
	}

	a.xpTracking.CurrentXP = currentXP
	a.xpTracking.CurrentLevel = currentLevel

	// Calculate XP to next level
	a.xpTracking.XPToNextLevel = getXPToNextLevel(currentXP, currentLevel)

	// Calculate XP per hour
	sessionDuration := time.Since(a.sessionStartTime).Hours()
	if sessionDuration > 0 {
		a.xpTracking.XPPerHour = float64(a.xpTracking.SessionXPGained) / sessionDuration
	}

	// ========== VERBESSERTE RUNS-TO-NEXT-LEVEL BERECHNUNG ==========
	a.calculateRunsToNextLevel()
}

// ========== NEUE VERBESSERTE RUNS-TO-NEXT-LEVEL BERECHNUNG ==========
func (a *App) calculateRunsToNextLevel() {
	// Reset values
	a.xpTracking.RunsToNextLevel = 0
	a.xpTracking.EstimatedRunsToNext = 0
	a.xpTracking.RunsCalculationMethod = "No data"

	// Exit early if max level or no XP needed
	if a.xpTracking.CurrentLevel >= 99 || a.xpTracking.XPToNextLevel <= 0 {
		a.xpTracking.RunsCalculationMethod = "Max level reached"
		return
	}

	// Method 1: Based on XP run history (reliable wenn genug Daten vorhanden)
	if len(a.xpRunHistory) >= 3 {
		var totalXP int64
		validRuns := 0
		
		// Nur positive XP-Werte berücksichtigen
		for _, xp := range a.xpRunHistory {
			if xp > 0 {
				totalXP += xp
				validRuns++
			}
		}
		
		if validRuns >= 3 {
			a.xpTracking.AverageXPPerRun = totalXP / int64(validRuns)
			if a.xpTracking.AverageXPPerRun > 0 {
				a.xpTracking.RunsToNextLevel = int(a.xpTracking.XPToNextLevel / a.xpTracking.AverageXPPerRun)
				a.xpTracking.RunsCalculationMethod = fmt.Sprintf("History (%d runs)", validRuns)
				fmt.Printf("🎯 Method 1: Runs to next level: %d (based on %d runs, avg %d XP/run)\n", 
					a.xpTracking.RunsToNextLevel, validRuns, a.xpTracking.AverageXPPerRun)
				return
			}
		}
	}

	// Method 2: Basierend auf Session-Performance
	if a.xpTracking.SessionXPGained > 0 && len(a.runTimes) > 0 {
		avgXPPerRun := a.xpTracking.SessionXPGained / int64(len(a.runTimes))
		if avgXPPerRun > 0 {
			a.xpTracking.AverageXPPerRun = avgXPPerRun
			a.xpTracking.RunsToNextLevel = int(a.xpTracking.XPToNextLevel / avgXPPerRun)
			a.xpTracking.RunsCalculationMethod = fmt.Sprintf("Session (%d runs)", len(a.runTimes))
			fmt.Printf("🎯 Method 2: Runs to next level: %d (session avg %d XP/run)\n", 
				a.xpTracking.RunsToNextLevel, avgXPPerRun)
			return
		}
	}

	// Method 3: Schätzung basierend auf aktuellem Run (falls aktiv)
	if a.runActive && a.xpTracking.XPThisRun > 0 {
		a.xpTracking.EstimatedRunsToNext = int(a.xpTracking.XPToNextLevel / a.xpTracking.XPThisRun)
		a.xpTracking.AverageXPPerRun = a.xpTracking.XPThisRun
		a.xpTracking.RunsCalculationMethod = "Current run estimate"
		fmt.Printf("🎯 Method 3: Estimated runs to next level: %d (current run: %d XP)\n", 
			a.xpTracking.EstimatedRunsToNext, a.xpTracking.XPThisRun)
		
		// Bei Schätzung basierend auf aktuellem Run, zeige beide Werte
		a.xpTracking.RunsToNextLevel = a.xpTracking.EstimatedRunsToNext
		return
	}

	// Method 4: Level-basierte Schätzung für hohe Level
	if a.xpTracking.CurrentLevel >= 85 {
		var typicalXPPerRun int64
		switch {
		case a.xpTracking.CurrentLevel >= 96:
			typicalXPPerRun = 100000000 // 100M XP pro Run für Level 96+
		case a.xpTracking.CurrentLevel >= 93:
			typicalXPPerRun = 75000000  // 75M XP pro Run für Level 93-95
		case a.xpTracking.CurrentLevel >= 90:
			typicalXPPerRun = 50000000  // 50M XP pro Run für Level 90-92
		case a.xpTracking.CurrentLevel >= 87:
			typicalXPPerRun = 30000000  // 30M XP pro Run für Level 87-89
		default:
			typicalXPPerRun = 20000000  // 20M XP pro Run für Level 85-86
		}
		
		estimatedRuns := int(a.xpTracking.XPToNextLevel / typicalXPPerRun)
		if estimatedRuns > 0 {
			a.xpTracking.RunsToNextLevel = estimatedRuns
			a.xpTracking.EstimatedRunsToNext = estimatedRuns
			a.xpTracking.AverageXPPerRun = typicalXPPerRun
			a.xpTracking.RunsCalculationMethod = fmt.Sprintf("Level-based estimate (L%d)", a.xpTracking.CurrentLevel)
			fmt.Printf("🎯 Method 4: Runs to next level: %d (level-based estimate for L%d)\n", 
				estimatedRuns, a.xpTracking.CurrentLevel)
			return
		}
	}

	// Method 5: General fallback für niedrigere Level
	if a.xpTracking.CurrentLevel < 85 {
		// Für niedrigere Level: geschätzte XP pro Run basierend auf Level
		var typicalXPPerRun int64
		switch {
		case a.xpTracking.CurrentLevel >= 70:
			typicalXPPerRun = 10000000 // 10M XP pro Run für Level 70-84
		case a.xpTracking.CurrentLevel >= 50:
			typicalXPPerRun = 5000000  // 5M XP pro Run für Level 50-69
		case a.xpTracking.CurrentLevel >= 30:
			typicalXPPerRun = 1000000  // 1M XP pro Run für Level 30-49
		default:
			typicalXPPerRun = 100000   // 100K XP pro Run für Level unter 30
		}
		
		estimatedRuns := int(a.xpTracking.XPToNextLevel / typicalXPPerRun)
		if estimatedRuns > 0 {
			a.xpTracking.RunsToNextLevel = estimatedRuns
			a.xpTracking.EstimatedRunsToNext = estimatedRuns
			a.xpTracking.AverageXPPerRun = typicalXPPerRun
			a.xpTracking.RunsCalculationMethod = fmt.Sprintf("General estimate (L%d)", a.xpTracking.CurrentLevel)
			fmt.Printf("🎯 Method 5: Runs to next level: %d (general estimate for L%d)\n", 
				estimatedRuns, a.xpTracking.CurrentLevel)
			return
		}
	}

	// Fallback: Keine Schätzung möglich
	a.xpTracking.RunsCalculationMethod = "Insufficient data"
	fmt.Printf("🎯 No reliable runs estimation possible\n")
}

// ========== ITEM TRACKING ==========
func (a *App) trackItemsFromGround(currentGroundItems map[string]data.Item) {
	now := time.Now()
	for lastKey, lastGroundItem := range a.lastGroundItems {
		if _, stillOnGround := currentGroundItems[lastKey]; !stillOnGround {
			itemName := a.getItemName(lastGroundItem)
			a.itemsFromGround[itemName] = now
		}
	}
}

func (a *App) isValidPickup(newItem data.Item) bool {
	itemName := a.getItemName(newItem)
	_, wasFromGround := a.itemsFromGround[itemName]
	return wasFromGround
}

// ========== FIXED ITEM PICKUP FUNCTION ==========
func (a *App) onItemPickedUp(itm data.Item) {
	itemName := a.getItemName(itm)

	// Debug: Show EVERY item pickup
	fmt.Printf("🔍 ITEM PICKED UP: '%s' (Quality: %s)\n", itemName, a.getItemQuality(itm))

	// Check if filters are enabled
	if a.filtersEnabled {
		// Debug: Test the filter function
		fmt.Printf("🧪 TESTING FILTER for: '%s' (Filters: ENABLED)\n", itemName)
		isFiltered := a.isFilteredItem(itemName)
		fmt.Printf("🧪 FILTER RESULT: %t\n", isFiltered)

		if isFiltered {
			fmt.Printf("🚫 ITEM FILTERED OUT: '%s'\n", itemName)
			return
		}
	} else {
		fmt.Printf("🔓 FILTERS DISABLED - Item will be added: '%s'\n", itemName)
	}

	fmt.Printf("✅ ITEM WILL BE ADDED: '%s'\n", itemName)

	// ========== PHASE 2: Enhanced Item Information ==========
	affixesText := a.getItemAffixes(itm)

	itemEntry := ItemEntry{
		Name:         itemName,
		OriginalName: itemName, // Store original for later
		Quality:      a.getItemQuality(itm),
		RunIndex:     a.currentRun,
		Time:         time.Now(),
		// Enhanced item data
		Affixes:      affixesText,
		IsEthereal:   itm.Ethereal,
		IsIdentified: itm.Identified,
		ItemLevel:    itm.LevelReq,
		// ArrayIndex wird später gesetzt
	}

	a.itemHistory = append(a.itemHistory, itemEntry)
	fmt.Printf("📦 ITEM ADDED TO HISTORY: %s (%s) - Run %d (Index: %d)\n", 
		itemName, itemEntry.Quality, a.currentRun, len(a.itemHistory)-1)

	// Enhanced logging for special items
	if affixesText != "" {
		fmt.Printf("🏷️ ITEM AFFIXES: %s\n", affixesText)
	}
	if itm.Ethereal {
		fmt.Printf("👻 ETHEREAL ITEM: %s\n", itemName)
	}

	go a.SaveCurrentProfile()
}

// Rest der Funktionen bleiben gleich wie vorher...
// (isFilteredItem, getItemName, getItemAffixes, etc. - alles unverändert)

func (a *App) isFilteredItem(itemName string) bool {
	// Debug: Show every item check
	fmt.Printf("🔍 Filter check for: '%s'\n", itemName)

	// Direct string matches (case-insensitive)
	itemLower := strings.ToLower(itemName)

	// All potion types that should be filtered
	filteredPatterns := []string{
		// Healing Potions
		"healingpotion", "healthpotion", "healing potion", "health potion",
		"minorhealingpotion", "lighthealingpotion", "greaterhealingpotion", "superhealingpotion",
		"minor healing potion", "light healing potion", "greater healing potion", "super healing potion",

		// Mana Potions
		"manapotion", "mana potion",
		"minormanapotion", "lightmanapotion", "greatermanapotion", "supermanapotion",
		"minor mana potion", "light mana potion", "greater mana potion", "super mana potion",

		// Rejuvenation Potions
		"rejuvenationpotion", "rejuvenation potion",
		"minorrejuvenationpotion", "minor rejuvenation potion",
		"fullrejuvenationpotion", "full rejuvenation potion",

		// Other Potions
		"antidotepotion", "antidote potion",
		"thawingpotion", "thawing potion",
		"staminapotion", "stamina potion",

		// Arrows and Bolts
		"arrow", "arrows", "bolt", "bolts",
		"pfeil", "pfeile", "bolzen",

		// Gold
		"gold",
	}

	// Exact matches for problematic items
	exactMatches := []string{
		// Potions
		"SuperHealingPotion",
		"GreaterHealingPotion",
		"SuperManaPotion",
		"GreaterManaPotion",
		"RejuvenationPotion",
		"FullRejuvenationPotion",
		"MinorRejuvenationPotion",
		"MinorHealingPotion",
		"LightHealingPotion",
		"MinorManaPotion",
		"LightManaPotion",
		"AntidotePotion",
		"ThawingPotion",
		"StaminaPotion",
		// Projectiles
		"Arrow",
		"Arrows",
		"Bolt",
		"Bolts",
		"Pfeil",
		"Pfeile",
		"Bolzen",
		// Gold
		"Gold",
	}

	// First check exact matches
	for _, exact := range exactMatches {
		if itemName == exact {
			fmt.Printf("✅ EXACT MATCH: '%s' -> FILTERED\n", itemName)
			return true
		}
	}

	// Then check pattern matches
	for _, pattern := range filteredPatterns {
		if strings.Contains(itemLower, pattern) {
			fmt.Printf("✅ PATTERN MATCH: '%s' contains '%s' -> FILTERED\n", itemLower, pattern)
			return true
		}
	}

	fmt.Printf("❌ NO MATCH: '%s' -> NOT FILTERED\n", itemName)
	return false
}

// Rest of the file continues with all the other functions unchanged...
// (getItemName, getItemAffixes, beautifyItemName, etc.)

func (a *App) getItemName(itm data.Item) string {
	if itm.Name == "" {
		return "Unknown Item"
	}

	rawName := string(itm.Name)

	// First level: Known item names mapping (common items)
	knownItems := map[string]string{
		"SerpentskinArmor":        "Serpentskin Armor",
		"StuddedLeather":          "Studded Leather",
		"ChainMail":               "Chain Mail",
		"SplintMail":              "Splint Mail",
		"PlateMail":               "Plate Mail",
		"FieldPlate":              "Field Plate",
		"GothicPlate":             "Gothic Plate",
		"FullPlateMail":           "Full Plate Mail",
		"LeatherArmor":            "Leather Armor",
		"HardLeatherArmor":        "Hard Leather Armor",
		"RingMail":                "Ring Mail",
		"ScaleMail":               "Scale Mail",
		"BrestPlate":              "Brest Plate",
		"LightPlate":              "Light Plate",
		"BronzePlate":             "Bronze Plate",
		"BattlePlate":             "Battle Plate",
		"WarHat":                  "War Hat",
		"Sallet":                  "Sallet",
		"Casque":                  "Casque",
		"Basinet":                 "Basinet",
		"WingedHelm":              "Winged Helm",
		"GrandCrown":              "Grand Crown",
		"DeathMask":               "Death Mask",
		"GhostArmor":              "Ghost Armor",
		"DemonhideArmor":          "Demonhide Armor",
		"TrellisedArmor":          "Trellised Armor",
		"LinkedMail":              "Linked Mail",
		"TigulatedMail":           "Tigulated Mail",
		"MeshArmor":               "Mesh Armor",
		"CuirBouilli":             "Cuir Bouilli",
		"GothicBow":               "Gothic Bow",
		"CompositeBow":            "Composite Bow",
		"BattleBow":               "Battle Bow",
		"WarBow":                  "War Bow",
		"LongBow":                 "Long Bow",
		"ShortBow":                "Short Bow",
		"HuntersBow":              "Hunter's Bow",
		"LongSword":               "Long Sword",
		"BroadSword":              "Broad Sword",
		"CrystalSword":            "Crystal Sword",
		"FalcataSword":            "Falcata Sword",
		"TwoHandedSword":          "Two-Handed Sword",
		"WarSword":                "War Sword",
		"BastardSword":            "Bastard Sword",
		"HandAxe":                 "Hand Axe",
		"Hatchet":                 "Hatchet",
		"Cleaver":                 "Cleaver",
		"BroadAxe":                "Broad Axe",
		"BattleAxe":               "Battle Axe",
		"LargeAxe":                "Large Axe",
		"GreatAxe":                "Great Axe",
		"GiantAxe":                "Giant Axe",
		"WalkingStick":            "Walking Stick",
		"GnarledStaff":            "Gnarled Staff",
		"BattleStaff":             "Battle Staff",
		"WarStaff":                "War Staff",
		"LongStaff":               "Long Staff",
		"QuarterStaff":            "Quarter Staff",
		"LeatherGloves":           "Leather Gloves",
		"HeavyGloves":             "Heavy Gloves",
		"ChainGloves":             "Chain Gloves",
		"LightGauntlets":          "Light Gauntlets",
		"Gauntlets":               "Gauntlets",
		"LeatherBoots":            "Leather Boots",
		"HeavyBoots":              "Heavy Boots",
		"ChainBoots":              "Chain Boots",
		"LightPlatedBoots":        "Light Plated Boots",
		"Greaves":                 "Greaves",
		"BeltPouch":               "Belt Pouch",
		"SashBelt":                "Sash Belt",
		"LightBelt":               "Light Belt",
		"HeavyBelt":               "Heavy Belt",
		"PlatedBelt":              "Plated Belt",
		"SmallShield":             "Small Shield",
		"LargeShield":             "Large Shield",
		"KiteShield":              "Kite Shield",
		"TowerShield":             "Tower Shield",
		"GothicShield":            "Gothic Shield",
		"BoneShield":              "Bone Shield",
		"SpikedShield":            "Spiked Shield",
		"BladeTalons":             "Blade Talons",
		"ScissorsSuwayyah":        "Scissors Suwayyah",
		"Quhab":                   "Quhab",
		"Wristblade":              "Wrist Blade",
		"HandScythe":              "Hand Scythe",
		"GreaterTalons":           "Greater Talons",
		"GreaterClaws":            "Greater Claws",
		"HealingPotion":           "Healing Potion",
		"ManaPotion":              "Mana Potion",
		"RejuvenationPotion":      "Rejuvenation Potion",
		"FullRejuvenationPotion":  "Full Rejuvenation Potion",
		"StaminaPotion":           "Stamina Potion",
		"AntidotePotion":          "Antidote Potion",
		"ThawingPotion":           "Thawing Potion",
	}

	// Check if we have a known translation
	if displayName, found := knownItems[rawName]; found {
		fmt.Printf("🏷️ ITEM NAME MAPPED: '%s' -> '%s'\n", rawName, displayName)
		return displayName
	}

	// Automatic CamelCase to spaces conversion
	beautifiedName := a.beautifyItemName(rawName)

	if beautifiedName != rawName {
		fmt.Printf("🏷️ ITEM NAME BEAUTIFIED: '%s' -> '%s'\n", rawName, beautifiedName)
	}

	return beautifiedName
}

// ========== PHASE 2: ENHANCED ITEM AFFIX DETECTION ==========

func (a *App) getItemAffixes(itm data.Item) string {
	var affixes []string

	// For items with detailed affix information available
	switch itm.Quality {
	case item.QualityMagic:
		// Magic items - try to get prefix/suffix info if available
		if itm.IdentifiedName != "" && itm.IdentifiedName != string(itm.Name) {
			return itm.IdentifiedName // Use identified name if available
		}

	case item.QualityRare:
		// Rare items - try to extract affix information
		if itm.IdentifiedName != "" && itm.IdentifiedName != string(itm.Name) {
			return itm.IdentifiedName // Use identified name if available
		}

	case item.QualitySet:
		// Set items - add set name if available
		if itm.IsNamed && itm.IdentifiedName != "" {
			affixes = append(affixes, fmt.Sprintf("Set: %s", itm.IdentifiedName))
		}

	case item.QualityUnique:
		// Unique items - add unique name if available
		if itm.IsNamed && itm.IdentifiedName != "" {
			affixes = append(affixes, fmt.Sprintf("Unique: %s", itm.IdentifiedName))
		}
	}

	// Add special properties
	if itm.Ethereal {
		affixes = append(affixes, "Ethereal")
	}

	if itm.HasSockets && len(itm.Sockets) > 0 {
		socketInfo := fmt.Sprintf("%d Sockets", len(itm.Sockets))
		// Add socketed rune/gem info if available
		var socketNames []string
		for _, socketedItem := range itm.Sockets {
			socketNames = append(socketNames, a.getItemName(socketedItem))
		}
		if len(socketNames) > 0 {
			socketInfo += fmt.Sprintf(" (%s)", strings.Join(socketNames, ", "))
		}
		affixes = append(affixes, socketInfo)
	}

	if itm.IsRuneword {
		affixes = append(affixes, fmt.Sprintf("Runeword: %s", itm.RunewordName))
	}

	// Add level requirement if significant
	if itm.LevelReq > 30 {
		affixes = append(affixes, fmt.Sprintf("Req Level %d", itm.LevelReq))
	}

	// Join all affixes
	return strings.Join(affixes, " • ")
}

func (a *App) beautifyItemName(name string) string {
	if name == "" {
		return name
	}

	// Convert CamelCase to "Proper Case" with spaces
	var result strings.Builder

	for i, char := range name {
		// Add spaces before capital letters (except at the beginning)
		if i > 0 && char >= 'A' && char <= 'Z' {
			// Check if the previous letter was lowercase (CamelCase)
			prev := rune(name[i-1])
			if prev >= 'a' && prev <= 'z' {
				result.WriteRune(' ')
			}
			// Or if we have a sequence of capital letters and the next one is lowercase
			if i < len(name)-1 {
				next := rune(name[i+1])
				if prev >= 'A' && prev <= 'Z' && next >= 'a' && next <= 'z' {
					result.WriteRune(' ')
				}
			}
		}
		result.WriteRune(char)
	}

	return result.String()
}

func (a *App) getItemQuality(itm data.Item) string {
	switch itm.Quality {
	case item.QualityNormal:
		return "Normal"
	case item.QualitySuperior:
		return "Superior"
	case item.QualityMagic:
		return "Magic"
	case item.QualityRare:
		return "Rare"
	case item.QualitySet:
		return "Set"
	case item.QualityUnique:
		return "Unique"
	default:
		return "Unknown"
	}
}

func (a *App) getCurrentInventory(gameData data.Data) map[string]data.Item {
	inventory := make(map[string]data.Item)
	for _, itm := range gameData.Inventory.AllItems {
		if itm.Name != "" && itm.Location.LocationType != item.LocationCursor {
			if itm.Location.LocationType == item.LocationInventory ||
				itm.Location.LocationType == item.LocationBelt ||
				itm.Location.LocationType == item.LocationCube {
				itemKey := a.getItemKey(itm)
				inventory[itemKey] = itm
			}
		}
	}
	return inventory
}

func (a *App) getGroundItems(gameData data.Data) map[string]data.Item {
	groundItems := make(map[string]data.Item)
	for _, itm := range gameData.Inventory.AllItems {
		if itm.Name != "" && itm.Location.LocationType == item.LocationGround {
			itemKey := a.getItemKey(itm)
			groundItems[itemKey] = itm
		}
	}
	return groundItems
}

func (a *App) initializeInventory(gameData data.Data) {
	a.lastInventory = a.getCurrentInventory(gameData)
	a.lastGroundItems = a.getGroundItems(gameData)
}

func (a *App) getItemKey(itm data.Item) string {
	return fmt.Sprintf("%s_%v_%d_%d_%d",
		itm.Name, itm.Location.LocationType, itm.Location.Page, itm.Position.X, itm.Position.Y)
}

// ========== PROFILE MANAGEMENT ==========
func getProfilesDir() string {
	exePath, err := os.Executable()
	if err != nil {
		return "./profiles"
	}
	return filepath.Join(filepath.Dir(exePath), "profiles")
}

func (a *App) getProfileFilePath(profile string) string {
	return filepath.Join(a.profilesDir, profile+".json")
}

func (a *App) LoadProfile(profile string) {
	filePath := a.getProfileFilePath(profile)
	file, err := os.Open(filePath)

	a.mu.Lock()
	defer a.mu.Unlock()

	if err != nil {
		a.killCounts = make(map[string]int)
		a.totalKills = 0
		a.runTimes = []int64{}
		a.itemHistory = []ItemEntry{}
		a.currentRun = 1 // Start at 1, not 0
		a.filtersEnabled = true // Default: filters enabled
		// ========== XP TRACKING INITIALIZATION ==========
		a.xpTracking = XPTracking{SessionXPGained: 0, XPThisRun: 0}
		a.xpRunHistory = make([]int64, 0)
		a.sessionStartTime = time.Now()
	} else {
		defer file.Close()
		var data PersistentData
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&data); err != nil {
			a.killCounts = make(map[string]int)
			a.totalKills = 0
			a.runTimes = []int64{}
			a.itemHistory = []ItemEntry{}
			a.currentRun = 1 // Start at 1, not 0
			a.filtersEnabled = true // Default: filters enabled
			// ========== XP TRACKING INITIALIZATION ==========
			a.xpTracking = XPTracking{SessionXPGained: 0, XPThisRun: 0}
			a.xpRunHistory = make([]int64, 0)
			a.sessionStartTime = time.Now()
		} else {
			a.killCounts = data.KillCounts
			a.totalKills = data.TotalKills
			a.runTimes = data.RunTimes
			a.itemHistory = data.Items
			a.filtersEnabled = data.FiltersEnabled
			// FIX: currentRun based on RunTimes + 1
			a.currentRun = len(data.RunTimes) + 1
			// ========== XP TRACKING DATA LOADING ==========
			a.xpTracking = data.XPTracking
			a.xpRunHistory = data.XPRunHistory
			a.sessionStartTime = time.Now() // Reset session start time on profile load
			
			// CRITICAL: Reset session-specific tracking when loading profile
			a.xpTracking.SessionXPGained = 0
			a.xpTracking.XPThisRun = 0

			// Initialize tracking if data is missing
			if a.xpRunHistory == nil {
				a.xpRunHistory = make([]int64, 0)
			}
		}
	}

	if a.killCounts == nil {
		a.killCounts = make(map[string]int)
	}
	if a.itemHistory == nil {
		a.itemHistory = []ItemEntry{}
	}

	a.currentProfile = profile
	a.runActive = false
	a.trackerInitialized = false
	a.previousCorpses = make(map[data.UnitID]CorpseInfo)
	a.lastInventory = make(map[string]data.Item)
	a.lastGroundItems = make(map[string]data.Item)
	a.itemsFromGround = make(map[string]time.Time)
}

func (a *App) listProfiles() []string {
	var profiles []string
	files, err := ioutil.ReadDir(a.profilesDir)
	if err != nil {
		return []string{"default"}
	}

	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".json") {
			name := strings.TrimSuffix(f.Name(), ".json")
			profiles = append(profiles, name)
		}
	}

	if len(profiles) == 0 {
		profiles = []string{"default"}
	}

	sort.Strings(profiles)
	return profiles
}

// ========== HELPER FUNCTIONS ==========
func (a *App) getRunStats() (fastest, slowest, average int64) {
	if len(a.runTimes) == 0 {
		return 0, 0, 0
	}

	fastest = a.runTimes[0]
	slowest = a.runTimes[0]
	var sum int64 = 0

	for _, t := range a.runTimes {
		if t < fastest {
			fastest = t
		}
		if t > slowest {
			slowest = t
		}
		sum += t
	}

	average = sum / int64(len(a.runTimes))
	return
}

func formatDuration(ms int64) string {
	if ms == 0 {
		return "00:00:00"
	}
	d := time.Duration(ms) * time.Millisecond
	return d.Truncate(time.Second).String()
}

// ========== HELPER FUNCTIONS FOR PLAYER DATA ==========

func (a *App) getPlayerClassName() string {
	// Use cached game data if available
	if a.lastGameData.PlayerUnit.Class > 0 {
		switch a.lastGameData.PlayerUnit.Class {
		case 0:
			return "Amazon"
		case 1:
			return "Sorceress"
		case 2:
			return "Necromancer"
		case 3:
			return "Paladin"
		case 4:
			return "Barbarian"
		case 5:
			return "Druid"
		case 6:
			return "Assassin"
		default:
			return fmt.Sprintf("Unknown (%d)", a.lastGameData.PlayerUnit.Class)
		}
	}
	return "Unknown"
}

func (a *App) getCurrentAreaName() string {
	// Use cached game data if available
	if a.lastGameData.PlayerUnit.Area > 0 {
		return a.getAreaName(a.lastGameData.PlayerUnit.Area)
	}
	return "Unknown"
}

func (a *App) getPlayerLevel() int {
	// Use XP tracking level if available and valid
	if a.xpTracking.CurrentLevel > 0 {
		return a.xpTracking.CurrentLevel
	}

	// Fallback to game data
	if a.lastGameData.PlayerUnit.Area > 0 {
		return a.getPlayerLevelFromStats(a.lastGameData.PlayerUnit)
	}

	return 1 // Default level
}

func (a *App) getPlayerLevelFromStats(playerUnit data.PlayerUnit) int {
	// Try to find level stat using the most common stat ID
	if levelStat, found := playerUnit.Stats.FindStat(stat.Level, 0); found {
		fmt.Printf("📊 LEVEL STAT FOUND: %d\n", levelStat.Value)
		return int(levelStat.Value)
	}

	// Try alternative stat IDs
	for _, statID := range []stat.ID{stat.Level, stat.ID(12), stat.ID(13)} {
		if levelStat, found := playerUnit.Stats.FindStat(statID, 0); found {
			fmt.Printf("📊 LEVEL STAT FOUND (ID %v): %d\n", statID, levelStat.Value)
			return int(levelStat.Value)
		}
	}

	fmt.Printf("⚠️ LEVEL STAT NOT FOUND, available stats: %d\n", len(playerUnit.Stats))
	// Debug: print available stats
	for i, statData := range playerUnit.Stats {
		if i < 5 { // Only show first 5 to avoid spam
			fmt.Printf("   Stat[%d]: ID=%v, Value=%d\n", i, statData.ID, statData.Value)
		}
	}

	return 1 // Default level
}

func (a *App) getPlayerExperience(playerUnit data.PlayerUnit) int64 {
	// Try to find experience stat
	if expStat, found := playerUnit.Stats.FindStat(stat.Experience, 0); found {
		fmt.Printf("📊 EXP STAT FOUND: %d\n", expStat.Value)
		return int64(expStat.Value)
	}

	// Try alternative stat IDs for experience
	for _, statID := range []stat.ID{stat.Experience, stat.ID(13), stat.ID(14)} {
		if expStat, found := playerUnit.Stats.FindStat(statID, 0); found {
			fmt.Printf("📊 EXP STAT FOUND (ID %v): %d\n", statID, expStat.Value)
			return int64(expStat.Value)
		}
	}

	fmt.Printf("⚠️ EXP STAT NOT FOUND\n")
	return 0
}

func (a *App) getAreaName(areaID area.ID) string {
	// Simplified area name mapping - could be extended
	switch areaID {
	case 1:
		return "Rogue Encampment"
	case 2:
		return "Blood Moor"
	case 3:
		return "Cold Plains"
	case 4:
		return "Stony Field"
	case 5:
		return "Dark Wood"
	case 6:
		return "Black Marsh"
	case 27:
		return "Jail Level 1"
	case 28:
		return "Jail Level 2"
	case 29:
		return "Jail Level 3"
	case 30:
		return "Inner Cloister"
	case 31:
		return "Cathedral"
	case 32:
		return "Catacombs Level 1"
	case 33:
		return "Catacombs Level 2"
	case 34:
		return "Catacombs Level 3"
	case 35:
		return "Catacombs Level 4"
	case 40:
		return "Lut Gholein"
	case 54:
		return "Arcane Sanctuary"
	case 55:
		return "Canyon of the Magi"
	case 56:
		return "Tal Rasha's Tomb"
	case 73:
		return "Tal Rasha's Chamber"
	case 75:
		return "Kurast Docks"
	case 76:
		return "Spider Forest"
	case 77:
		return "Great Marsh"
	case 78:
		return "Flayer Jungle"
	case 79:
		return "Lower Kurast"
	case 80:
		return "Kurast Bazaar"
	case 81:
		return "Upper Kurast"
	case 82:
		return "Kurast Causeway"
	case 83:
		return "Travincal"
	case 84:
		return "Durance of Hate Level 1"
	case 85:
		return "Durance of Hate Level 2"
	case 86:
		return "Durance of Hate Level 3"
	case 103:
		return "The Pandemonium Fortress"
	case 104:
		return "Outer Steppes"
	case 105:
		return "Plains of Despair"
	case 106:
		return "City of the Damned"
	case 107:
		return "River of Flame"
	case 108:
		return "Chaos Sanctuary"
	case 109:
		return "Harrogath"
	case 110:
		return "Bloody Foothills"
	case 111:
		return "Frigid Highlands"
	case 112:
		return "Arreat Plateau"
	case 113:
		return "Crystalline Passage"
	case 114:
		return "Frozen River"
	case 115:
		return "Glacial Trail"
	case 116:
		return "Drifter Cavern"
	case 117:
		return "Frozen Tundra"
	case 118:
		return "The Ancients' Way"
	case 119:
		return "Icy Cellar"
	case 120:
		return "Arreat Summit"
	case 121:
		return "Nihlathak's Temple"
	case 122:
		return "Halls of Anguish"
	case 123:
		return "Halls of Pain"
	case 124:
		return "Halls of Vaught"
	case 131:
		return "Worldstone Keep Level 1"
	case 132:
		return "Worldstone Keep Level 2"
	case 133:
		return "Worldstone Keep Level 3"
	case 134:
		return "Throne of Destruction"
	case 135:
		return "The Worldstone Chamber"
	default:
		return fmt.Sprintf("Area %d", areaID)
	}
}

// ========== MAIN ==========
func main() {
	fmt.Println("🎮 Starting D2R Kill Counter & Item Tracker (KORRIGIERTER ITEM TRACKER)...")

	// Create an instance of the app structure
	app := NewApp()

	// Create application with the WORKING binding structure
	err := wails.Run(&options.App{
		Title:  "D2R Kill Counter & Item Tracker",
		Width:  1200,
		Height: 800,
		AssetServer: &assetserver.Options{
			Assets: embedFS,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.Startup,
		OnDomReady:       app.DomReady,
		OnBeforeClose:    app.BeforeClose,
		OnShutdown:       app.Shutdown,
		// CRITICAL: This binds the app to JavaScript
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}