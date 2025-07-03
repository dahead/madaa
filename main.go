// madaa = mass data analyzer
// CLI application which analyzes a given path (parameter 0)
// for certain things.

// most frequent words in filenames found
// - filenames get split into an array, separated by comma or space.
// most frequent file types
// - example: 95% of all files are of type mp3
// percentage of file types and their sizes in the given path.
// permissions (rights, owners) of file types
// - example: xxx files from yyy files are executable
//            xxx files are read-only
// datetime change and modification by frequency
// - example: 500 from 3233 files were modified in the last 30 days.
//            233 files were modified in the last 30 days.

// only display top 3 "types" of each category

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	WordFreq    map[string]int
	TypeFreq    map[string]int
	TypeSizes   map[string]int64
	Permissions map[string]int
	RecentMods  int
	TotalFiles  int
	TotalSize   int64
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
		WordFreq:    make(map[string]int),
		TypeFreq:    make(map[string]int),
		TypeSizes:   make(map[string]int64),
		Permissions: make(map[string]int),
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			processFile(path, info, stats)
		}
		return nil
	})

	return stats, err
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
	}

	// Recent modifications (last 30 days)
	if time.Since(info.ModTime()) <= 30*24*time.Hour {
		stats.RecentMods++
	}
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
	fmt.Printf("Total size: %.2f MB\n\n", float64(stats.TotalSize)/(1024*1024))

	displayTop3("Most Frequent Words in Filenames", stats.WordFreq)
	displayTop3FileTypes("Most Frequent File Types", stats.TypeFreq, stats.TotalFiles)
	displayTop3FileSizes("File Types by Size", stats.TypeSizes, stats.TotalSize)
	displayPermissions(stats.Permissions, stats.TotalFiles)
	displayTimeAnalysis(stats.RecentMods, stats.TotalFiles)
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
