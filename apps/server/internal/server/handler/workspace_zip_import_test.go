package handler

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"slices"
	"testing"
)

func TestExpandWorkspaceImportZipKeepsNamedContainers(t *testing.T) {
	nested := buildWorkspaceArchiveForExtractionTest(t, nil)
	archive := buildWorkspaceArchiveForExtractionTest(t, []workspaceArchiveTestEntry{
		{name: "./", mode: fs.ModeDir | 0755},
		{name: "empty/", mode: fs.ModeDir | 0755},
		{name: "directory-without-separator", mode: fs.ModeDir | 0755},
		{name: "docs/README.md", content: []byte("# Catalogue")},
		{name: "docs/nested.zip", content: nested},
		{name: ".git/config", content: []byte("hidden")},
		{name: "linked.md", mode: fs.ModeSymlink | 0777, content: []byte("docs/README.md")},
	})

	var state workspaceZipImportState
	if err := expandWorkspaceImportZip(archive, "target", "archive.v2.ZIP", 0, &state); err != nil {
		t.Fatal(err)
	}
	if want := []string{"target/archive.v2", "target/archive.v2/docs/nested"}; !slices.Equal(state.containerDirs, want) {
		t.Fatalf("archive containers = %v, want %v", state.containerDirs, want)
	}
	if want := []string{"target/archive.v2/empty", "target/archive.v2/directory-without-separator"}; !slices.Equal(state.directories, want) {
		t.Fatalf("explicit directories = %v, want %v", state.directories, want)
	}
	if len(state.files) != 1 || state.files[0].SourcePath != "target/archive.v2/docs/README.md" {
		t.Fatalf("unexpected extracted files: %+v", state.files)
	}
	if len(state.skippedItems) != 2 || state.skippedItems[0].SourcePath != "target/archive.v2/.git/config" ||
		state.skippedItems[1].SourcePath != "target/archive.v2/linked.md" {
		t.Fatalf("unexpected skipped files: %+v", state.skippedItems)
	}
}

func TestNormalizeWorkspaceImportZipEntryPathRejectsUnsafePaths(t *testing.T) {
	for _, path := range []string{"/absolute.md", `C:\\absolute.md`, `\\server\share\file.md`, "../outside.md", "inside/../file.md", "inside/.."} {
		t.Run(path, func(t *testing.T) {
			if _, _, err := normalizeWorkspaceImportZipEntryPath("target/archive", path); err == nil {
				t.Fatalf("unsafe entry path %q was accepted", path)
			}
		})
	}
}

type workspaceArchiveTestEntry struct {
	name    string
	mode    fs.FileMode
	content []byte
}

func buildWorkspaceArchiveForExtractionTest(t *testing.T, entries []workspaceArchiveTestEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if len(entry.content) > 0 {
			if _, err := file.Write(entry.content); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
