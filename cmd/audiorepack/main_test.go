package main

import "testing"

// Real `7zz l -slt` shape: header, "----------", then one blank-line-separated
// record per entry. Solid archives repeat the same Block for every file.
const solidOut = `
7-Zip (z) 24.09 (arm64)

Listing archive: book.7z

--
Path = book.7z
Type = 7z
Solid = +
Blocks = 1

----------
Path = Author - Title
Size = 0
Attributes = D_ drwxr-xr-x

Path = Author - Title/001.mp3
Size = 1000
Attributes = A_ -rw-r--r--
Method = LZMA2:24
Block = 0

Path = Author - Title/002.mp3
Size = 2000
Attributes = A_ -rw-r--r--
Method = LZMA2:24
Block = 0
`

const nonSolidOut = `
Listing archive: book.7z

----------
Path = Author - Title
Size = 0
Attributes = D_ drwxr-xr-x

Path = Author - Title/001.mp3
Size = 1000
Attributes = A_ -rw-r--r--
Method = Copy
Block = 0

Path = Author - Title/002.mp3
Size = 2000
Attributes = A_ -rw-r--r--
Method = Copy
Block = 1
`

// Stored entries sometimes carry no Block line at all — each is its own block.
const noBlockOut = `
----------
Path = Author - Title/001.mp3
Size = 1000
Attributes = A_ -rw-r--r--
Method = Copy

Path = Author - Title/002.mp3
Size = 2000
Attributes = A_ -rw-r--r--
Method = Copy
`

func TestParseSLTSkipsHeaderAndDirs(t *testing.T) {
	entries := parseSLT(solidOut)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3 (1 dir + 2 files): %+v", len(entries), entries)
	}
	if entries[0].path != "Author - Title" || !entries[0].dir {
		t.Errorf("first entry should be the directory, got %+v", entries[0])
	}
	if entries[1].path != "Author - Title/001.mp3" || entries[1].dir {
		t.Errorf("second entry wrong: %+v", entries[1])
	}
	// The "Path = book.7z" line above the "----------" separator describes the
	// archive itself and must not be counted as an entry.
	for _, e := range entries {
		if e.path == "book.7z" {
			t.Error("archive header record leaked into the entry list")
		}
	}
}

func TestCountBlocks(t *testing.T) {
	tests := []struct {
		name          string
		out           string
		blocks, files int
		wantSolid     bool
	}{
		{"solid", solidOut, 1, 2, true},
		{"non-solid", nonSolidOut, 2, 2, false},
		{"stored without block lines", noBlockOut, 2, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks, files := countBlocks(parseSLT(tt.out))
			if blocks != tt.blocks || files != tt.files {
				t.Fatalf("got %d blocks / %d files, want %d / %d", blocks, files, tt.blocks, tt.files)
			}
			if solid := files > 1 && blocks < files; solid != tt.wantSolid {
				t.Errorf("solid = %v, want %v", solid, tt.wantSolid)
			}
		})
	}
}

func TestSameFilesDetectsDrift(t *testing.T) {
	before := parseSLT(solidOut)
	if err := sameFiles(before, parseSLT(nonSolidOut)); err != nil {
		t.Errorf("same file list should compare equal: %v", err)
	}
	if err := sameFiles(before, parseSLT(noBlockOut)[:1]); err == nil {
		t.Error("a shorter file list should be rejected")
	}
}
