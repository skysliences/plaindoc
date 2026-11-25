package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// ParsePasskeyOrigin validates the configured public browser origin and returns its canonical form.
func ParsePasskeyOrigin(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	invalid := errors.New("AUTH_PASSKEY_ORIGIN must be an HTTPS domain origin or an HTTP localhost origin, without a path, query or fragment")
	origin, err := url.Parse(value)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" ||
		origin.ForceQuery || strings.Contains(value, "#") || (origin.Path != "" && origin.Path != "/") {
		return nil, invalid
	}
	host, err := idna.Lookup.ToASCII(strings.ToLower(origin.Hostname()))
	if err != nil || host == "" || strings.HasSuffix(host, ".") || net.ParseIP(host) != nil || strings.HasSuffix(origin.Host, ":") {
		return nil, invalid
	}
	if origin.Scheme != "https" && (origin.Scheme != "http" || host != "localhost") {
		return nil, invalid
	}
	port := origin.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, invalid
		}
		port = strconv.Itoa(number)
	}
	if origin.Scheme == "https" && port == "443" || origin.Scheme == "http" && port == "80" {
		port = ""
	}
	address := host
	if port != "" {
		address = net.JoinHostPort(host, port)
	}
	return &url.URL{Scheme: origin.Scheme, Host: address}, nil
}
