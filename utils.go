// utils.go - Utility Functions for D2R Tracker
package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
)

// ========== PROFILE MANAGEMENT UTILITIES ==========

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

// ========== TIME AND FORMATTING UTILITIES ==========

func formatDuration(ms int64) string {
	if ms == 0 {
		return "00:00:00"
	}
	d := time.Duration(ms) * time.Millisecond
	return d.Truncate(time.Second).String()
}

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

// ========== PLAYER DATA UTILITIES ==========

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

// ========== ITEM UTILITIES ==========

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

func (a *App) getItemKey(itm data.Item) string {
	return fmt.Sprintf("%s_%v_%d_%d_%d",
		itm.Name, itm.Location.LocationType, itm.Location.Page, itm.Position.X, itm.Position.Y)
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

// ========== ITEM TRACKING UTILITIES ==========

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

// ========== ITEM AFFIX UTILITIES ==========

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