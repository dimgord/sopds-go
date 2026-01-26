package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type FileInfo struct {
	Name string
	Size uint64
}

type ZipInfo struct {
	Path       string
	Files      []FileInfo
	TotalSize  uint64
	TotalFiles int
}

// FileKey is used to identify duplicate files (name + size)
type FileKey struct {
	Name string
	Size uint64
}

func main() {
	listFiles := flag.Bool("l", false, "List files in each ZIP")
	onlyDups := flag.Bool("o", false, "With -l, list only duplicate files")
	showDupes := flag.Bool("d", false, "Show only ZIPs that are fully duplicated (can be deleted)")
	flag.Parse()

	dirs := flag.Args()
	if len(dirs) == 0 {
		fmt.Println("Usage: zipdupes [options] <dir1> [dir2] ...")
		fmt.Println("Options:")
		fmt.Println("  -l    List files in each ZIP")
		fmt.Println("  -o    With -l, list only duplicate files")
		fmt.Println("  -d    Show only ZIPs that are fully duplicated (can be deleted)")
		os.Exit(1)
	}

	// Collect all ZIP files
	var zipFiles []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() && strings.ToLower(filepath.Ext(path)) == ".zip" {
				zipFiles = append(zipFiles, path)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error walking %s: %v\n", dir, err)
		}
	}

	if len(zipFiles) == 0 {
		fmt.Println("No ZIP files found")
		return
	}

	fmt.Printf("Found %d ZIP files\n", len(zipFiles))

	// Read all ZIP contents
	zipInfos := make(map[string]*ZipInfo)
	fileLocations := make(map[FileKey][]string) // file -> list of ZIPs containing it

	startTime := time.Now()
	lastUpdate := time.Now()

	for i, path := range zipFiles {
		// Progress update every 200ms
		if time.Since(lastUpdate) > 200*time.Millisecond || i == len(zipFiles)-1 {
			lastUpdate = time.Now()
			elapsed := time.Since(startTime)
			pct := float64(i+1) / float64(len(zipFiles)) * 100

			var etaStr string
			if i > 0 {
				rate := float64(i) / elapsed.Seconds()
				remaining := len(zipFiles) - i - 1
				if rate > 0 {
					eta := time.Duration(float64(remaining)/rate) * time.Second
					etaStr = formatDuration(eta)
				}
			}

			// Progress bar
			barWidth := 30
			filled := int(float64(barWidth) * float64(i+1) / float64(len(zipFiles)))
			bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)

			fmt.Printf("\r[%s] %5.1f%% | %d/%d | ETA: %s   ", bar, pct, i+1, len(zipFiles), etaStr)
		}

		info, err := readZipInfo(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nError reading %s: %v\n", path, err)
			continue
		}

		zipInfos[path] = info

		// Track which ZIPs contain each file
		for _, f := range info.Files {
			key := FileKey{Name: f.Name, Size: f.Size}
			fileLocations[key] = append(fileLocations[key], path)
		}
	}

	elapsed := time.Since(startTime)
	fmt.Printf("\r[%s] 100.0%% | %d/%d | Done in %s   \n", strings.Repeat("=", 30), len(zipFiles), len(zipFiles), formatDuration(elapsed))

	// Analyze duplicates
	var fullyDuplicated []string
	var partiallyDuplicated []string

	for path, info := range zipInfos {
		if len(info.Files) == 0 {
			continue
		}

		duplicateCount := 0
		for _, f := range info.Files {
			key := FileKey{Name: f.Name, Size: f.Size}
			locations := fileLocations[key]
			// File is duplicate if it exists in another ZIP (not just this one)
			if len(locations) > 1 {
				duplicateCount++
			}
		}

		if duplicateCount == len(info.Files) {
			// All files are duplicates - this ZIP can be deleted
			fullyDuplicated = append(fullyDuplicated, path)
		} else if duplicateCount > 0 {
			partiallyDuplicated = append(partiallyDuplicated, path)
		}
	}

	// Sort results
	sort.Strings(fullyDuplicated)
	sort.Strings(partiallyDuplicated)

	// Output
	if *listFiles && !*showDupes {
		if *onlyDups {
			fmt.Println("\n=== Duplicate Files ===")
		} else {
			fmt.Println("\n=== ZIP Contents ===")
		}
		paths := make([]string, 0, len(zipInfos))
		for p := range zipInfos {
			paths = append(paths, p)
		}
		sort.Strings(paths)

		for _, path := range paths {
			info := zipInfos[path]
			zipPrinted := false

			for _, f := range info.Files {
				key := FileKey{Name: f.Name, Size: f.Size}
				locations := fileLocations[key]
				isDup := len(locations) > 1

				// Skip non-duplicates if -o flag is set
				if *onlyDups && !isDup {
					continue
				}

				// Print ZIP header on first file output
				if !zipPrinted {
					fmt.Printf("\n%s (%d files, %s)\n", path, info.TotalFiles, formatSize(info.TotalSize))
					zipPrinted = true
				}

				if isDup {
					// Count occurrences in current ZIP vs other ZIPs
					var others []string
					sameZipCount := 0
					for _, loc := range locations {
						if loc == path {
							sameZipCount++
						} else {
							others = append(others, filepath.Base(loc))
						}
					}

					// Build DUP description
					var dupDesc string
					if sameZipCount > 1 {
						dupDesc = fmt.Sprintf("%dx in same ZIP", sameZipCount)
						if len(others) > 0 {
							dupDesc += "; " + strings.Join(others, ", ")
						}
					} else {
						dupDesc = strings.Join(others, ", ")
					}

					fmt.Printf("  %s (%s) [LISTED in: %s; DUP in: %s]\n",
						f.Name, formatSize(f.Size), filepath.Base(path), dupDesc)
				} else {
					fmt.Printf("  %s (%s)\n", f.Name, formatSize(f.Size))
				}
			}
		}
	}

	if *showDupes {
		if len(fullyDuplicated) == 0 {
			fmt.Println("\nNo fully duplicated ZIPs found")
		} else {
			fmt.Printf("\n=== Fully Duplicated ZIPs (can be deleted): %d ===\n", len(fullyDuplicated))
			var totalSize uint64
			for _, path := range fullyDuplicated {
				info := zipInfos[path]
				fmt.Printf("%s (%d files, %s)\n", path, info.TotalFiles, formatSize(info.TotalSize))
				totalSize += info.TotalSize

				if *listFiles {
					for _, f := range info.Files {
						key := FileKey{Name: f.Name, Size: f.Size}
						otherZips := fileLocations[key]
						// Show where else this file exists
						var others []string
						for _, other := range otherZips {
							if other != path {
								others = append(others, filepath.Base(other))
							}
						}
						fmt.Printf("  %s (%s) -> also in: %s\n", f.Name, formatSize(f.Size), strings.Join(others, ", "))
					}
				}
			}
			fmt.Printf("\nTotal: %d ZIPs, %s can be freed\n", len(fullyDuplicated), formatSize(totalSize))
		}
	} else {
		// Summary
		fmt.Printf("\n=== Summary ===\n")
		fmt.Printf("Total ZIPs: %d\n", len(zipInfos))
		fmt.Printf("Fully duplicated (can delete): %d\n", len(fullyDuplicated))
		fmt.Printf("Partially duplicated: %d\n", len(partiallyDuplicated))
		fmt.Printf("Unique: %d\n", len(zipInfos)-len(fullyDuplicated)-len(partiallyDuplicated))

		if len(fullyDuplicated) > 0 {
			fmt.Println("\nFully duplicated ZIPs:")
			var totalSize uint64
			for _, path := range fullyDuplicated {
				info := zipInfos[path]
				totalSize += info.TotalSize
				fmt.Printf("  %s (%s)\n", path, formatSize(info.TotalSize))
			}
			fmt.Printf("Total size that can be freed: %s\n", formatSize(totalSize))
		}
	}
}

func readZipInfo(path string) (*ZipInfo, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	info := &ZipInfo{
		Path: path,
	}

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		fi := FileInfo{
			Name: f.Name,
			Size: f.UncompressedSize64,
		}
		info.Files = append(info.Files, fi)
		info.TotalSize += f.UncompressedSize64
		info.TotalFiles++
	}

	return info, nil
}

func formatSize(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	d = d.Round(time.Second)

	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
