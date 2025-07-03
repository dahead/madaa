package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

type FileInfo struct {
	Path    string
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
	Ext     string
}

type Stats struct {
	WordFreq         map[string]int
	TypeFreq         map[string]int
	TypeSizes        map[string]int64
	Permissions      map[string]int
	RecentMods       int
	TotalFiles       int
	TotalSize        int64
	LargestFiles     []FileSize
	EmptyFiles       int
	SizeDistribution map[string]int
	DirDepths        map[string]int
	FilesPerDir      map[string]int
	EmptyDirs        int
	OldestFile       *FileAge
	NewestFile       *FileAge
	YearDistribution map[int]int
	StaleFiles       int
	HiddenFiles      int
	SystemFiles      int
	Symlinks         int
	AccessTimes      map[string]int
	WriteProtected   int
	TotalDirs        int
}

type FileSize struct {
	Path string
	Size int64
}

type FileAge struct {
	Path     string
	ModTime  time.Time
	IsCreate bool
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: madaa <path>")
		os.Exit(1)
	}

	path := os.Args[1]
	stats, err := analyzeDirectory(path)
	if err != nil {
		fmt.Printf("Error analyzing directory: %v\n", err)
		os.Exit(1)
	}

	displayResults(stats)
}

func analyzeDirectory(root string) (*Stats, error) {
	stats := &Stats{
		WordFreq:         make(map[string]int),
		TypeFreq:         make(map[string]int),
		TypeSizes:        make(map[string]int64),
		Permissions:      make(map[string]int),
		SizeDistribution: make(map[string]int),
		DirDepths:        make(map[string]int),
		FilesPerDir:      make(map[string]int),
		YearDistribution: make(map[int]int),
		AccessTimes:      make(map[string]int),
		LargestFiles:     make([]FileSize, 0),
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			processDirectory(path, info, stats, root)
		} else {
			processFile(path, info, stats)
		}
		return nil
	})

	calculateDirAverages(stats)
	return stats, err
}

func processDirectory(path string, info os.FileInfo, stats *Stats, root string) {
	stats.TotalDirs++

	// Directory depth analysis
	relPath, _ := filepath.Rel(root, path)
	depth := strings.Count(relPath, string(os.PathSeparator))
	if relPath != "." {
		stats.DirDepths[path] = depth
	}

	// Check for empty directories
	entries, err := os.ReadDir(path)
	if err == nil && len(entries) == 0 {
		stats.EmptyDirs++
	}

	// Count files per directory
	fileCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			fileCount++
		}
	}
	if fileCount > 0 {
		stats.FilesPerDir[path] = fileCount
	}

	// Hidden directories
	if strings.HasPrefix(filepath.Base(path), ".") && path != root {
		stats.HiddenFiles++
	}
}

func processFile(path string, info os.FileInfo, stats *Stats) {
	stats.TotalFiles++
	stats.TotalSize += info.Size()

	// Extract filename and extension
	filename := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		ext = "no extension"
	}

	// Word frequency analysis
	words := extractWords(filename)
	for _, word := range words {
		if len(word) > 1 {
			stats.WordFreq[strings.ToLower(word)]++
		}
	}

	// File type analysis
	stats.TypeFreq[ext]++
	stats.TypeSizes[ext] += info.Size()

	// Permission analysis
	mode := info.Mode()
	if mode&0111 != 0 {
		stats.Permissions["executable"]++
	}
	if mode&0200 == 0 {
		stats.Permissions["read-only"]++
		stats.WriteProtected++
	}

	// Recent modifications (last 30 days)
	if time.Since(info.ModTime()) <= 30*24*time.Hour {
		stats.RecentMods++
	}

	// Size-related analysis
	analyzeSizes(path, info, stats)

	// Age analysis
	analyzeAge(path, info, stats)

	// Special files
	analyzeSpecialFiles(path, info, stats)

	// Access patterns
	analyzeAccessPatterns(path, info, stats)
}

func analyzeSizes(path string, info os.FileInfo, stats *Stats) {
	size := info.Size()

	// Largest files tracking
	stats.LargestFiles = append(stats.LargestFiles, FileSize{path, size})
	sort.Slice(stats.LargestFiles, func(i, j int) bool {
		return stats.LargestFiles[i].Size > stats.LargestFiles[j].Size
	})
	if len(stats.LargestFiles) > 3 {
		stats.LargestFiles = stats.LargestFiles[:3]
	}

	// Empty files
	if size == 0 {
		stats.EmptyFiles++
	}

	// Size distribution
	if size < 1024 {
		stats.SizeDistribution["tiny (<1KB)"]++
	} else if size < 1024*1024 {
		stats.SizeDistribution["small (<1MB)"]++
	} else if size < 100*1024*1024 {
		stats.SizeDistribution["medium (<100MB)"]++
	} else {
		stats.SizeDistribution["large (>100MB)"]++
	}
}

func analyzeAge(path string, info os.FileInfo, stats *Stats) {
	modTime := info.ModTime()

	// Track oldest and newest files
	if stats.OldestFile == nil || modTime.Before(stats.OldestFile.ModTime) {
		stats.OldestFile = &FileAge{path, modTime, false}
	}
	if stats.NewestFile == nil || modTime.After(stats.NewestFile.ModTime) {
		stats.NewestFile = &FileAge{path, modTime, false}
	}

	// Year distribution
	year := modTime.Year()
	stats.YearDistribution[year]++

	// Stale files (not modified in 6+ months)
	if time.Since(modTime) > 6*30*24*time.Hour {
		stats.StaleFiles++
	}
}

func analyzeSpecialFiles(path string, info os.FileInfo, stats *Stats) {
	filename := filepath.Base(path)

	// Hidden files
	if strings.HasPrefix(filename, ".") {
		stats.HiddenFiles++
	}

	// Symlinks
	if info.Mode()&os.ModeSymlink != 0 {
		stats.Symlinks++
	}

	// System files (basic detection)
	if isSystemFile(filename) {
		stats.SystemFiles++
	}
}

func analyzeAccessPatterns(path string, info os.FileInfo, stats *Stats) {
	// Get access time if available (Unix systems)
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		accessTime := time.Unix(stat.Atim.Sec, stat.Atim.Nsec)
		daysSinceAccess := int(time.Since(accessTime).Hours() / 24)

		if daysSinceAccess <= 7 {
			stats.AccessTimes["last 7 days"]++
		} else if daysSinceAccess <= 30 {
			stats.AccessTimes["last 30 days"]++
		} else if daysSinceAccess <= 90 {
			stats.AccessTimes["last 90 days"]++
		} else {
			stats.AccessTimes["older than 90 days"]++
		}
	}
}

func isSystemFile(filename string) bool {
	systemFiles := []string{
		"thumbs.db", "desktop.ini", ".ds_store",
		"hiberfil.sys", "pagefile.sys", "swapfile.sys",
		"system volume information", "recycler", "$recycle.bin",
	}

	lower := strings.ToLower(filename)
	for _, sys := range systemFiles {
		if lower == sys {
			return true
		}
	}
	return false
}

func calculateDirAverages(stats *Stats) {
	// This function could calculate additional directory statistics if needed
}

func extractWords(filename string) []string {
	// Remove extension for word extraction
	name := strings.TrimSuffix(filename, filepath.Ext(filename))

	// Split on common delimiters
	replacer := strings.NewReplacer(",", " ", "_", " ", "-", " ", ".", " ")
	name = replacer.Replace(name)

	return strings.Fields(name)
}

func displayResults(stats *Stats) {
	fmt.Printf("=== MADAA Analysis Results ===\n")
	fmt.Printf("Total files: %d\n", stats.TotalFiles)
	fmt.Printf("Total directories: %d\n", stats.TotalDirs)
	fmt.Printf("Total size: %.2f MB\n\n", float64(stats.TotalSize)/(1024*1024))

	displayTop3("Most Frequent Words in Filenames", stats.WordFreq)
	displayTop3FileTypes("Most Frequent File Types", stats.TypeFreq, stats.TotalFiles)
	displayTop3FileSizes("File Types by Size", stats.TypeSizes, stats.TotalSize)
	displayPermissions(stats.Permissions, stats.TotalFiles)
	displayTimeAnalysis(stats.RecentMods, stats.TotalFiles)

	// New displays
	displayLargestFiles(stats.LargestFiles)
	displaySizeDistribution(stats.SizeDistribution, stats.TotalFiles)
	displayDirectoryAnalysis(stats)
	displayAgeAnalysis(stats)
	displaySpecialFiles(stats)
	displayAccessPatterns(stats.AccessTimes, stats.TotalFiles)
}

func displayTop3(title string, data map[string]int) {
	fmt.Printf("=== %s ===\n", title)

	type kv struct {
		Key   string
		Value int
	}

	var sorted []kv
	for k, v := range data {
		sorted = append(sorted, kv{k, v})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	for i, item := range sorted {
		if i >= 3 {
			break
		}
		fmt.Printf("%d. %s: %d occurrences\n", i+1, item.Key, item.Value)
	}
	fmt.Println()
}

func displayTop3FileTypes(title string, data map[string]int, total int) {
	fmt.Printf("=== %s ===\n", title)

	type kv struct {
		Key   string
		Value int
	}

	var sorted []kv
	for k, v := range data {
		sorted = append(sorted, kv{k, v})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	for i, item := range sorted {
		if i >= 3 {
			break
		}
		percentage := float64(item.Value) / float64(total) * 100
		fmt.Printf("%d. %s: %d files (%.1f%%)\n", i+1, item.Key, item.Value, percentage)
	}
	fmt.Println()
}

func displayTop3FileSizes(title string, data map[string]int64, totalSize int64) {
	fmt.Printf("=== %s ===\n", title)

	type kv struct {
		Key   string
		Value int64
	}

	var sorted []kv
	for k, v := range data {
		sorted = append(sorted, kv{k, v})
	}

	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})

	for i, item := range sorted {
		if i >= 3 {
			break
		}
		percentage := float64(item.Value) / float64(totalSize) * 100
		sizeMB := float64(item.Value) / (1024 * 1024)
		fmt.Printf("%d. %s: %.2f MB (%.1f%%)\n", i+1, item.Key, sizeMB, percentage)
	}
	fmt.Println()
}

func displayPermissions(perms map[string]int, total int) {
	fmt.Printf("=== File Permissions ===\n")
	if exec, ok := perms["executable"]; ok {
		fmt.Printf("Executable files: %d of %d (%.1f%%)\n", exec, total, float64(exec)/float64(total)*100)
	}
	if ro, ok := perms["read-only"]; ok {
		fmt.Printf("Read-only files: %d of %d (%.1f%%)\n", ro, total, float64(ro)/float64(total)*100)
	}
	fmt.Println()
}

func displayTimeAnalysis(recentMods, total int) {
	fmt.Printf("=== Recent Modifications ===\n")
	fmt.Printf("Files modified in last 30 days: %d of %d (%.1f%%)\n", recentMods, total, float64(recentMods)/float64(total)*100)
	fmt.Println()
}

func displayLargestFiles(largest []FileSize) {
	fmt.Printf("=== Largest Files ===\n")
	for i, file := range largest {
		sizeMB := float64(file.Size) / (1024 * 1024)
		fmt.Printf("%d. %s: %.2f MB\n", i+1, file.Path, sizeMB)
	}
	fmt.Println()
}

func displaySizeDistribution(dist map[string]int, total int) {
	fmt.Printf("=== Size Distribution ===\n")
	categories := []string{"tiny (<1KB)", "small (<1MB)", "medium (<100MB)", "large (>100MB)"}
	for _, cat := range categories {
		if count, ok := dist[cat]; ok {
			percentage := float64(count) / float64(total) * 100
			fmt.Printf("%s: %d files (%.1f%%)\n", cat, count, percentage)
		}
	}
	fmt.Printf("Empty files: %d\n", dist["empty"])
	fmt.Println()
}

func displayDirectoryAnalysis(stats *Stats) {
	fmt.Printf("=== Directory Analysis ===\n")
	fmt.Printf("Empty directories: %d\n", stats.EmptyDirs)

	// Find deepest directories
	type depthInfo struct {
		Path  string
		Depth int
	}
	var depths []depthInfo
	for path, depth := range stats.DirDepths {
		depths = append(depths, depthInfo{path, depth})
	}
	sort.Slice(depths, func(i, j int) bool {
		return depths[i].Depth > depths[j].Depth
	})

	fmt.Printf("Deepest directories (top 3):\n")
	for i, d := range depths {
		if i >= 3 {
			break
		}
		fmt.Printf("%d. %s (depth: %d)\n", i+1, d.Path, d.Depth)
	}

	// Average files per directory
	totalFiles := 0
	for _, count := range stats.FilesPerDir {
		totalFiles += count
	}
	if len(stats.FilesPerDir) > 0 {
		avg := float64(totalFiles) / float64(len(stats.FilesPerDir))
		fmt.Printf("Average files per directory: %.1f\n", avg)
	}
	fmt.Println()
}

func displayAgeAnalysis(stats *Stats) {
	fmt.Printf("=== Age Analysis ===\n")
	if stats.OldestFile != nil {
		fmt.Printf("Oldest file: %s (%s)\n", stats.OldestFile.Path, stats.OldestFile.ModTime.Format("2006-01-02"))
	}
	if stats.NewestFile != nil {
		fmt.Printf("Newest file: %s (%s)\n", stats.NewestFile.Path, stats.NewestFile.ModTime.Format("2006-01-02"))
	}
	fmt.Printf("Stale files (>6 months): %d (%.1f%%)\n", stats.StaleFiles, float64(stats.StaleFiles)/float64(stats.TotalFiles)*100)

	// Year distribution (top 3)
	type yearInfo struct {
		Year  int
		Count int
	}
	var years []yearInfo
	for year, count := range stats.YearDistribution {
		years = append(years, yearInfo{year, count})
	}
	sort.Slice(years, func(i, j int) bool {
		return years[i].Count > years[j].Count
	})

	fmt.Printf("Most active years (top 3):\n")
	for i, y := range years {
		if i >= 3 {
			break
		}
		fmt.Printf("%d. %d: %d files\n", i+1, y.Year, y.Count)
	}
	fmt.Println()
}

func displaySpecialFiles(stats *Stats) {
	fmt.Printf("=== Special Files ===\n")
	fmt.Printf("Hidden files: %d\n", stats.HiddenFiles)
	fmt.Printf("System files: %d\n", stats.SystemFiles)
	fmt.Printf("Symlinks: %d\n", stats.Symlinks)
	fmt.Printf("Write-protected files: %d\n", stats.WriteProtected)
	fmt.Println()
}

func displayAccessPatterns(access map[string]int, total int) {
	fmt.Printf("=== Access Patterns ===\n")
	periods := []string{"last 7 days", "last 30 days", "last 90 days", "older than 90 days"}
	for _, period := range periods {
		if count, ok := access[period]; ok {
			percentage := float64(count) / float64(total) * 100
			fmt.Printf("Files accessed %s: %d (%.1f%%)\n", period, count, percentage)
		}
	}
	fmt.Println()
}
