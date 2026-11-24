package service

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestLoginIPAddressesNormalizeAndEnforce(t *testing.T) {
	addresses, err := normalizeLoginIPAddresses([]string{" 2001:0db8::1 ", "::ffff:192.0.2.10", "192.0.2.10"})
	if err != nil || !slices.Equal(addresses, []string{"192.0.2.10", "2001:db8::1"}) {
		t.Fatalf("normalized addresses are %v, err=%v", addresses, err)
	}
	for _, value := range []string{"", "192.0.2.10/32", "192.0.2.10:80", "fe80::1%eth0", "example.com"} {
		if _, err := normalizeLoginIPAddresses([]string{value}); err == nil {
			t.Fatalf("invalid IP was accepted: %q", value)
		}
	}

	ctx := context.Background()
	restricted := &models.User{LoginIPAddressesJSON: `["192.0.2.10","2001:db8::1"]`}
	for _, clientIP := range []string{"192.0.2.10", "::ffff:192.0.2.10", "2001:0db8:0:0::1"} {
		if err := ensureAuthUserLoginIPAllowed(WithAuthClientIP(ctx, clientIP), restricted); err != nil {
			t.Fatalf("expected %q to be allowed, err=%v", clientIP, err)
		}
	}
	for _, clientIP := range []string{"", "192.0.2.11", "malformed"} {
		if err := ensureAuthUserLoginIPAllowed(WithAuthClientIP(ctx, clientIP), restricted); !errors.Is(err, ErrLoginIPDenied) {
			t.Fatalf("expected %q to be denied, err=%v", clientIP, err)
		}
	}
	if err := ensureAuthUserLoginIPAllowed(ctx, &models.User{LoginIPAddressesJSON: `[]`}); err != nil {
		t.Fatalf("empty list should allow login, err=%v", err)
	}
	for _, raw := range []string{`null`, `["invalid"]`, `invalid`} {
		user := &models.User{LoginIPAddressesJSON: raw}
		if err := ensureAuthUserLoginIPAllowed(WithAuthClientIP(ctx, "192.0.2.10"), user); !errors.Is(err, ErrLoginIPDenied) {
			t.Fatalf("corrupt stored list should fail closed, err=%v", err)
		}
	}
}
