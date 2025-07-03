package main

import (
	"container/heap"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type FileSize struct {
	Path string
	Size int64
}

type FileAge struct {
	Path     string
	ModTime  time.Time
	IsCreate bool
}

// FileSizeHeap implements heap.Interface for tracking largest files efficiently
type FileSizeHeap []FileSize

func (h FileSizeHeap) Len() int           { return len(h) }
func (h FileSizeHeap) Less(i, j int) bool { return h[i].Size < h[j].Size } // min-heap
func (h FileSizeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *FileSizeHeap) Push(x interface{}) {
	*h = append(*h, x.(FileSize))
}

func (h *FileSizeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[0 : n-1]
	return item
}

type Stats struct {
	WordFreq         map[string]int
	TypeFreq         map[string]int
	TypeSizes        map[string]int64
	Permissions      map[string]int
	RecentMods       int
	TotalFiles       int
	TotalSize        int64
	LargestFiles     *FileSizeHeap
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

type model struct {
	analyzing bool
	progress  progress.Model
	stats     *Stats
	err       error
	path      string
	done      bool
}

func initialModel(path string) model {
	return model{
		analyzing: true,
		progress:  progress.New(progress.WithDefaultGradient()),
		path:      path,
	}
}

type analysisMsg struct {
	stats *Stats
	err   error
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		analyzeCmd(m.path),
		m.progress.Init(),
	)
}

func analyzeCmd(path string) tea.Cmd {
	return func() tea.Msg {
		stats, err := analyzeDirectory(path)
		return analysisMsg{stats: stats, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "q" || msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case analysisMsg:
		m.analyzing = false
		m.stats = msg.stats
		m.err = msg.err
		m.done = true
		return m, tea.Quit
	case progress.FrameMsg:
		if m.analyzing {
			progressModel, cmd := m.progress.Update(msg)
			m.progress = progressModel.(progress.Model)
			return m, cmd
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	if m.analyzing {
		return fmt.Sprintf("\n%s Analyzing %s...\n\n%s\n\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("🔍"),
			lipgloss.NewStyle().Bold(true).Render(m.path),
			m.progress.View())
	}

	if m.stats == nil {
		return "No data available"
	}

	return displayResults(m.stats)
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))

	// Size colors
	tinyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	smallStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
	mediumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	largeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	// Type colors
	codeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	docStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	mediaStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("165"))
	archiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))

	// Status colors
	goodStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	badStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	pathStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	numberStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	percentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("118"))
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: madaa <path>")
		os.Exit(1)
	}

	path := os.Args[1]
	p := tea.NewProgram(initialModel(path))

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
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
		LargestFiles:     &FileSizeHeap{},
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			processDirectory(path, stats, root)
		} else {
			processFile(path, info, stats)
		}
		return nil
	})

	return stats, err
}

func processDirectory(path string, stats *Stats, root string) {
	stats.TotalDirs++

	relPath, _ := filepath.Rel(root, path)
	depth := strings.Count(relPath, string(os.PathSeparator))
	if relPath != "." {
		stats.DirDepths[path] = depth
	}

	entries, err := os.ReadDir(path)
	if err == nil && len(entries) == 0 {
		stats.EmptyDirs++
	}

	fileCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			fileCount++
		}
	}
	if fileCount > 0 {
		stats.FilesPerDir[path] = fileCount
	}

	if strings.HasPrefix(filepath.Base(path), ".") && path != root {
		stats.HiddenFiles++
	}
}

func processFile(path string, info os.FileInfo, stats *Stats) {
	stats.TotalFiles++
	stats.TotalSize += info.Size()

	filename := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		ext = "no extension"
	}

	words := extractWords(filename)
	for _, word := range words {
		if len(word) > 1 {
			stats.WordFreq[strings.ToLower(word)]++
		}
	}

	stats.TypeFreq[ext]++
	stats.TypeSizes[ext] += info.Size()

	mode := info.Mode()
	if mode&0111 != 0 {
		stats.Permissions["executable"]++
	}
	if mode&0200 == 0 {
		stats.Permissions["read-only"]++
		stats.WriteProtected++
	}

	if time.Since(info.ModTime()) <= 30*24*time.Hour {
		stats.RecentMods++
	}

	analyzeSizes(path, info, stats)
	analyzeAge(path, info, stats)
	analyzeSpecialFiles(path, info, stats)
	analyzeAccessPatterns(info, stats)
}

func analyzeSizes(path string, info os.FileInfo, stats *Stats) {
	size := info.Size()

	// Optimized largest files tracking using heap
	if stats.LargestFiles.Len() < 3 {
		heap.Push(stats.LargestFiles, FileSize{path, size})
	} else if size > (*stats.LargestFiles)[0].Size {
		heap.Pop(stats.LargestFiles) // Remove smallest
		heap.Push(stats.LargestFiles, FileSize{path, size})
	}

	if size == 0 {
		stats.EmptyFiles++
	}

	if size < 1024 {
		stats.SizeDistribution["tiny"]++
	} else if size < 1024*1024 {
		stats.SizeDistribution["small"]++
	} else if size < 100*1024*1024 {
		stats.SizeDistribution["medium"]++
	} else {
		stats.SizeDistribution["large"]++
	}
}

func analyzeAge(path string, info os.FileInfo, stats *Stats) {
	modTime := info.ModTime()

	if stats.OldestFile == nil || modTime.Before(stats.OldestFile.ModTime) {
		stats.OldestFile = &FileAge{path, modTime, false}
	}
	if stats.NewestFile == nil || modTime.After(stats.NewestFile.ModTime) {
		stats.NewestFile = &FileAge{path, modTime, false}
	}

	year := modTime.Year()
	stats.YearDistribution[year]++

	if time.Since(modTime) > 6*30*24*time.Hour {
		stats.StaleFiles++
	}
}

func analyzeSpecialFiles(path string, info os.FileInfo, stats *Stats) {
	filename := filepath.Base(path)

	if strings.HasPrefix(filename, ".") {
		stats.HiddenFiles++
	}

	if info.Mode()&os.ModeSymlink != 0 {
		stats.Symlinks++
	}

	if isSystemFile(filename) {
		stats.SystemFiles++
	}
}

func analyzeAccessPatterns(info os.FileInfo, stats *Stats) {
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

func extractWords(filename string) []string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	replacer := strings.NewReplacer(",", " ", "_", " ", "-", " ", ".", " ")
	name = replacer.Replace(name)
	return strings.Fields(name)
}

func getFileTypeStyle(ext string) lipgloss.Style {
	codeExts := []string{".go", ".py", ".js", ".html", ".css", ".json", ".xml", ".sql", ".sh", ".bat"}
	docExts := []string{".txt", ".md", ".pdf", ".doc", ".docx", ".rtf", ".odt"}
	mediaExts := []string{".jpg", ".jpeg", ".png", ".gif", ".mp3", ".mp4", ".wav", ".avi", ".mov"}
	archiveExts := []string{".zip", ".tar", ".gz", ".7z", ".rar", ".bz2"}

	for _, e := range codeExts {
		if e == ext {
			return codeStyle
		}
	}
	for _, e := range docExts {
		if e == ext {
			return docStyle
		}
	}
	for _, e := range mediaExts {
		if e == ext {
			return mediaStyle
		}
	}
	for _, e := range archiveExts {
		if e == ext {
			return archiveStyle
		}
	}
	return lipgloss.NewStyle()
}

func getSizeStyle(size int64) lipgloss.Style {
	if size < 1024 {
		return tinyStyle
	} else if size < 1024*1024 {
		return smallStyle
	} else if size < 100*1024*1024 {
		return mediumStyle
	} else {
		return largeStyle
	}
}

func displayResults(stats *Stats) string {
	var result strings.Builder

	// Title
	result.WriteString(titleStyle.Render("MADAA - Mass Data Analysis Results"))
	result.WriteString("\n\n")

	// Overview
	result.WriteString(headerStyle.Render("Overview"))
	result.WriteString("\n")
	result.WriteString(fmt.Sprintf("Files: %s  Directories: %s  Size: %s MB\n\n",
		numberStyle.Render(fmt.Sprintf("%d", stats.TotalFiles)),
		numberStyle.Render(fmt.Sprintf("%d", stats.TotalDirs)),
		numberStyle.Render(fmt.Sprintf("%.1f", float64(stats.TotalSize)/(1024*1024)))))

	// File types
	result.WriteString(headerStyle.Render("File Types"))
	result.WriteString("\n")
	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	for k, v := range stats.TypeFreq {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Value > sorted[j].Value
	})
	for i, item := range sorted {
		if i >= 5 {
			break
		}
		percentage := float64(item.Value) / float64(stats.TotalFiles) * 100
		style := getFileTypeStyle(item.Key)
		result.WriteString(fmt.Sprintf("%s %s %s\n",
			style.Render(fmt.Sprintf("%-12s", item.Key)),
			numberStyle.Render(fmt.Sprintf("%6d", item.Value)),
			percentStyle.Render(fmt.Sprintf("(%5.1f%%)", percentage))))
	}
	result.WriteString("\n")

	// Largest files - optimized display
	result.WriteString(headerStyle.Render("Largest Files"))
	result.WriteString("\n")

	// Convert heap to sorted slice for display
	largestFiles := make([]FileSize, stats.LargestFiles.Len())
	copy(largestFiles, *stats.LargestFiles)
	sort.Slice(largestFiles, func(i, j int) bool {
		return largestFiles[i].Size > largestFiles[j].Size
	})

	for _, file := range largestFiles {
		sizeMB := float64(file.Size) / (1024 * 1024)
		style := getSizeStyle(file.Size)
		result.WriteString(fmt.Sprintf("%s %s\n",
			style.Render(fmt.Sprintf("%8.1f MB", sizeMB)),
			pathStyle.Render(file.Path)))
	}
	result.WriteString("\n")

	// Size distribution
	result.WriteString(headerStyle.Render("Size Distribution"))
	result.WriteString("\n")
	categories := []struct {
		name  string
		style lipgloss.Style
	}{
		{"tiny (<1KB)", tinyStyle},
		{"small (<1MB)", smallStyle},
		{"medium (<100MB)", mediumStyle},
		{"large (>100MB)", largeStyle},
	}
	for _, cat := range categories {
		if count, ok := stats.SizeDistribution[strings.Split(cat.name, " ")[0]]; ok {
			percentage := float64(count) / float64(stats.TotalFiles) * 100
			result.WriteString(fmt.Sprintf("%s %s %s\n",
				cat.style.Render(fmt.Sprintf("%-16s", cat.name)),
				numberStyle.Render(fmt.Sprintf("%6d", count)),
				percentStyle.Render(fmt.Sprintf("(%5.1f%%)", percentage))))
		}
	}
	result.WriteString("\n")

	// Age analysis
	result.WriteString(headerStyle.Render("Age Analysis"))
	result.WriteString("\n")
	if stats.OldestFile != nil {
		result.WriteString(fmt.Sprintf("Oldest: %s %s\n",
			pathStyle.Render(stats.OldestFile.Path),
			goodStyle.Render(stats.OldestFile.ModTime.Format("2006-01-02"))))
	}
	if stats.NewestFile != nil {
		result.WriteString(fmt.Sprintf("Newest: %s %s\n",
			pathStyle.Render(stats.NewestFile.Path),
			goodStyle.Render(stats.NewestFile.ModTime.Format("2006-01-02"))))
	}
	stalePercent := float64(stats.StaleFiles) / float64(stats.TotalFiles) * 100
	staleStyle := goodStyle
	if stalePercent > 50 {
		staleStyle = warnStyle
	}
	if stalePercent > 80 {
		staleStyle = badStyle
	}
	result.WriteString(fmt.Sprintf("Stale (>6mo): %s %s\n",
		numberStyle.Render(fmt.Sprintf("%d", stats.StaleFiles)),
		staleStyle.Render(fmt.Sprintf("(%.1f%%)", stalePercent))))
	result.WriteString("\n")

	// Special files
	result.WriteString(headerStyle.Render("Special Files"))
	result.WriteString("\n")
	result.WriteString(fmt.Sprintf("Hidden: %s  System: %s  Symlinks: %s  Write-protected: %s\n",
		numberStyle.Render(fmt.Sprintf("%d", stats.HiddenFiles)),
		numberStyle.Render(fmt.Sprintf("%d", stats.SystemFiles)),
		numberStyle.Render(fmt.Sprintf("%d", stats.Symlinks)),
		warnStyle.Render(fmt.Sprintf("%d", stats.WriteProtected))))
	result.WriteString("\n")

	// Directory info
	result.WriteString(headerStyle.Render("Directory Info"))
	result.WriteString("\n")
	result.WriteString(fmt.Sprintf("Empty dirs: %s  Recent changes: %s %s\n",
		numberStyle.Render(fmt.Sprintf("%d", stats.EmptyDirs)),
		numberStyle.Render(fmt.Sprintf("%d", stats.RecentMods)),
		percentStyle.Render(fmt.Sprintf("(%.1f%%)", float64(stats.RecentMods)/float64(stats.TotalFiles)*100))))

	result.WriteString("\nPress 'q' to quit")

	return result.String()
}
