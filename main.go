package main

import (
	"container/heap"
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/sync/errgroup"
	"gopkg.in/ini.v1"
)

const defaultConfigContent = `[file_types]
# Application files
.exe=app
.app=app
.apk=app
.ipa=app

# Code files
.go=code
.mod=code
.py=code
.js=code
.html=code
.css=code
.json=code
.xml=code
.sql=code
.sh=code
.bat=code
.c=code
.cpp=code
.java=code
.php=code
.rb=code
.rs=code
.swift=code

# Document files
.txt=doc
.md=doc
.pdf=doc
.rtf=doc
.odt=doc
.md=doc
.json=doc
.ini=doc
.conf=doc
.log=doc
.csv=doc
.doc=doc
.docx=doc
.xls=doc
.xlsx=doc
.ppt=doc
.pptx=doc
.epub=doc

# Media files
.jpg=media
.jpeg=media
.png=media
.gif=media
.mp3=media
.mp4=media
.wav=media
.avi=media
.mov=media
.webm=media
.flv=media
.mkv=media
.ogg=media
.wmv=media
.mpg=media
.mpeg=media
.m4v=media
.m4a=media
.m4p=media

# Archive files
.zip=archive
.tar=archive
.gz=archive
.7z=archive
.rar=archive
.bz2=archive
`

type FileSize struct {
	Path string
	Size int64
	Type string
}

type FileAge struct {
	Path     string
	ModTime  time.Time
	IsCreate bool
}

type FileSizeHeap []FileSize

func (h FileSizeHeap) Len() int           { return len(h) }
func (h FileSizeHeap) Less(i, j int) bool { return h[i].Size < h[j].Size }
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
	LargestByType    map[string]*FileSizeHeap
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
	mu               sync.RWMutex
}

type Config struct {
	Count int
	Path  string
}

type model struct {
	analyzing      bool
	progress       progress.Model
	stats          *Stats
	err            error
	config         Config
	done           bool
	processedFiles int
	totalFiles     int
	progressChan   chan progressMsg
}

func initialModel(config Config) model {
	return model{
		analyzing:    true,
		progress:     progress.New(progress.WithDefaultGradient()),
		config:       config,
		progressChan: make(chan progressMsg, 100),
	}
}

type analysisMsg struct {
	stats *Stats
	err   error
}

type progressMsg struct {
	processed int
	total     int
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		analyzeCmd(m.config, m.progressChan),
		m.progress.Init(),
		listenForProgress(m.progressChan),
	)
}

func analyzeCmd(config Config, progressChan chan progressMsg) tea.Cmd {
	return func() tea.Msg {
		stats, err := analyzeDirectory(config.Path, config.Count, progressChan)
		return analysisMsg{stats: stats, err: err}
	}
}

func listenForProgress(progressChan chan progressMsg) tea.Cmd {
	return func() tea.Msg {
		return <-progressChan
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
	case progressMsg:
		m.processedFiles = msg.processed
		m.totalFiles = msg.total
		if m.totalFiles > 0 {
			percent := float64(m.processedFiles) / float64(m.totalFiles)
			cmd := m.progress.SetPercent(percent)
			return m, tea.Batch(cmd, listenForProgress(m.progressChan))
		}
		return m, listenForProgress(m.progressChan)
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
		var progressInfo string
		if m.totalFiles > 0 {
			progressInfo = fmt.Sprintf(" (%d/%d files)", m.processedFiles, m.totalFiles)
		}

		return fmt.Sprintf("\n%s Analyzing %s%s...\n\n%s\n\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("🔍"),
			lipgloss.NewStyle().Bold(true).Render(m.config.Path),
			progressInfo,
			m.progress.View())
	}

	if m.stats == nil {
		return "No data available"
	}

	return displayResults(m.stats, m.config.Count)
}

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))

	tinyStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	smallStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("34"))
	mediumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	largeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	appStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	codeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	docStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))
	mediaStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("165"))
	archiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))

	goodStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	warnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
	badStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	pathStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	numberStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	percentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("118"))

	// Global config maps
	fileTypeStyleMap map[string]lipgloss.Style
	systemFilesMap   map[string]bool
)

func loadConfig() error {
	// Create default config if it doesn't exist
	configPath := "config.ini"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := createDefaultConfig(configPath); err != nil {
			return err
		}
	}

	cfg, err := ini.Load(configPath)
	if err != nil {
		return err
	}

	// Initialize maps
	fileTypeStyleMap = make(map[string]lipgloss.Style)
	systemFilesMap = make(map[string]bool)

	// Load file types
	fileTypesSection := cfg.Section("file_types")
	for _, key := range fileTypesSection.Keys() {
		ext := key.Name()
		category := key.Value()

		switch category {
		case "app":
			fileTypeStyleMap[ext] = appStyle
		case "code":
			fileTypeStyleMap[ext] = codeStyle
		case "doc":
			fileTypeStyleMap[ext] = docStyle
		case "media":
			fileTypeStyleMap[ext] = mediaStyle
		case "archive":
			fileTypeStyleMap[ext] = archiveStyle
		}
	}

	return nil
}

func createDefaultConfig(path string) error {
	return os.WriteFile(path, []byte(defaultConfigContent), 0644)
}

func main() {
	var count int
	flag.IntVar(&count, "count", 3, "Number of top files to show")
	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Println("Usage: madaa [--count N] <path>")
		os.Exit(1)
	}

	// Load configuration
	if err := loadConfig(); err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	config := Config{
		Count: count,
		Path:  flag.Arg(0),
	}

	p := tea.NewProgram(initialModel(config))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}

func analyzeDirectory(root string, maxFiles int, progressChan chan progressMsg) (*Stats, error) {
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
		LargestByType:    make(map[string]*FileSizeHeap),
	}

	heap.Init(stats.LargestFiles)

	// First pass: count total files for progress tracking
	var totalFiles int64
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			atomic.AddInt64(&totalFiles, 1)
		}
		return nil
	})

	// Use concurrent processing
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)
	pathChan := make(chan string, 100)
	numWorkers := runtime.NumCPU()

	// Counter for processed files
	var processedFiles int64

	// Progress ticker
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processed := atomic.LoadInt64(&processedFiles)
				total := atomic.LoadInt64(&totalFiles)
				if total > 0 {
					select {
					case progressChan <- progressMsg{processed: int(processed), total: int(total)}:
					default:
					}
				}
			}
		}
	}()

	// Start workers
	for i := 0; i < numWorkers; i++ {
		g.Go(func() error {
			return processWorker(ctx, pathChan, stats, maxFiles, root, &processedFiles)
		})
	}

	// Walk directory and send paths to workers
	g.Go(func() error {
		defer close(pathChan)
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case pathChan <- path:
				return nil
			}
		})
	})

	err := g.Wait()

	// Send final progress
	select {
	case progressChan <- progressMsg{processed: int(totalFiles), total: int(totalFiles)}:
	default:
	}

	return stats, err
}

func processWorker(ctx context.Context, pathChan <-chan string, stats *Stats, maxFiles int, root string, processedFiles *int64) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case path, ok := <-pathChan:
			if !ok {
				return nil
			}

			info, err := os.Lstat(path)
			if err != nil {
				continue
			}

			if info.IsDir() {
				processDirectory(path, stats, root)
			} else {
				processFile(path, info, stats, maxFiles)
				atomic.AddInt64(processedFiles, 1)
			}
		}
	}
}

func processDirectory(path string, stats *Stats, root string) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	stats.TotalDirs++

	relPath, _ := filepath.Rel(root, path)
	depth := strings.Count(relPath, string(os.PathSeparator))
	if relPath != "." {
		stats.DirDepths[path] = depth
	}

	if strings.HasPrefix(filepath.Base(path), ".") && path != root {
		stats.HiddenFiles++
	}

	entries, err := os.ReadDir(path)
	if err == nil {
		if len(entries) == 0 {
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
	}
}

func processFile(path string, info os.FileInfo, stats *Stats, maxFiles int) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

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

	if stats.LargestFiles.Len() < maxFiles {
		heap.Push(stats.LargestFiles, FileSize{path, info.Size(), ext})
	} else if info.Size() > (*stats.LargestFiles)[0].Size {
		heap.Pop(stats.LargestFiles)
		heap.Push(stats.LargestFiles, FileSize{path, info.Size(), ext})
	}

	if stats.LargestByType[ext] == nil {
		stats.LargestByType[ext] = &FileSizeHeap{}
		heap.Init(stats.LargestByType[ext])
	}

	typeHeap := stats.LargestByType[ext]
	topFilesPerType := min(maxFiles, 10) // Limit per-type files
	if typeHeap.Len() < topFilesPerType {
		heap.Push(typeHeap, FileSize{path, info.Size(), ext})
	} else if info.Size() > (*typeHeap)[0].Size {
		heap.Pop(typeHeap)
		heap.Push(typeHeap, FileSize{path, info.Size(), ext})
	}

	// Use separate function for permissions
	processFilePermissions(info, stats)

	if time.Since(info.ModTime()) <= 30*24*time.Hour {
		stats.RecentMods++
	}

	analyzeSizes(info, stats)
	analyzeAge(path, info, stats)
	analyzeSpecialFiles(path, info, stats)
	analyzeAccessPatterns(info, stats)
}

func processFilePermissions(info os.FileInfo, stats *Stats) {
	mode := info.Mode()
	if mode&0111 != 0 {
		stats.Permissions["executable"]++
	}
	if mode&0200 == 0 {
		stats.Permissions["read-only"]++
		stats.WriteProtected++
	}
}

func analyzeSizes(info os.FileInfo, stats *Stats) {
	size := info.Size()

	if size == 0 {
		stats.EmptyFiles++
	}

	switch {
	case size < 1024:
		stats.SizeDistribution["tiny"]++
	case size < 1024*1024:
		stats.SizeDistribution["small"]++
	case size < 100*1024*1024:
		stats.SizeDistribution["medium"]++
	default:
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

}

func analyzeAccessPatterns(info os.FileInfo, stats *Stats) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		accessTime := time.Unix(stat.Atim.Sec, stat.Atim.Nsec)
		daysSinceAccess := int(time.Since(accessTime).Hours() / 24)

		switch {
		case daysSinceAccess <= 7:
			stats.AccessTimes["last 7 days"]++
		case daysSinceAccess <= 30:
			stats.AccessTimes["last 30 days"]++
		case daysSinceAccess <= 90:
			stats.AccessTimes["last 90 days"]++
		default:
			stats.AccessTimes["older than 90 days"]++
		}
	}
}

func extractWords(filename string) []string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	replacer := strings.NewReplacer(",", " ", "_", " ", "-", " ", ".", " ")
	name = replacer.Replace(name)
	return strings.Fields(name)
}

func getFileTypeStyle(ext string) lipgloss.Style {
	if style, ok := fileTypeStyleMap[ext]; ok {
		return style
	}
	return lipgloss.NewStyle()
}

func getSizeStyle(size int64) lipgloss.Style {
	switch {
	case size < 1024:
		return tinyStyle
	case size < 1024*1024:
		return smallStyle
	case size < 100*1024*1024:
		return mediumStyle
	default:
		return largeStyle
	}
}

func displayResults(stats *Stats, maxCount int) string {
	var result strings.Builder

	result.WriteString(titleStyle.Render("MADAA - Mass Data Analysis Results"))
	result.WriteString("\n\n")

	// Overview section
	result.WriteString(headerStyle.Render("Overview"))
	result.WriteString("\n")
	result.WriteString(fmt.Sprintf("Files: %s  Directories: %s  Size: %s MB\n\n",
		numberStyle.Render(fmt.Sprintf("%d", stats.TotalFiles)),
		numberStyle.Render(fmt.Sprintf("%d", stats.TotalDirs)),
		numberStyle.Render(fmt.Sprintf("%.1f", float64(stats.TotalSize)/(1024*1024)))))

	// File Categories section
	result.WriteString(headerStyle.Render("File Categories"))
	result.WriteString("\n")

	// Collect category statistics
	categories := make(map[string]int)

	// Zähle Dateien pro Kategorie
	for ext, count := range stats.TypeFreq {
		if _, exists := fileTypeStyleMap[ext]; exists {
			category := ""

			// Extrahiere die Kategorie aus dem Mapping
			for configExt, configStyle := range fileTypeStyleMap {
				if configExt == ext {
					if strings.Contains(configStyle.String(), "208") {
						category = "App"
					} else if strings.Contains(configStyle.String(), "82") {
						category = "Code"
					} else if strings.Contains(configStyle.String(), "33") {
						category = "Document"
					} else if strings.Contains(configStyle.String(), "165") {
						category = "Media"
					} else if strings.Contains(configStyle.String(), "208") {
						category = "Archive"
					}
					break
				}
			}

			if category != "" {
				categories[category] += count
			}
		}
	}

	// Sort categories by count
	type categoryStat struct {
		name  string
		count int
	}
	var sortedCategories []categoryStat
	for cat, count := range categories {
		sortedCategories = append(sortedCategories, categoryStat{
			name:  cat,
			count: count,
		})
	}
	sort.Slice(sortedCategories, func(i, j int) bool {
		return sortedCategories[i].count > sortedCategories[j].count
	})

	// Display categories
	for _, cat := range sortedCategories {
		if cat.count == 0 {
			continue
		}
		percentage := float64(cat.count) / float64(stats.TotalFiles) * 100
		result.WriteString(fmt.Sprintf("%s %s %s\n",
			getFileTypeStyle(strings.ToLower(cat.name)).Render(fmt.Sprintf("%-12s", cat.name)),
			numberStyle.Render(fmt.Sprintf("%6d", cat.count)),
			percentStyle.Render(fmt.Sprintf("(%5.1f%%)", percentage))))
	}
	result.WriteString("\n")

	// File Type Details
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

	displayCount := min(maxCount, len(sorted))
	for i := 0; i < displayCount; i++ {
		item := sorted[i]
		percentage := float64(item.Value) / float64(stats.TotalFiles) * 100
		style := getFileTypeStyle(item.Key)
		result.WriteString(fmt.Sprintf("%s %s %s\n",
			style.Render(fmt.Sprintf("%-12s", item.Key)),
			numberStyle.Render(fmt.Sprintf("%6d", item.Value)),
			percentStyle.Render(fmt.Sprintf("(%5.1f%%)", percentage))))
	}
	result.WriteString("\n")

	// Top N Largest Files section
	result.WriteString(headerStyle.Render(fmt.Sprintf("Top %d Largest Files", maxCount)))
	result.WriteString("\n")
	displayLargestFiles(stats.LargestFiles, &result)

	// Size Distribution section
	result.WriteString(headerStyle.Render("Size Distribution"))
	result.WriteString("\n")
	sizeCategories := []struct {
		name  string
		key   string
		style lipgloss.Style
	}{
		{"tiny (<1KB)", "tiny", tinyStyle},
		{"small (<1MB)", "small", smallStyle},
		{"medium (<100MB)", "medium", mediumStyle},
		{"large (>100MB)", "large", largeStyle},
	}
	for _, cat := range sizeCategories {
		if count, ok := stats.SizeDistribution[cat.key]; ok {
			percentage := float64(count) / float64(stats.TotalFiles) * 100
			result.WriteString(fmt.Sprintf("%s %s %s\n",
				cat.style.Render(fmt.Sprintf("%-16s", cat.name)),
				numberStyle.Render(fmt.Sprintf("%6d", count)),
				percentStyle.Render(fmt.Sprintf("(%5.1f%%)", percentage))))
		}
	}
	result.WriteString("\n")

	// Age Analysis section
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

	// Special Files section
	result.WriteString(headerStyle.Render("Special Files"))
	result.WriteString("\n")
	result.WriteString(fmt.Sprintf("Hidden: %s  System: %s  Symlinks: %s  Write-protected: %s\n",
		numberStyle.Render(fmt.Sprintf("%d", stats.HiddenFiles)),
		numberStyle.Render(fmt.Sprintf("%d", stats.SystemFiles)),
		numberStyle.Render(fmt.Sprintf("%d", stats.Symlinks)),
		warnStyle.Render(fmt.Sprintf("%d", stats.WriteProtected))))
	result.WriteString("\n")

	// Directory Info section
	result.WriteString(headerStyle.Render("Directory Info"))
	result.WriteString("\n")
	result.WriteString(fmt.Sprintf("Empty dirs: %s  Recent changes: %s %s\n",
		numberStyle.Render(fmt.Sprintf("%d", stats.EmptyDirs)),
		numberStyle.Render(fmt.Sprintf("%d", stats.RecentMods)),
		percentStyle.Render(fmt.Sprintf("(%.1f%%)", float64(stats.RecentMods)/float64(stats.TotalFiles)*100))))

	return result.String()
}

func displayLargestFiles(heap *FileSizeHeap, result *strings.Builder) {
	files := make([]FileSize, heap.Len())
	copy(files, *heap)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Size > files[j].Size
	})

	for _, file := range files {
		sizeMB := float64(file.Size) / (1024 * 1024)
		style := getSizeStyle(file.Size)
		result.WriteString(fmt.Sprintf("  %s %s\n",
			style.Render(fmt.Sprintf("%8.1f MB", sizeMB)),
			pathStyle.Render(file.Path)))
	}
	result.WriteString("\n")
}
