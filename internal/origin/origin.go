// Package origin validates and normalizes public HTTP(S) origins shared by
// startup configuration and the administrator's transport settings.
package origin

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.ForceQuery || u.RawQuery != "" || strings.Contains(raw, "#") || (u.Path != "" && u.Path != "/") || u.RawPath != "" || strings.HasSuffix(u.Host, ":") {
		return "", errors.New("invalid HTTP(S) origin")
	}
	host := strings.ToLower(u.Hostname())
	for _, character := range host {
		if character <= 32 || character > 126 {
			return "", errors.New("origin hostname must use ASCII or Punycode")
		}
	}
	if strings.HasPrefix(u.Host, "[") {
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.Is6() || ip.Zone() != "" {
			return "", errors.New("invalid IPv6 origin")
		}
		host = ip.String()
		if ip.Is4In6() {
			bytes := ip.As16()
			host = fmt.Sprintf("::ffff:%x:%x", uint16(bytes[12])<<8|uint16(bytes[13]), uint16(bytes[14])<<8|uint16(bytes[15]))
		}
	} else if strings.Contains(host, ":") {
		return "", errors.New("IPv6 origin must use brackets")
	}
	port := u.Port()
	if port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return "", errors.New("origin port must be between 1 and 65535")
		}
		port = strconv.FormatUint(number, 10)
		if u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443" {
			port = ""
		}
	}
	u.Host = host
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	u.Path = ""
	return u.String(), nil
}
