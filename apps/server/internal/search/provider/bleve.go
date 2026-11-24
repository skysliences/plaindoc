package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/keyword"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"gorm.io/gorm"
)

const (
	defaultBleveSearchPage       = 1
	defaultBleveSearchPageSize   = 20
	maxBleveSearchPageSize       = 200
	defaultBleveSearchChunkSize  = 200
	maxBleveSearchCandidateCount = 5000
	maxBleveSearchScopeFilterIDs = 512
)

// BleveProviderOptions 定义 BleveProvider 初始化参数。
type BleveProviderOptions struct {
	DB             *gorm.DB
	VisibilityRepo repository.SearchVisibilityRepository
	IndexPath      string
}

// BleveProvider 基于 Bleve 的内置检索引擎。
type BleveProvider struct {
	visibilityRepo repository.SearchVisibilityRepository
	indexPath      string

	mu    sync.RWMutex
	index bleve.Index
}

// NewBleveProvider 创建 BleveProvider。
func NewBleveProvider(options BleveProviderOptions) *BleveProvider {
	visibilityRepo := options.VisibilityRepo
	if visibilityRepo == nil && options.DB != nil {
		visibilityRepo = repository.NewGormSearchVisibilityRepository(options.DB)
	}
	return &BleveProvider{
		visibilityRepo: visibilityRepo,
		indexPath:      strings.TrimSpace(options.IndexPath),
	}
}

func (p *BleveProvider) Name() string {
	return "bleve"
}

func (p *BleveProvider) Health(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil {
		return errors.New("bleve search provider is nil")
	}
	if p.visibilityRepo == nil {
		return errors.New("bleve search provider visibility repository is nil")
	}
	if strings.TrimSpace(p.indexPath) == "" {
		return errors.New("bleve index path is empty")
	}

	indexInstance, err := p.ensureIndex(ctx)
	if err != nil {
		return err
	}
	_, err = indexInstance.DocCount()
	return err
}

func (p *BleveProvider) Verify(ctx context.Context, config map[string]any) error {
	return p.Health(ctx)
}

func (p *BleveProvider) EnsureSchema(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := p.ensureIndex(ctx)
	return err
}

func (p *BleveProvider) Upsert(ctx context.Context, records []IndexRecord) error {
	if err := p.Health(ctx); err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	indexInstance, err := p.ensureIndex(ctx)
	if err != nil {
		return err
	}

	batch := indexInstance.NewBatch()
	for _, item := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		docID := strings.TrimSpace(item.DocID)
		if docID == "" {
			continue
		}
		if item.IsDeleted {
			batch.Delete(docID)
			continue
		}

		updatedAtUnix := item.UpdatedAtUnix
		if updatedAtUnix <= 0 {
			updatedAtUnix = time.Now().UTC().Unix()
		}

		document := bleveIndexDocument{
			DocID:           docID,
			SpaceID:         strings.TrimSpace(item.SpaceID),
			NodeID:          strings.TrimSpace(item.NodeID),
			Title:           strings.TrimSpace(item.Title),
			BodyPlain:       strings.TrimSpace(item.BodyPlain),
			Terms:           splitTerms(item.Terms),
			TitleTerms:      splitTerms(item.TitleTerms),
			VisibilityScope: strings.TrimSpace(item.VisibilityScope),
			MinRole:         item.MinRole,
			UpdatedAtUnix:   updatedAtUnix,
			IsDeleted:       false,
			SpaceStatus:     strings.TrimSpace(item.SpaceStatus),
			DocStatus:       strings.TrimSpace(item.DocStatus),
			AnalyzerName:    strings.TrimSpace(item.AnalyzerName),
			AnalyzerVersion: strings.TrimSpace(item.AnalyzerVersion),
		}
		if err := batch.Index(docID, document); err != nil {
			return err
		}
	}

	return indexInstance.Batch(batch)
}

func (p *BleveProvider) Delete(ctx context.Context, docIDs []string) error {
	if err := p.Health(ctx); err != nil {
		return err
	}
	if len(docIDs) == 0 {
		return nil
	}

	indexInstance, err := p.ensureIndex(ctx)
	if err != nil {
		return err
	}

	batch := indexInstance.NewBatch()
	for _, item := range docIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		docID := strings.TrimSpace(item)
		if docID == "" {
			continue
		}
		batch.Delete(docID)
	}
	return indexInstance.Batch(batch)
}

func (p *BleveProvider) PurgeBySpace(ctx context.Context, spaceID string) error {
	if err := p.Health(ctx); err != nil {
		return err
	}
	normalizedSpaceID := strings.TrimSpace(spaceID)
	if normalizedSpaceID == "" {
		return nil
	}

	indexInstance, err := p.ensureIndex(ctx)
	if err != nil {
		return err
	}

	query := bleve.NewTermQuery(normalizedSpaceID)
	query.SetField("space_id")
	documentIDs, err := p.searchDocumentIDs(ctx, indexInstance, query)
	if err != nil {
		return err
	}
	if len(documentIDs) == 0 {
		return nil
	}
	return p.Delete(ctx, documentIDs)
}

func (p *BleveProvider) Search(ctx context.Context, request SearchRequest) (SearchResponse, error) {
	if err := p.Health(ctx); err != nil {
		return SearchResponse{}, err
	}

	normalizedQuery := strings.TrimSpace(request.Query)
	if normalizedQuery == "" {
		return SearchResponse{Total: 0, Hits: []SearchHit{}}, nil
	}

	normalizedPage, normalizedPageSize := normalizeBlevePagination(request.Page, request.PageSize)
	indexInstance, err := p.ensureIndex(ctx)
	if err != nil {
		return SearchResponse{}, err
	}

	query := buildBleveSearchQuery(request)
	searchSnippetTerms := buildSearchSnippetKeywords(request.Query, request.RawQuery)
	searchResults, err := p.searchCandidates(ctx, indexInstance, query, request.Sort, searchSnippetTerms)
	if err != nil {
		return SearchResponse{}, err
	}
	if len(searchResults) == 0 {
		return SearchResponse{Total: 0, Hits: []SearchHit{}}, nil
	}
	filteredHits, err := p.filterCandidatesByVisibility(ctx, request, searchResults)
	if err != nil {
		return SearchResponse{}, err
	}
	if len(filteredHits) == 0 {
		return SearchResponse{Total: 0, Hits: []SearchHit{}}, nil
	}

	total := int64(len(filteredHits))
	if normalizedPage-1 > (len(filteredHits)-1)/normalizedPageSize {
		return SearchResponse{Total: total, Hits: []SearchHit{}}, nil
	}
	start := (normalizedPage - 1) * normalizedPageSize
	end := start + normalizedPageSize
	if end > len(filteredHits) {
		end = len(filteredHits)
	}

	return SearchResponse{
		Total: total,
		Hits:  filteredHits[start:end],
	}, nil
}

func (p *BleveProvider) filterCandidatesByVisibility(
	ctx context.Context,
	request SearchRequest,
	candidates []bleveSearchCandidate,
) ([]SearchHit, error) {
	if len(candidates) == 0 {
		return []SearchHit{}, nil
	}

	// 索引只负责召回，权限和状态以当前数据库记录为准。
	candidateIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.DocID)
	}
	visibleIDs, err := p.filterVisibleDocIDs(ctx, request, candidateIDs)
	if err != nil {
		return nil, err
	}

	isSingleSpaceSearch := strings.TrimSpace(request.SpaceID) != ""
	needsHigherRole := func(candidate bleveSearchCandidate) bool {
		return !request.CanViewAllDocuments && candidate.MinRole > 1 &&
			strings.EqualFold(strings.TrimSpace(candidate.VisibilityScope), string(models.VisibilityMember))
	}
	roleLevelBySpaceID := map[string]int{}
	if !isSingleSpaceSearch && strings.TrimSpace(request.ActorUserID) != "" {
		var spaceIDs []string
		for _, candidate := range candidates {
			if _, visible := visibleIDs[candidate.DocID]; visible && needsHigherRole(candidate) {
				spaceIDs = append(spaceIDs, candidate.SpaceID)
			}
		}
		if len(spaceIDs) > 0 {
			roleLevelBySpaceID, err = p.visibilityRepo.ResolveUserRoleLevelsBySpaces(ctx, request.ActorUserID, spaceIDs)
			if err != nil {
				return nil, err
			}
		}
	}

	filtered := make([]SearchHit, 0, len(visibleIDs))
	for _, candidate := range candidates {
		if _, visible := visibleIDs[candidate.DocID]; !visible {
			continue
		}
		if needsHigherRole(candidate) {
			roleLevel := request.UserRoleLevel
			if !isSingleSpaceSearch {
				roleLevel = roleLevelBySpaceID[candidate.SpaceID]
			}
			if roleLevel < candidate.MinRole {
				continue
			}
		}
		filtered = append(filtered, SearchHit{
			DocID:     candidate.DocID,
			Score:     candidate.Score,
			Snippet:   candidate.Snippet,
			UpdatedAt: candidate.UpdatedAt,
		})
	}
	return filtered, nil
}

func (p *BleveProvider) Capabilities() Capabilities {
	return Capabilities{
		SupportsHighlight:      false,
		SupportsSortUpdatedAt:  true,
		SupportsTypo:           false,
		SupportsFacets:         false,
		SupportsCustomAnalyzer: true,
	}
}

// Reset 清空 Bleve 索引并重新创建索引结构。
func (p *BleveProvider) Reset(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil {
		return errors.New("bleve search provider is nil")
	}
	if strings.TrimSpace(p.indexPath) == "" {
		return errors.New("bleve index path is empty")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.index != nil {
		_ = p.index.Close()
		p.index = nil
	}
	if err := os.RemoveAll(p.indexPath); err != nil {
		return err
	}
	return p.createIndexLocked()
}

// DocCount 返回当前索引文档数量。
func (p *BleveProvider) DocCount(ctx context.Context) (uint64, error) {
	if err := p.Health(ctx); err != nil {
		return 0, err
	}
	indexInstance, err := p.ensureIndex(ctx)
	if err != nil {
		return 0, err
	}
	return indexInstance.DocCount()
}

func (p *BleveProvider) ensureIndex(ctx context.Context) (bleve.Index, error) {
	if p == nil {
		return nil, errors.New("bleve search provider is nil")
	}
	if strings.TrimSpace(p.indexPath) == "" {
		return nil, errors.New("bleve index path is empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.mu.RLock()
	current := p.index
	p.mu.RUnlock()
	if current != nil {
		return current, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.index != nil {
		return p.index, nil
	}

	indexPath := filepath.Clean(p.indexPath)
	_, statErr := os.Stat(indexPath)
	switch {
	case statErr == nil:
		opened, err := bleve.Open(indexPath)
		if err != nil {
			return nil, err
		}
		p.index = opened
		return opened, nil
	case errors.Is(statErr, os.ErrNotExist):
		if err := p.createIndexLocked(); err != nil {
			return nil, err
		}
		return p.index, nil
	default:
		return nil, statErr
	}
}

func (p *BleveProvider) createIndexLocked() error {
	if p == nil {
		return errors.New("bleve search provider is nil")
	}
	if strings.TrimSpace(p.indexPath) == "" {
		return errors.New("bleve index path is empty")
	}

	indexPath := filepath.Clean(p.indexPath)
	parentDir := filepath.Dir(indexPath)
	if parentDir != "" && parentDir != "." {
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return err
		}
	}

	created, err := bleve.New(indexPath, buildBleveIndexMapping())
	if err != nil {
		return err
	}
	p.index = created
	return nil
}

func (p *BleveProvider) searchCandidates(
	ctx context.Context,
	indexInstance bleve.Index,
	searchQuery query.Query,
	sortMode SortMode,
	snippetTerms []string,
) ([]bleveSearchCandidate, error) {
	candidates := make([]bleveSearchCandidate, 0, 256)
	seen := make(map[string]struct{}, 256)

	for from := 0; from < maxBleveSearchCandidateCount; from += defaultBleveSearchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		searchRequest := bleve.NewSearchRequestOptions(
			searchQuery,
			defaultBleveSearchChunkSize,
			from,
			false,
		)
		searchRequest.Fields = []string{
			"doc_id",
			"space_id",
			"title",
			"body_plain",
			"updated_at_unix",
			"visibility_scope",
			"min_role",
		}
		if sortMode == SortModeUpdatedAtDesc {
			searchRequest.SortBy([]string{"-updated_at_unix"})
		}

		searchResult, err := indexInstance.Search(searchRequest)
		if err != nil {
			return nil, err
		}
		if len(searchResult.Hits) == 0 {
			break
		}

		for _, hit := range searchResult.Hits {
			docID := strings.TrimSpace(hit.ID)
			if docID == "" {
				continue
			}
			if _, exists := seen[docID]; exists {
				continue
			}
			seen[docID] = struct{}{}

			title := extractStringField(hit.Fields, "title")
			bodyPlain := extractStringField(hit.Fields, "body_plain")
			spaceID := extractStringField(hit.Fields, "space_id")
			updatedAtUnix := extractInt64Field(hit.Fields, "updated_at_unix")
			visibilityScope := extractStringField(hit.Fields, "visibility_scope")
			minRole := int(extractInt64Field(hit.Fields, "min_role"))
			if minRole < 1 {
				minRole = 1
			}
			candidates = append(candidates, bleveSearchCandidate{
				DocID:           docID,
				SpaceID:         spaceID,
				Score:           hit.Score,
				Snippet:         buildBleveSearchSnippet(title, bodyPlain, snippetTerms),
				UpdatedAt:       time.Unix(updatedAtUnix, 0).UTC(),
				VisibilityScope: visibilityScope,
				MinRole:         minRole,
			})
		}

		if len(searchResult.Hits) < defaultBleveSearchChunkSize {
			break
		}
	}

	return candidates, nil
}

func (p *BleveProvider) searchDocumentIDs(
	ctx context.Context,
	indexInstance bleve.Index,
	searchQuery query.Query,
) ([]string, error) {
	result := make([]string, 0, 256)
	seen := make(map[string]struct{}, 256)

	for from := 0; from < maxBleveSearchCandidateCount; from += defaultBleveSearchChunkSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		searchRequest := bleve.NewSearchRequestOptions(
			searchQuery,
			defaultBleveSearchChunkSize,
			from,
			false,
		)
		searchResult, err := indexInstance.Search(searchRequest)
		if err != nil {
			return nil, err
		}
		if len(searchResult.Hits) == 0 {
			break
		}

		for _, hit := range searchResult.Hits {
			docID := strings.TrimSpace(hit.ID)
			if docID == "" {
				continue
			}
			if _, exists := seen[docID]; exists {
				continue
			}
			seen[docID] = struct{}{}
			result = append(result, docID)
		}

		if len(searchResult.Hits) < defaultBleveSearchChunkSize {
			break
		}
	}
	return result, nil
}

func (p *BleveProvider) filterVisibleDocIDs(
	ctx context.Context,
	request SearchRequest,
	documentIDs []string,
) (map[string]struct{}, error) {
	if p == nil || p.visibilityRepo == nil {
		return nil, errors.New("bleve search provider visibility repository is nil")
	}
	if len(documentIDs) == 0 {
		return map[string]struct{}{}, nil
	}

	visibleDocumentIDs, err := p.visibilityRepo.FilterVisibleDocumentIDsByCandidates(
		ctx,
		repository.SearchVisibleDocumentIDsByCandidatesParams{
			ActorUserID:           strings.TrimSpace(request.ActorUserID),
			SpaceID:               strings.TrimSpace(request.SpaceID),
			ScopeSpaceIDs:         request.ScopeSpaceIDs,
			CanViewAllDocuments:   request.CanViewAllDocuments,
			IncludeRenderedOffice: true,
			CandidateDocumentIDs:  documentIDs,
		},
	)
	if err != nil {
		return nil, err
	}

	visible := make(map[string]struct{}, len(visibleDocumentIDs))
	for _, item := range visibleDocumentIDs {
		docID := strings.TrimSpace(item)
		if docID == "" {
			continue
		}
		visible[docID] = struct{}{}
	}
	return visible, nil
}

func normalizeBlevePagination(page int, pageSize int) (int, int) {
	normalizedPage := page
	if normalizedPage <= 0 {
		normalizedPage = defaultBleveSearchPage
	}

	normalizedPageSize := pageSize
	if normalizedPageSize <= 0 {
		normalizedPageSize = defaultBleveSearchPageSize
	}
	if normalizedPageSize > maxBleveSearchPageSize {
		normalizedPageSize = maxBleveSearchPageSize
	}
	return normalizedPage, normalizedPageSize
}

func buildBleveIndexMapping() *mapping.IndexMappingImpl {
	indexMapping := bleve.NewIndexMapping()
	documentMapping := bleve.NewDocumentMapping()

	keywordField := func(store bool) *mapping.FieldMapping {
		mapping := bleve.NewTextFieldMapping()
		mapping.Analyzer = keyword.Name
		mapping.Store = store
		mapping.IncludeInAll = false
		return mapping
	}

	documentMapping.AddFieldMappingsAt("doc_id", keywordField(true))
	documentMapping.AddFieldMappingsAt("space_id", keywordField(true))
	documentMapping.AddFieldMappingsAt("node_id", keywordField(false))
	documentMapping.AddFieldMappingsAt("visibility_scope", keywordField(true))
	documentMapping.AddFieldMappingsAt("space_status", keywordField(false))
	documentMapping.AddFieldMappingsAt("doc_status", keywordField(false))
	documentMapping.AddFieldMappingsAt("analyzer_name", keywordField(false))
	documentMapping.AddFieldMappingsAt("analyzer_version", keywordField(false))
	documentMapping.AddFieldMappingsAt("terms", keywordField(false))
	documentMapping.AddFieldMappingsAt("title_terms", keywordField(false))

	titleMapping := bleve.NewTextFieldMapping()
	titleMapping.Store = true
	documentMapping.AddFieldMappingsAt("title", titleMapping)

	bodyPlainMapping := bleve.NewTextFieldMapping()
	bodyPlainMapping.Store = true
	documentMapping.AddFieldMappingsAt("body_plain", bodyPlainMapping)

	minRoleMapping := bleve.NewNumericFieldMapping()
	minRoleMapping.Store = true
	documentMapping.AddFieldMappingsAt("min_role", minRoleMapping)

	updatedAtMapping := bleve.NewNumericFieldMapping()
	updatedAtMapping.Store = true
	documentMapping.AddFieldMappingsAt("updated_at_unix", updatedAtMapping)

	isDeletedMapping := bleve.NewBooleanFieldMapping()
	isDeletedMapping.Store = false
	documentMapping.AddFieldMappingsAt("is_deleted", isDeletedMapping)

	indexMapping.DefaultMapping = documentMapping
	return indexMapping
}

func buildBleveSearchQuery(request SearchRequest) query.Query {
	queryTerms := buildSearchQueryTermsWithRaw(request.Query, request.RawQuery)
	boostedQueryTerms := extractCompoundLiteralTokens(request.RawQuery)
	boostedSet := make(map[string]struct{}, len(boostedQueryTerms))
	for _, item := range boostedQueryTerms {
		boostedSet[item] = struct{}{}
	}

	tokenQueries := make([]query.Query, 0, len(queryTerms))
	for _, token := range queryTerms {
		termsQuery := bleve.NewTermQuery(token)
		termsQuery.SetField("terms")
		titleTermsQuery := bleve.NewTermQuery(token)
		titleTermsQuery.SetField("title_terms")

		if _, exists := boostedSet[token]; exists {
			termsQuery.SetBoost(3.0)
			titleTermsQuery.SetBoost(4.0)
		}
		tokenQueries = append(tokenQueries, bleve.NewDisjunctionQuery(termsQuery, titleTermsQuery))
	}
	if len(tokenQueries) == 0 {
		return bleve.NewMatchNoneQuery()
	}

	filterQueries := make([]query.Query, 0, 4)
	spaceStatusQuery := bleve.NewTermQuery(string(models.EntityStatusActive))
	spaceStatusQuery.SetField("space_status")
	docStatusQuery := bleve.NewTermQuery(string(models.EntityStatusActive))
	docStatusQuery.SetField("doc_status")
	isDeletedQuery := bleve.NewBoolFieldQuery(false)
	isDeletedQuery.SetField("is_deleted")
	filterQueries = append(filterQueries, spaceStatusQuery, docStatusQuery, isDeletedQuery)

	spaceID := strings.TrimSpace(request.SpaceID)
	if spaceID != "" {
		spaceQuery := bleve.NewTermQuery(spaceID)
		spaceQuery.SetField("space_id")
		filterQueries = append(filterQueries, spaceQuery)
	} else {
		scopeSpaceIDs := normalizeBleveScopeSpaceIDs(request.ScopeSpaceIDs)
		if len(scopeSpaceIDs) > 0 && len(scopeSpaceIDs) <= maxBleveSearchScopeFilterIDs {
			spaceQueries := make([]query.Query, 0, len(scopeSpaceIDs))
			for _, item := range scopeSpaceIDs {
				spaceQuery := bleve.NewTermQuery(item)
				spaceQuery.SetField("space_id")
				spaceQueries = append(spaceQueries, spaceQuery)
			}
			filterQueries = append(filterQueries, bleve.NewDisjunctionQuery(spaceQueries...))
		}
	}

	tokenRecallQuery := bleve.NewDisjunctionQuery(tokenQueries...)
	tokenRecallQuery.SetMin(float64(resolveTokenMinShouldMatch(len(tokenQueries))))
	filterQueries = append(filterQueries, tokenRecallQuery)
	return bleve.NewConjunctionQuery(filterQueries...)
}

func splitTerms(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	fields := strings.Fields(strings.ToLower(value))
	if len(fields) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, item := range fields {
		term := strings.TrimSpace(item)
		if term == "" {
			continue
		}
		if _, exists := seen[term]; exists {
			continue
		}
		seen[term] = struct{}{}
		result = append(result, term)
	}
	return result
}

func normalizeBleveScopeSpaceIDs(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	result := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		spaceID := strings.TrimSpace(item)
		if spaceID == "" {
			continue
		}
		if _, exists := seen[spaceID]; exists {
			continue
		}
		seen[spaceID] = struct{}{}
		result = append(result, spaceID)
	}
	return result
}

func buildBleveSearchSnippet(title string, bodyPlain string, queryTerms []string) string {
	return buildKeywordWindowSnippetFromTitleAndBody(title, bodyPlain, queryTerms)
}

func extractStringField(fields map[string]any, key string) string {
	if len(fields) == 0 {
		return ""
	}
	rawValue, exists := fields[key]
	if !exists {
		return ""
	}
	switch current := rawValue.(type) {
	case string:
		return strings.TrimSpace(current)
	case []byte:
		return strings.TrimSpace(string(current))
	default:
		return strings.TrimSpace(fmt.Sprint(current))
	}
}

func extractInt64Field(fields map[string]any, key string) int64 {
	if len(fields) == 0 {
		return 0
	}
	rawValue, exists := fields[key]
	if !exists {
		return 0
	}
	switch current := rawValue.(type) {
	case int:
		return int64(current)
	case int64:
		return current
	case float64:
		return int64(current)
	case float32:
		return int64(current)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(current), 10, 64)
		if err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}

type bleveIndexDocument struct {
	DocID           string   `json:"doc_id"`
	SpaceID         string   `json:"space_id"`
	NodeID          string   `json:"node_id"`
	Title           string   `json:"title"`
	BodyPlain       string   `json:"body_plain"`
	Terms           []string `json:"terms"`
	TitleTerms      []string `json:"title_terms"`
	VisibilityScope string   `json:"visibility_scope"`
	MinRole         int      `json:"min_role"`
	UpdatedAtUnix   int64    `json:"updated_at_unix"`
	IsDeleted       bool     `json:"is_deleted"`
	SpaceStatus     string   `json:"space_status"`
	DocStatus       string   `json:"doc_status"`
	AnalyzerName    string   `json:"analyzer_name"`
	AnalyzerVersion string   `json:"analyzer_version"`
}

type bleveSearchCandidate struct {
	DocID           string
	SpaceID         string
	Score           float64
	Snippet         string
	UpdatedAt       time.Time
	VisibilityScope string
	MinRole         int
}
