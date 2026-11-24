package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"strings"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

const maxLoginIPAddresses = 64

var (
	ErrLoginIPDenied           = errors.New("login IP is not allowed")
	errInvalidLoginIPAddresses = errors.New("invalid login IP addresses")
)

type authClientIPKey struct{}

// WithAuthClientIP 在可信 HTTP 边界记录已解析的来访 IP。
func WithAuthClientIP(ctx context.Context, clientIP string) context.Context {
	address, _ := netip.ParseAddr(strings.TrimSpace(clientIP))
	return context.WithValue(ctx, authClientIPKey{}, address.Unmap())
}

func normalizeLoginIPAddresses(values []string) ([]string, error) {
	if len(values) > maxLoginIPAddresses {
		return nil, errInvalidLoginIPAddresses
	}
	addresses := make([]string, 0, len(values))
	seen := make(map[netip.Addr]struct{}, len(values))
	for _, value := range values {
		address, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil || address.Zone() != "" {
			return nil, errInvalidLoginIPAddresses
		}
		address = address.Unmap()
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		addresses = append(addresses, address.String())
	}
	slices.Sort(addresses)
	return addresses, nil
}

func decodeLoginIPAddresses(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return []string{}, nil
	}
	var addresses []string
	if err := json.Unmarshal([]byte(value), &addresses); err != nil || addresses == nil {
		return nil, errInvalidLoginIPAddresses
	}
	return normalizeLoginIPAddresses(addresses)
}

func ensureAuthUserLoginIPAllowed(ctx context.Context, user *models.User) error {
	if user == nil {
		return ErrLoginIPDenied
	}
	addresses, err := decodeLoginIPAddresses(user.LoginIPAddressesJSON)
	if err != nil {
		return ErrLoginIPDenied
	}
	if len(addresses) == 0 {
		return nil
	}
	clientIP, _ := ctx.Value(authClientIPKey{}).(netip.Addr)
	if !clientIP.IsValid() || clientIP.Zone() != "" || !slices.Contains(addresses, clientIP.String()) {
		return ErrLoginIPDenied
	}
	return nil
}
