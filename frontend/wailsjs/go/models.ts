export namespace main {
	
	export class ItemsResponse {
	    items: ItemEntry[];
	    total_items: number;
	    current_page: number;
	    items_per_page: number;
	    total_pages: number;
	    show_all: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ItemsResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], ItemEntry);
	        this.total_items = source["total_items"];
	        this.current_page = source["current_page"];
	        this.items_per_page = source["items_per_page"];
	        this.total_pages = source["total_pages"];
	        this.show_all = source["show_all"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class XPTracking {
	    current_xp: number;
	    current_level: number;
	    xp_to_next_level: number;
	    xp_this_run: number;
	    xp_per_hour: number;
	    runs_to_next_level: number;
	    average_xp_per_run: number;
	    session_xp_gained: number;
	    run_start_xp: number;
	    estimated_runs_to_next: number;
	    runs_calculation_method: string;
	
	    static createFrom(source: any = {}) {
	        return new XPTracking(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current_xp = source["current_xp"];
	        this.current_level = source["current_level"];
	        this.xp_to_next_level = source["xp_to_next_level"];
	        this.xp_this_run = source["xp_this_run"];
	        this.xp_per_hour = source["xp_per_hour"];
	        this.runs_to_next_level = source["runs_to_next_level"];
	        this.average_xp_per_run = source["average_xp_per_run"];
	        this.session_xp_gained = source["session_xp_gained"];
	        this.run_start_xp = source["run_start_xp"];
	        this.estimated_runs_to_next = source["estimated_runs_to_next"];
	        this.runs_calculation_method = source["runs_calculation_method"];
	    }
	}
	export class ItemEntry {
	    name: string;
	    original_name: string;
	    quality: string;
	    run_index: number;
	    // Go type: time
	    time: any;
	    affixes?: string;
	    is_ethereal?: boolean;
	    is_identified?: boolean;
	    item_level?: number;
	    array_index: number;
	
	    static createFrom(source: any = {}) {
	        return new ItemEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.original_name = source["original_name"];
	        this.quality = source["quality"];
	        this.run_index = source["run_index"];
	        this.time = this.convertValues(source["time"], null);
	        this.affixes = source["affixes"];
	        this.is_ethereal = source["is_ethereal"];
	        this.is_identified = source["is_identified"];
	        this.item_level = source["item_level"];
	        this.array_index = source["array_index"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GameStats {
	    normal: number;
	    champion: number;
	    unique: number;
	    superUnique: number;
	    minion: number;
	    total: number;
	    currentRun: string;
	    fastestRun: string;
	    slowestRun: string;
	    averageRun: string;
	    totalRuns: number;
	    runActive: boolean;
	    totalItems: number;
	    recentItems: ItemEntry[];
	    currentProfile: string;
	    profiles: string[];
	    filtersEnabled: boolean;
	    xpTracking: XPTracking;
	    playerLevel: number;
	    playerClass: string;
	    currentArea: string;
	    // Go type: time
	    sessionStartTime: any;
	    itemsData: ItemsResponse;
	
	    static createFrom(source: any = {}) {
	        return new GameStats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.normal = source["normal"];
	        this.champion = source["champion"];
	        this.unique = source["unique"];
	        this.superUnique = source["superUnique"];
	        this.minion = source["minion"];
	        this.total = source["total"];
	        this.currentRun = source["currentRun"];
	        this.fastestRun = source["fastestRun"];
	        this.slowestRun = source["slowestRun"];
	        this.averageRun = source["averageRun"];
	        this.totalRuns = source["totalRuns"];
	        this.runActive = source["runActive"];
	        this.totalItems = source["totalItems"];
	        this.recentItems = this.convertValues(source["recentItems"], ItemEntry);
	        this.currentProfile = source["currentProfile"];
	        this.profiles = source["profiles"];
	        this.filtersEnabled = source["filtersEnabled"];
	        this.xpTracking = this.convertValues(source["xpTracking"], XPTracking);
	        this.playerLevel = source["playerLevel"];
	        this.playerClass = source["playerClass"];
	        this.currentArea = source["currentArea"];
	        this.sessionStartTime = this.convertValues(source["sessionStartTime"], null);
	        this.itemsData = this.convertValues(source["itemsData"], ItemsResponse);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	

}

