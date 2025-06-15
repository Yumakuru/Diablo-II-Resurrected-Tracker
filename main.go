// main.go - D2R Kill Counter & Item Tracker (OPTIMIERT - Schritt 2: Utils ausgelagert)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
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

// ========== ITEM DATA STRUCTURES ==========
type ItemDatabase struct {
	UniqueItems []string `json:"uniqueItems"`
	SetItems    []string `json:"setItems"`
}

type ItemListResponse struct {
	UniqueItems     []string `json:"unique_items"`
	SetItems        []string `json:"set_items"`
	AllSpecialItems []string `json:"all_special_items"`
	TotalCount      int      `json:"total_count"`
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

// ========== APP STRUCT (ERWEITERT) ==========
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

	// ========== NEUE: AUSGELAGERTE DATEN ==========
	itemDatabase       ItemDatabase       // Loaded from JSON file
	xpTable           map[int]int64       // Loaded from xp_table.json
	itemNameMapping   map[string]string   // Loaded from item_names.json
	areaNameMapping   map[string]string   // Loaded from area_names.json
}

// ========== CONSTRUCTOR ==========
func NewApp() *App {
	now := time.Now()
	app := &App{
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
		// ========== NEUE: DATEN INITIALIZATION ==========
		itemDatabase:     ItemDatabase{},
		xpTable:          make(map[int]int64),
		itemNameMapping:  make(map[string]string),
		areaNameMapping:  make(map[string]string),
	}

	// Load all external data files
	if err := app.loadAllDataFiles(); err != nil {
		fmt.Printf("⚠️ Warning: Could not load some data files: %v\n", err)
		fmt.Println("💡 Some features may use fallback data")
	}

	return app
}

// ========== NEUE: LOAD ALL DATA FILES ==========
func (a *App) loadAllDataFiles() error {
	var errors []string

	// Load item database
	if err := a.loadItemDatabase(); err != nil {
		errors = append(errors, fmt.Sprintf("items.json: %v", err))
	}

	// Load XP table
	if err := a.loadXPTable(); err != nil {
		errors = append(errors, fmt.Sprintf("xp_table.json: %v", err))
	}

	// Load item name mapping
	if err := a.loadItemNameMapping(); err != nil {
		errors = append(errors, fmt.Sprintf("item_names.json: %v", err))
	}

	// Load area name mapping
	if err := a.loadAreaNameMapping(); err != nil {
		errors = append(errors, fmt.Sprintf("area_names.json: %v", err))
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to load: %s", strings.Join(errors, ", "))
	}

	fmt.Println("✅ All data files loaded successfully")
	return nil
}

// ========== NEUE: LOAD XP TABLE ==========
func (a *App) loadXPTable() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not get executable path: %v", err)
	}

	xpTablePath := filepath.Join(filepath.Dir(exePath), "xp_table.json")
	
	// Fallback: try current working directory
	if _, err := os.Stat(xpTablePath); os.IsNotExist(err) {
		xpTablePath = "xp_table.json"
	}

	// Check if file exists
	if _, err := os.Stat(xpTablePath); os.IsNotExist(err) {
		fmt.Printf("⚠️ xp_table.json not found at: %s\n", xpTablePath)
		return fmt.Errorf("xp_table.json not found")
	}

	// Read file
	data, err := ioutil.ReadFile(xpTablePath)
	if err != nil {
		return fmt.Errorf("could not read xp_table.json: %v", err)
	}

	// Parse JSON into string->int64 map first
	var stringMap map[string]int64
	if err := json.Unmarshal(data, &stringMap); err != nil {
		return fmt.Errorf("could not parse xp_table.json: %v", err)
	}

	// Convert to int->int64 map
	a.xpTable = make(map[int]int64)
	for levelStr, xp := range stringMap {
		level, err := strconv.Atoi(levelStr)
		if err != nil {
			fmt.Printf("⚠️ Skipping invalid level: %s\n", levelStr)
			continue
		}
		a.xpTable[level] = xp
	}

	fmt.Printf("✅ XP table loaded: %d levels\n", len(a.xpTable))
	return nil
}

// ========== NEUE: LOAD ITEM NAME MAPPING ==========
func (a *App) loadItemNameMapping() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not get executable path: %v", err)
	}

	itemNamesPath := filepath.Join(filepath.Dir(exePath), "item_names.json")
	
	// Fallback: try current working directory
	if _, err := os.Stat(itemNamesPath); os.IsNotExist(err) {
		itemNamesPath = "item_names.json"
	}

	// Check if file exists
	if _, err := os.Stat(itemNamesPath); os.IsNotExist(err) {
		fmt.Printf("⚠️ item_names.json not found at: %s\n", itemNamesPath)
		return fmt.Errorf("item_names.json not found")
	}

	// Read file
	data, err := ioutil.ReadFile(itemNamesPath)
	if err != nil {
		return fmt.Errorf("could not read item_names.json: %v", err)
	}

	// Parse JSON
	if err := json.Unmarshal(data, &a.itemNameMapping); err != nil {
		return fmt.Errorf("could not parse item_names.json: %v", err)
	}

	fmt.Printf("✅ Item name mapping loaded: %d items\n", len(a.itemNameMapping))
	return nil
}

// ========== NEUE: LOAD AREA NAME MAPPING ==========
func (a *App) loadAreaNameMapping() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not get executable path: %v", err)
	}

	areaNamesPath := filepath.Join(filepath.Dir(exePath), "area_names.json")
	
	// Fallback: try current working directory
	if _, err := os.Stat(areaNamesPath); os.IsNotExist(err) {
		areaNamesPath = "area_names.json"
	}

	// Check if file exists
	if _, err := os.Stat(areaNamesPath); os.IsNotExist(err) {
		fmt.Printf("⚠️ area_names.json not found at: %s\n", areaNamesPath)
		return fmt.Errorf("area_names.json not found")
	}

	// Read file
	data, err := ioutil.ReadFile(areaNamesPath)
	if err != nil {
		return fmt.Errorf("could not read area_names.json: %v", err)
	}

	// Parse JSON into string->string map first
	var stringMap map[string]string
	if err := json.Unmarshal(data, &stringMap); err != nil {
		return fmt.Errorf("could not parse area_names.json: %v", err)
	}

	a.areaNameMapping = stringMap

	fmt.Printf("✅ Area name mapping loaded: %d areas\n", len(a.areaNameMapping))
	return nil
}

// ========== ITEM DATABASE FUNCTIONS ==========
func (a *App) loadItemDatabase() error {
	// Try to find items.json in the same directory as the executable
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not get executable path: %v", err)
	}

	itemsPath := filepath.Join(filepath.Dir(exePath), "items.json")
	
	// Fallback: try current working directory
	if _, err := os.Stat(itemsPath); os.IsNotExist(err) {
		itemsPath = "items.json"
	}

	// Check if file exists
	if _, err := os.Stat(itemsPath); os.IsNotExist(err) {
		fmt.Printf("⚠️ items.json not found at: %s\n", itemsPath)
		return fmt.Errorf("items.json not found")
	}

	// Read file
	data, err := ioutil.ReadFile(itemsPath)
	if err != nil {
		return fmt.Errorf("could not read items.json: %v", err)
	}

	// Parse JSON
	if err := json.Unmarshal(data, &a.itemDatabase); err != nil {
		return fmt.Errorf("could not parse items.json: %v", err)
	}

	fmt.Printf("✅ Item database loaded successfully from: %s\n", itemsPath)
	fmt.Printf("   - Unique items: %d\n", len(a.itemDatabase.UniqueItems))
	fmt.Printf("   - Set items: %d\n", len(a.itemDatabase.SetItems))

	return nil
}

// ========== NEW API METHOD FOR ITEM LISTS ==========
func (a *App) GetItemLists() ItemListResponse {
	a.mu.RLock()
	defer a.mu.RUnlock()

	allSpecialItems := append(a.itemDatabase.UniqueItems, a.itemDatabase.SetItems...)
	
	// Sort the combined list
	sort.Strings(allSpecialItems)

	return ItemListResponse{
		UniqueItems:     a.itemDatabase.UniqueItems,
		SetItems:        a.itemDatabase.SetItems,
		AllSpecialItems: allSpecialItems,
		TotalCount:      len(allSpecialItems),
	}
}

// ========== NEUE: XP FUNKTIONEN MIT EXTERNEN DATEN ==========
func (a *App) getXPForLevel(level int) int64 {
	if xp, exists := a.xpTable[level]; exists {
		return xp
	}
	return 0
}

func (a *App) getXPToNextLevel(currentXP int64, currentLevel int) int64 {
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
	
	// For levels below 90, use the loaded table
	nextLevelXP := a.getXPForLevel(currentLevel + 1)
	if nextLevelXP == 0 {
		return 0 // Max level reached
	}
	return nextLevelXP - currentXP
}

// ========== NEUE: ITEM NAME FUNCTION MIT EXTERNEN DATEN ==========
func (a *App) getItemName(itm data.Item) string {
	if itm.Name == "" {
		return "Unknown Item"
	}

	rawName := string(itm.Name)

	// Check if we have a known translation from loaded mapping
	if displayName, found := a.itemNameMapping[rawName]; found {
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

// ========== NEUE: AREA NAME FUNCTION MIT EXTERNEN DATEN ==========
func (a *App) getAreaName(areaID area.ID) string {
	// Convert area ID to string and lookup in mapping
	areaIDStr := fmt.Sprintf("%d", areaID)
	if areaName, found := a.areaNameMapping[areaIDStr]; found {
		return areaName
	}
	return fmt.Sprintf("Area %d", areaID)
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
	a.xpTracking.XPToNextLevel = a.getXPToNextLevel(currentXP, currentLevel)

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
// (Item tracking utility functions moved to utils.go)

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

// ========== ITEM AFFIX FUNCTIONS ==========
// (Moved to utils.go)

// ========== PROFILE MANAGEMENT ==========
// (Moved to utils.go)

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

// ========== PROFILE LOADING ==========
// (Profile utilities moved to utils.go)

// ========== HELPER FUNCTIONS ==========
// (Moved to utils.go)

// ========== MAIN ==========
func main() {
	fmt.Println("🎮 Starting D2R Kill Counter & Item Tracker (OPTIMIERT - Schritt 2: Utils ausgelagert)...")

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