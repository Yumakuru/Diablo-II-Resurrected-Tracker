# Changelog
## [2025-06-15]
  🆕 New File: items.json
      Contains all Unique Items and Set Items in a structured JSON format
      Significantly reduces the size of main.go
  📄 Updated index.html
      New function loadItemData() loads items from the JSON file
      Improved error handling for missing or invalid item data
      Debug functions now also display item data loading status
      Removed large inline item arrays from the HTML
  ⚙️ Updated main.go
      Introduced new data structures for item database management
      New function loadItemDatabase() loads item data at startup
      New API method GetItemLists() (optional for frontend integration)
      Removed over 200 lines of hardcoded item arrays
  🗂️ Externalized Data Mappings (~250 lines saved)
      xp_table.json – XP values for all levels
      item_names.json – Mapping of internal to display item names
      area_names.json – Mapping of area IDs to readable names
  🛠️ Externalized Utility Functions (~150 lines saved)
      New file: utils.go, containing:
      Profile management utilities
      Player data handling
      Item-related utilities
      Formatting helpers
      Item tracking functions
  💡 Benefits
      ✅ Much cleaner and more maintainable code
      ✅ Item lists are now easily extendable without modifying the code
      ✅ Clear separation of data and logic
      ✅ Smaller executable file size
