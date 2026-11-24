package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"os"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
	unicodeencoding "golang.org/x/text/encoding/unicode"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
)

const workspaceFolderManifestLimit = 1 << 20

var errWorkspaceImportFolderManifest = errors.New("上传文件夹的目录结构无效或超过限制")

type workspaceFolderManifest struct {
	Paths       []string `json:"paths"`
	Directories []string `json:"directories"`
}

type workspaceFolderImport struct {
	files           []workspaceImportedSource
	directories     []string
	unimportedItems []workspaceImportItemResponse
}

func collectWorkspaceFolderImport(rawManifest string, headers []*multipart.FileHeader) (workspaceFolderImport, error) {
	var manifest workspaceFolderManifest
	if len(rawManifest) > workspaceFolderManifestLimit ||
		!utf8.ValidString(rawManifest) ||
		json.Unmarshal([]byte(rawManifest), &manifest) != nil ||
		len(manifest.Paths) != len(headers) ||
		len(manifest.Paths)+len(manifest.Directories) == 0 ||
		len(manifest.Directories) > workspaceImportMaxZipEntryCount {
		return workspaceFolderImport{}, errWorkspaceImportFolderManifest
	}

	root := ""
	filePaths := make(map[string]struct{}, len(manifest.Paths))
	directoryPaths := make(map[string]struct{}, len(manifest.Directories))
	collectPath := func(rawPath string, isFile bool) bool {
		parts := strings.Split(rawPath, "/")
		limit := workspaceImportMaxZipDirectoryDepth
		if isFile {
			limit++
			if len(parts) < 2 {
				return false
			}
		}
		if len(parts) > limit {
			return false
		}
		for _, part := range parts {
			if !validWorkspaceFolderPart(part) {
				return false
			}
		}
		if root == "" {
			root = parts[0]
		}
		if root != parts[0] {
			return false
		}

		dirPath := rawPath
		if isFile {
			if _, exists := filePaths[rawPath]; exists {
				return false
			}
			filePaths[rawPath] = struct{}{}
			dirPath = path.Dir(rawPath)
		}
		for dirPath != "." {
			directoryPaths[dirPath] = struct{}{}
			dirPath = path.Dir(dirPath)
		}
		return len(directoryPaths) <= workspaceImportMaxZipEntryCount
	}

	for _, dirPath := range manifest.Directories {
		if !collectPath(dirPath, false) {
			return workspaceFolderImport{}, errWorkspaceImportFolderManifest
		}
	}
	for _, filePath := range manifest.Paths {
		if !collectPath(filePath, true) {
			return workspaceFolderImport{}, errWorkspaceImportFolderManifest
		}
	}
	for filePath := range filePaths {
		if _, exists := directoryPaths[filePath]; exists {
			return workspaceFolderImport{}, errWorkspaceImportFolderManifest
		}
	}

	folder := workspaceFolderImport{
		files:       make([]workspaceImportedSource, 0, len(headers)),
		directories: make([]string, 0, len(directoryPaths)),
	}
	var zipEntryCount int
	var zipExpandedBytes int64
	for index, header := range headers {
		filePath := manifest.Paths[index]
		if isHiddenWorkspaceImportPath(filePath) {
			folder.unimportedItems = append(folder.unimportedItems, skippedHiddenWorkspaceImportItem(filePath, false))
			continue
		}
		content, err := readWorkspaceImportFileHeader(header)
		if err != nil {
			return workspaceFolderImport{}, err
		}
		if detectWorkspaceImportType(filePath) == importDetectedTypeZIP {
			parentDir := path.Dir(filePath)
			archiveName := uniqueWorkspaceFolderArchiveName(filePath, directoryPaths, filePaths)
			archive := workspaceZipImportState{entryCount: zipEntryCount, totalBytes: zipExpandedBytes}
			err := expandWorkspaceImportZip(content, parentDir, archiveName, 0, &archive)
			// Keep limits cumulative even when an archive's partial output is discarded.
			zipEntryCount, zipExpandedBytes = archive.entryCount, archive.totalBytes
			if err != nil {
				folder.unimportedItems = append(folder.unimportedItems, workspaceImportItemResponse{
					SourceName:   path.Base(filePath),
					SourcePath:   filePath,
					DetectedType: importDetectedTypeZIP,
					Status:       importItemStatusFailed,
					Stage:        importItemStageUnzipping,
					ErrorMessage: "解压 ZIP 失败：" + err.Error(),
				})
				continue
			}
			for _, dir := range archive.containerDirs {
				directoryPaths[dir] = struct{}{}
			}
			for _, dir := range archive.directories {
				directoryPaths[dir] = struct{}{}
			}
			folder.files = append(folder.files, archive.files...)
			folder.unimportedItems = append(folder.unimportedItems, archive.skippedItems...)
			continue
		}
		folder.files = append(folder.files, workspaceImportedSource{
			SourceName: path.Base(filePath),
			SourcePath: filePath,
			Content:    content,
		})
	}
	for dirPath := range directoryPaths {
		if isHiddenWorkspaceImportPath(dirPath) {
			folder.unimportedItems = append(folder.unimportedItems, skippedHiddenWorkspaceImportItem(dirPath, true))
			continue
		}
		folder.directories = append(folder.directories, dirPath)
	}
	// A parent path sorts before any of its descendants.
	slices.Sort(folder.directories)
	slices.SortFunc(folder.files, func(left, right workspaceImportedSource) int {
		return strings.Compare(left.SourcePath, right.SourcePath)
	})
	slices.SortFunc(folder.unimportedItems, func(left, right workspaceImportItemResponse) int {
		return strings.Compare(left.SourcePath, right.SourcePath)
	})
	return folder, nil
}

func uniqueWorkspaceFolderArchiveName(filePath string, directoryPaths, filePaths map[string]struct{}) string {
	name := path.Base(filePath)
	extension := path.Ext(name)
	stem := strings.TrimSpace(strings.TrimSuffix(name, extension))
	dir := path.Dir(filePath)
	for suffix := 0; ; suffix++ {
		candidate := name
		if suffix > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, suffix, extension)
		}
		extractedPath := workspaceZipContainerPath(dir, candidate)
		if extractedPath == "" {
			return candidate
		}
		_, directoryExists := directoryPaths[extractedPath]
		_, fileExists := filePaths[extractedPath]
		if !directoryExists && (!fileExists || detectWorkspaceImportType(extractedPath) == importDetectedTypeZIP) {
			return candidate
		}
	}
}

func validWorkspaceFolderPart(part string) bool {
	if strings.TrimSpace(part) == "" || part == "." || part == ".." ||
		strings.ContainsRune(part, '\\') || utf8.RuneCountInString(part) > 255 {
		return false
	}
	return strings.IndexFunc(part, unicode.IsControl) < 0
}

func (h *workspaceHandler) importWorkspaceFolder(
	ctx context.Context,
	runtimeState *workspaceImportRuntime,
	folder workspaceFolderImport,
) []workspaceImportItemResponse {
	for _, source := range folder.files {
		runtimeState.assetSourcesByPath[source.SourcePath] = source
	}

	items := make([]workspaceImportItemResponse, 0, len(folder.directories)+len(folder.files)+len(folder.unimportedItems))
	for _, dirPath := range folder.directories {
		item := workspaceImportItemResponse{
			SourceName:   path.Base(dirPath),
			SourcePath:   dirPath,
			DetectedType: importDetectedTypeDirectory,
			Stage:        importItemStageCreatingFolder,
		}
		nodeID, err := h.ensureWorkspaceImportDirectory(ctx, runtimeState, dirPath)
		if err == nil {
			item.CreatedNodeID = &nodeID
		}
		items = append(items, finishWorkspaceFolderItem(item, err))
	}
	for _, source := range folder.files {
		item := workspaceImportItemResponse{
			SourceName:   source.SourceName,
			SourcePath:   source.SourcePath,
			DetectedType: detectWorkspaceImportType(source.SourcePath),
			Stage:        importItemStageCreatingFolder,
		}
		nodeID, docID, err := h.importWorkspaceFolderFile(ctx, runtimeState, source, &item)
		item.CreatedNodeID = nodeID
		item.CreatedDocumentID = docID
		items = append(items, finishWorkspaceFolderItem(item, err))
	}
	return append(items, folder.unimportedItems...)
}

func finishWorkspaceFolderItem(item workspaceImportItemResponse, err error) workspaceImportItemResponse {
	if err != nil {
		item.Status = importItemStatusFailed
		item.ErrorMessage = err.Error()
		return item
	}
	item.Status = importItemStatusSuccess
	item.Stage = importItemStageDone
	return item
}

func (h *workspaceHandler) importWorkspaceFolderFile(
	ctx context.Context,
	runtimeState *workspaceImportRuntime,
	source workspaceImportedSource,
	item *workspaceImportItemResponse,
) (*string, *string, error) {
	parentID, err := h.ensureWorkspaceImportDirectory(ctx, runtimeState, path.Dir(source.SourcePath))
	if err != nil {
		return nil, nil, err
	}
	item.Stage = importItemStageConverting
	conversion, err := h.convertWorkspaceFolderFile(ctx, runtimeState, source, item.DetectedType)
	if errors.Is(err, errWorkspaceImportUnsupportedType) || errors.Is(err, errWorkspaceImportTextEncoding) {
		item.DetectedType = importDetectedTypeUnsupported
		item.Stage = importItemStageCreatingDocument
		nodeID, createErr := h.createWorkspaceImportedUnsupportedFile(ctx, runtimeState, &parentID, source.SourceName)
		return nodeID, nil, createErr
	}
	if err != nil {
		return nil, nil, err
	}
	item.Stage = importItemStageCreatingDocument
	item.DetectedType = conversion.DetectedType
	if models.IsOfficeDocumentFormat(conversion.Format) {
		return h.createWorkspaceImportedOfficeDocument(ctx, runtimeState, &parentID, conversion, source.SourcePath)
	}
	nodeID, docID, err := h.createWorkspaceImportedMarkdownDocument(ctx, runtimeState, &parentID, conversion, source.SourcePath)
	if err != nil && conversion.ImageSource != nil {
		if cleanupErr := os.Remove(conversion.ImageSource.localPath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
			slog.WarnContext(ctx, "清理未写入文档的导入图片失败", "err", cleanupErr)
		}
	}
	return nodeID, docID, err
}

func (h *workspaceHandler) convertWorkspaceFolderFile(
	ctx context.Context,
	runtimeState *workspaceImportRuntime,
	source workspaceImportedSource,
	detectedType string,
) (workspaceImportConversionResult, error) {
	conversion := workspaceImportConversionResult{
		Format:       models.DocumentFormatMarkdown,
		Title:        source.SourceName,
		DetectedType: detectedType,
	}
	switch detectedType {
	case importDetectedTypeDOCX, importDetectedTypeXLSX:
		if !workspaceFolderOfficePackageReadable(source.Content, detectedType) {
			return workspaceImportConversionResult{}, errWorkspaceImportUnsupportedType
		}
		converted, err := h.convertWorkspaceImportSource(ctx, runtimeState, source)
		if err != nil {
			if ctx.Err() != nil {
				return workspaceImportConversionResult{}, ctx.Err()
			}
			return workspaceImportConversionResult{}, errWorkspaceImportUnsupportedType
		}
		converted.Title = source.SourceName
		return converted, nil
	case importDetectedTypeImage:
		contentType := detectWorkspaceFolderImageContentType(source)
		if contentType == "" {
			return workspaceImportConversionResult{}, errWorkspaceImportUnsupportedType
		}
		imageSource, err := h.stageWorkspaceFolderImage(ctx, contentType, source.Content, runtimeState.now)
		if err != nil {
			return workspaceImportConversionResult{}, err
		}
		conversion.ImportedFileKind = models.ImportedFileKindImage
		conversion.ImageSource = imageSource
	case importDetectedTypeMarkdown:
		content, err := decodeWorkspaceFolderText(source.Content, true)
		if err != nil {
			return workspaceImportConversionResult{}, err
		}
		content, err = h.rewriteWorkspaceImportMarkdownImageSources(ctx, runtimeState, source.SourcePath, content)
		if err != nil {
			return workspaceImportConversionResult{}, err
		}
		conversion.ContentMD = content
	default:
		knownText, supported := workspaceFolderTextType(source.SourceName)
		if !supported {
			return workspaceImportConversionResult{}, errWorkspaceImportUnsupportedType
		}
		content, err := decodeWorkspaceFolderText(source.Content, knownText)
		if err != nil {
			return workspaceImportConversionResult{}, err
		}
		conversion.ContentMD = content
		conversion.DetectedType = importDetectedTypeText
		conversion.ImportedFileKind = models.ImportedFileKindText
	}
	return conversion, nil
}

func workspaceFolderOfficePackageReadable(content []byte, detectedType string) bool {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil || len(reader.File) > workspaceImportMaxZipEntryCount {
		return false
	}
	mainPart := "word/document.xml"
	if detectedType == importDetectedTypeXLSX {
		mainPart = "xl/workbook.xml"
	}
	hasMainPart, hasContentTypes := false, false
	remainingBytes := uint64(workspaceImportMaxZipExpandedBytes)
	for _, file := range reader.File {
		if file.UncompressedSize64 > remainingBytes {
			return false
		}
		remainingBytes -= file.UncompressedSize64
		switch file.Name {
		case mainPart:
			hasMainPart = true
		case "[Content_Types].xml":
			hasContentTypes = true
		}
	}
	return hasMainPart && hasContentTypes
}

func workspaceFolderTextType(name string) (knownText bool, supported bool) {
	switch strings.ToLower(path.Ext(name)) {
	case ".doc", ".docm", ".xls", ".xlsm", ".ppt", ".pptx", ".pdf", ".pages", ".numbers", ".key",
		".odt", ".ods", ".odp", ".epub", ".heic", ".heif", ".avif", ".ico", ".tiff", ".tif",
		".mp3", ".m4a", ".wav", ".ogg", ".flac", ".mp4", ".mkv", ".mov", ".webm", ".avi",
		".rar", ".7z", ".tar", ".gz", ".zip", ".bin", ".dmg", ".exe", ".dll", ".so", ".class", ".wasm":
		return false, false
	case ".txt", ".csv", ".tsv", ".json", ".jsonl", ".yaml", ".yml", ".toml", ".xml", ".html", ".htm",
		".css", ".scss", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".vue", ".svelte",
		".ini", ".conf", ".env", ".properties", ".log", ".sql", ".rst", ".tex",
		".go", ".py", ".rb", ".php", ".swift", ".rs", ".c", ".h", ".cpp", ".hpp",
		".java", ".kt", ".cs", ".sh", ".bash", ".zsh", ".ps1", ".bat":
		return true, true
	}
	return false, true
}

func decodeWorkspaceFolderText(raw []byte, knownText bool) (string, error) {
	if len(raw) == 0 && !knownText {
		return "", errWorkspaceImportTextEncoding
	}
	content := bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(content) {
		if isWorkspaceFolderReadableText(string(content)) {
			return string(content), nil
		}
		return "", errWorkspaceImportTextEncoding
	}

	var decoded string
	var err error
	switch {
	case bytes.HasPrefix(raw, []byte{0xFE, 0xFF}), bytes.HasPrefix(raw, []byte{0xFF, 0xFE}):
		var decodedBytes []byte
		decodedBytes, err = unicodeencoding.UTF16(unicodeencoding.BigEndian, unicodeencoding.ExpectBOM).NewDecoder().Bytes(raw)
		decoded = string(decodedBytes)
	case knownText:
		decoded, err = decodeWorkspaceImportText(raw)
	default:
		return "", errWorkspaceImportTextEncoding
	}
	if err != nil || strings.ContainsRune(decoded, utf8.RuneError) || !isWorkspaceFolderReadableText(decoded) {
		return "", errWorkspaceImportTextEncoding
	}
	return decoded, nil
}

func isWorkspaceFolderReadableText(content string) bool {
	return strings.IndexFunc(content, func(r rune) bool {
		switch r {
		case '\t', '\n', '\r', '\f':
			return false
		default:
			return unicode.IsControl(r)
		}
	}) < 0
}

func (h *workspaceHandler) createWorkspaceImportedUnsupportedFile(
	ctx context.Context,
	runtimeState *workspaceImportRuntime,
	parentID *string,
	fileName string,
) (*string, error) {
	maxSort, err := h.workspaceRepo.GetMaxNodeSort(ctx, runtimeState.spaceID, parentID)
	if err != nil {
		return nil, err
	}
	nodeID := strings.ToLower(ulid.Make().String())
	title := ensureWorkspaceImportUniqueTitle(runtimeState.existingSiblingTitles, parentID, fileName)
	node := &models.Node{
		NodeID:           nodeID,
		SpaceID:          runtimeState.spaceID,
		ParentNodeID:     parentID,
		Type:             models.NodeTypeDoc,
		ImportedFileKind: models.ImportedFileKindUnsupported,
		Title:            title,
		Sort:             maxSort + 1,
		CreatedByUserID:  &runtimeState.actorUserID,
		UpdatedByUserID:  &runtimeState.actorUserID,
		CreatedAt:        runtimeState.now,
		UpdatedAt:        runtimeState.now,
	}
	if err := h.workspaceRepo.CreateNode(ctx, repository.WorkspaceCreateNodeParams{
		Node:       node,
		TouchSpace: runtimeState.spaceID,
		TouchedAt:  runtimeState.now,
	}); err != nil {
		return nil, err
	}
	runtimeState.createdNodes = append(runtimeState.createdNodes, workspaceImportCreatedNodeResponse{
		NodeID:           nodeID,
		ParentID:         parentID,
		Title:            title,
		Type:             models.NodeTypeDoc,
		ImportedFileKind: models.ImportedFileKindUnsupported,
	})
	return &nodeID, nil
}
