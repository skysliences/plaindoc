package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

const DocumentUnlockCookieTTL = time.Hour

var (
	ErrDocumentPasswordRequired    = errors.New("document password required")
	ErrDocumentPasswordInvalid     = errors.New("invalid document password")
	ErrDocumentPasswordLength      = errors.New("document password outside allowed length")
	ErrDocumentPasswordRateLimited = errors.New("too many document password attempts")
)

type DocumentPasswordStatus struct {
	Locked    bool `json:"locked"`
	CanManage bool `json:"canManage"`
}

type DocumentPasswordService struct {
	repo       repository.DocumentPasswordRepository
	visibility *VisibilityService
	signingKey [32]byte
	attempts   *documentPasswordAttempts
}

func NewDocumentPasswordService(repo repository.DocumentPasswordRepository, visibility *VisibilityService, secret string) *DocumentPasswordService {
	return &DocumentPasswordService{
		repo: repo, visibility: visibility,
		signingKey: sha256.Sum256([]byte("bugodoc:document-unlock:v1:" + secret)),
		attempts: &documentPasswordAttempts{
			global: rate.NewLimiter(15, 15), entries: make(map[documentPasswordAttemptKey]documentPasswordAttempt),
		},
	}
}

func DocumentUnlockCookieName(documentID string) string {
	hash := sha256.Sum256([]byte(documentID))
	return "bugodoc_doc_unlock_" + hex.EncodeToString(hash[:16])
}

// Status returns lock metadata to an ordinary reader without requiring the additional password.
func (s *DocumentPasswordService) Status(ctx context.Context, documentID, viewerUserID string) (DocumentPasswordStatus, error) {
	if err := s.AuthorizeRead(ctx, documentID, viewerUserID); err != nil {
		return DocumentPasswordStatus{}, err
	}
	generation, err := s.Generation(ctx, documentID)
	if err != nil {
		return DocumentPasswordStatus{}, err
	}
	err = s.authorizeManagement(ctx, documentID, viewerUserID)
	if err != nil && !errors.Is(err, ErrViewerLoginRequired) && !errors.Is(err, ErrDocumentAccessDenied) {
		return DocumentPasswordStatus{}, err
	}
	return DocumentPasswordStatus{Locked: generation != "", CanManage: err == nil}, nil
}

// AuthorizeRead checks the usual document visibility, including imported records with no body preview.
func (s *DocumentPasswordService) AuthorizeRead(ctx context.Context, documentID, viewerUserID string) error {
	_, err := s.visibility.getDocument(ctx, documentID, viewerUserID, true)
	return err
}

func (s *DocumentPasswordService) authorizeManagement(ctx context.Context, documentID, userID string) error {
	if userID == "" {
		return ErrViewerLoginRequired
	}
	info, err := s.visibility.getActiveDocument(ctx, documentID)
	if err != nil {
		return err
	}
	ownerID := info.SpaceOwnerUserID
	if info.Document.CreatedByUserID != nil && strings.TrimSpace(*info.Document.CreatedByUserID) != "" {
		ownerID = *info.Document.CreatedByUserID
	}
	if ownerID != userID {
		return ErrDocumentAccessDenied
	}
	return s.visibility.authorizeDocumentWriteAccess(ctx, info, userID)
}

// Set changes or removes a password. Every successful set creates a new unlock generation.
func (s *DocumentPasswordService) Set(ctx context.Context, documentID, userID string, password *string) (DocumentPasswordStatus, error) {
	if err := s.authorizeManagement(ctx, documentID, userID); err != nil {
		return DocumentPasswordStatus{}, err
	}
	if password == nil {
		err := s.repo.Delete(ctx, documentID)
		return DocumentPasswordStatus{CanManage: err == nil}, err
	}
	if !validDocumentPassword(*password) {
		return DocumentPasswordStatus{}, ErrDocumentPasswordLength
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		return DocumentPasswordStatus{}, err
	}
	err = s.repo.Set(ctx, &models.DocumentPassword{
		DocumentID: documentID, PasswordHash: string(hash), Generation: ulid.Make().String(),
	})
	return DocumentPasswordStatus{Locked: err == nil, CanManage: err == nil}, err
}

func (s *DocumentPasswordService) Generation(ctx context.Context, documentID string) (string, error) {
	if s == nil {
		return "", nil
	}
	record, err := s.record(ctx, documentID)
	if err != nil || record == nil {
		return "", err
	}
	return record.Generation, nil
}

func (s *DocumentPasswordService) Check(ctx context.Context, documentID, viewerUserID, grant string) error {
	_, err := s.AuthorizedGeneration(ctx, documentID, viewerUserID, grant)
	return err
}

func (s *DocumentPasswordService) LockedDocumentIDsByLocalObjectKey(ctx context.Context, objectKey string) ([]string, error) {
	if s == nil {
		return nil, nil
	}
	return s.repo.LockedDocumentIDsByLocalObjectKey(ctx, objectKey)
}

// AuthorizedGeneration returns the generation checked against this grant, for downstream signed links.
func (s *DocumentPasswordService) AuthorizedGeneration(ctx context.Context, documentID, viewerUserID, grant string) (string, error) {
	generation, err := s.Generation(ctx, documentID)
	if err != nil || generation == "" {
		return generation, err
	}
	claims := &documentUnlockClaims{}
	_, err = jwt.ParseWithClaims(grant, claims, func(_ *jwt.Token) (any, error) {
		return s.signingKey[:], nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || claims.Subject != documentID || claims.Viewer != viewerUserID || claims.Generation != generation {
		return "", ErrDocumentPasswordRequired
	}
	return generation, nil
}

// Unlock verifies the additional password. The caller must already have checked ordinary or share access.
func (s *DocumentPasswordService) Unlock(ctx context.Context, documentID, viewerUserID, password, clientIP string) (string, error) {
	if s == nil {
		return "", nil
	}
	record, err := s.record(ctx, documentID)
	if err != nil || record == nil {
		return "", err
	}
	if !validDocumentPassword(password) {
		return "", ErrDocumentPasswordInvalid
	}
	now := time.Now()
	if !s.attempts.allow(documentID, clientIP, now) {
		return "", ErrDocumentPasswordRateLimited
	}
	if err := bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(password)); err != nil {
		return "", ErrDocumentPasswordInvalid
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, &documentUnlockClaims{
		Viewer: viewerUserID, Generation: record.Generation,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: documentID, ExpiresAt: jwt.NewNumericDate(now.Add(DocumentUnlockCookieTTL)),
		},
	}).SignedString(s.signingKey[:])
}

func (s *DocumentPasswordService) record(ctx context.Context, documentID string) (*models.DocumentPassword, error) {
	record, err := s.repo.Get(ctx, documentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return record, err
}

func validDocumentPassword(password string) bool {
	return len(password) >= 6 && len(password) <= 72 && utf8.ValidString(password) && strings.TrimSpace(password) != ""
}

type documentUnlockClaims struct {
	Viewer     string `json:"viewer,omitempty"`
	Generation string `json:"generation"`
	jwt.RegisteredClaims
}

type documentPasswordAttemptKey struct{ documentID, clientIP string }

type documentPasswordAttempt struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type documentPasswordAttempts struct {
	mu        sync.Mutex
	global    *rate.Limiter
	entries   map[documentPasswordAttemptKey]documentPasswordAttempt
	lastPrune time.Time
}

func (a *documentPasswordAttempts) allow(documentID, clientIP string, now time.Time) bool {
	if !a.global.AllowN(now, 1) {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Sub(a.lastPrune) >= time.Minute {
		for key, entry := range a.entries {
			if now.Sub(entry.lastSeen) >= 2*time.Minute {
				delete(a.entries, key)
			}
		}
		a.lastPrune = now
	}
	key := documentPasswordAttemptKey{documentID, clientIP}
	entry, exists := a.entries[key]
	if !exists {
		if len(a.entries) >= 4096 {
			return false
		}
		entry.limiter = rate.NewLimiter(rate.Every(12*time.Second), 5)
	}
	entry.lastSeen = now
	a.entries[key] = entry
	return entry.limiter.AllowN(now, 1)
}
