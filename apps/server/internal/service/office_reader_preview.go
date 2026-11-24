package service

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"golang.org/x/sync/singleflight"
)

const (
	maxReaderOfficeSourceBytes   = 32 << 20
	maxReaderOfficeExpandedBytes = 128 << 20
	readerOfficeRenderTimeout    = 20 * time.Second
)

// OfficeReaderPreviewService provides previews for already-authorized Office documents
// when the reader cannot use a separate ONLYOFFICE instance.
type OfficeReaderPreviewService struct {
	renderer  *OfficeHTMLRenderService
	documents repository.ReaderPageRepository
	renders   singleflight.Group
	slots     chan struct{}
}

func NewOfficeReaderPreviewService(renderer *OfficeHTMLRenderService) *OfficeReaderPreviewService {
	return &OfficeReaderPreviewService{
		renderer:  renderer,
		documents: repository.NewGormReaderPageRepository(renderer.db),
		slots:     make(chan struct{}, 2),
	}
}

func (s *OfficeReaderPreviewService) Read(
	ctx context.Context,
	spaceID string,
	document ReaderDocumentViewModel,
) (string, error) {
	if document.RenderStatus == models.DocumentRenderStatusSuccess && strings.TrimSpace(document.ContentMD) != "" {
		return sanitizeOfficePreviewHTML(document.ContentMD)
	}
	if s == nil || s.renderer == nil || s.renderer.documentAttachmentRepo == nil ||
		document.SourceBlobID == nil || *document.SourceBlobID == "" {
		return "", errors.New("office preview source unavailable")
	}
	blobID := *document.SourceBlobID
	key := fmt.Sprintf("%s:%d:%s", document.ID, document.ContentVersion, blobID)
	render := s.renders.DoChan(key, func() (any, error) {
		renderCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), readerOfficeRenderTimeout)
		defer cancel()
		return s.render(renderCtx, spaceID, document, blobID)
	})
	select {
	case result := <-render:
		if result.Err != nil {
			return "", result.Err
		}
		return result.Val.(string), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *OfficeReaderPreviewService) render(
	ctx context.Context,
	spaceID string,
	document ReaderDocumentViewModel,
	blobID string,
) (string, error) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	current, err := s.documents.GetDocumentByDocumentID(ctx, document.ID)
	if err != nil {
		return "", err
	}
	if current.SpaceID != spaceID || current.ContentVersion != document.ContentVersion ||
		current.SourceBlobID == nil || *current.SourceBlobID != blobID {
		return "", errors.New("office preview source changed")
	}
	if current.RenderStatus == models.DocumentRenderStatusSuccess && strings.TrimSpace(current.ContentMD) != "" {
		return sanitizeOfficePreviewHTML(current.ContentMD)
	}
	blob, err := s.renderer.documentAttachmentRepo.GetBlobByBlobID(ctx, blobID)
	if err != nil {
		return "", err
	}
	if blob == nil || blob.SizeBytes > maxReaderOfficeSourceBytes {
		return "", errors.New("office preview source unavailable or too large")
	}
	blobReader := adminSpaceExportDefaultBlobContentReader{
		localRootDir:         s.renderer.localRootDir,
		imageHostingService:  s.renderer.imageHostingService,
		maxBlobReadSizeBytes: maxReaderOfficeSourceBytes,
	}
	fileName := ""
	if current.SourceFileName != nil {
		fileName = *current.SourceFileName
	}
	source, err := blobReader.ReadBlobContent(ctx, *blob, fileName)
	if err != nil {
		return "", err
	}
	if err := validateOfficePreviewArchive(source); err != nil {
		return "", err
	}
	renderedHTML, err := s.renderer.renderOfficeHTML(ctx, OfficeHTMLRenderTask{
		DocumentID:        document.ID,
		SpaceID:           spaceID,
		Format:            current.Format,
		ContentVersion:    current.ContentVersion,
		SourceContent:     source,
		SourceBlobID:      blobID,
		InlineImageAssets: true,
	})
	if err != nil {
		return "", err
	}
	renderedHTML, err = sanitizeOfficePreviewHTML(renderedHTML)
	if err != nil {
		return "", err
	}
	saved, err := s.documents.SaveOfficePreview(ctx, document.ID, current.ContentVersion, blobID, renderedHTML, time.Now().UTC())
	if err != nil {
		return "", err
	}
	if !saved {
		return "", errors.New("office preview source changed")
	}
	s.renderer.enqueueSearchSync(document.ID)
	return renderedHTML, nil
}

func validateOfficePreviewArchive(source []byte) error {
	archive, err := zip.NewReader(bytes.NewReader(source), int64(len(source)))
	if err != nil {
		return err
	}
	remaining := uint64(maxReaderOfficeExpandedBytes)
	for _, file := range archive.File {
		if file.UncompressedSize64 > remaining {
			return errors.New("office preview archive exceeds its expansion limit")
		}
		remaining -= file.UncompressedSize64
	}
	return nil
}
