package service

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"strings"

	"usbridge-client/internal/models"
)

func normalizeAllowedRoutes(resp *models.WireGuardBootstrapResponse) []string {
	routes := make([]string, 0, len(resp.AllowedIPs)+1)
	seen := map[string]struct{}{}

	add := func(route string) {
		route = strings.TrimSpace(route)
		if route == "" {
			return
		}
		if _, ok := seen[route]; ok {
			return
		}
		seen[route] = struct{}{}
		routes = append(routes, route)
	}

	for _, allowedIP := range resp.AllowedIPs {
		add(allowedIP)
	}
	if len(routes) == 0 && strings.TrimSpace(resp.ServerAddress) != "" {
		add(strings.TrimSpace(resp.ServerAddress) + "/32")
	}

	return routes
}

func wireGuardKeyBase64ToHex(value string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("failed to decode WireGuard key: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func wireGuardIPCConfig(resp *models.WireGuardBootstrapResponse, privateKey string, listenPort int) (string, error) {
	privateKeyHex, err := wireGuardKeyBase64ToHex(privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to prepare client private key: %w", err)
	}
	serverKeyHex, err := wireGuardKeyBase64ToHex(resp.ServerPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to prepare server public key: %w", err)
	}

	var b strings.Builder
	b.WriteString("private_key=")
	b.WriteString(privateKeyHex)
	b.WriteByte('\n')
	if listenPort > 0 {
		b.WriteString("listen_port=")
		b.WriteString(fmt.Sprintf("%d", listenPort))
		b.WriteByte('\n')
	}
	b.WriteString("replace_peers=true\n")
	b.WriteString("public_key=")
	b.WriteString(serverKeyHex)
	b.WriteByte('\n')
	b.WriteString("endpoint=")
	b.WriteString(fmt.Sprintf("%s:%d", strings.TrimSpace(resp.ServerEndpointHost), resp.ServerEndpointPort))
	b.WriteByte('\n')
	if resp.PersistentKeepalive > 0 {
		b.WriteString("persistent_keepalive_interval=")
		b.WriteString(fmt.Sprintf("%d", resp.PersistentKeepalive))
		b.WriteByte('\n')
	}
	b.WriteString("replace_allowed_ips=true\n")
	allowedIPs := resp.AllowedIPs
	if len(allowedIPs) == 0 {
		allowedIPs = normalizeAllowedRoutes(resp)
	}
	for _, allowedIP := range allowedIPs {
		allowedIP = strings.TrimSpace(allowedIP)
		if allowedIP == "" {
			continue
		}
		b.WriteString("allowed_ip=")
		b.WriteString(allowedIP)
		b.WriteByte('\n')
	}

	return b.String(), nil
}

func parseCIDR(cidr string) (net.IP, *net.IPNet, error) {
	ip, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil, nil, err
	}
	ip = ip.To4()
	if ip == nil {
		return nil, nil, fmt.Errorf("only IPv4 WireGuard addresses are supported, got %q", cidr)
	}
	return ip, network, nil
}

func prefixLength(network *net.IPNet) int {
	if network == nil {
		return 0
	}
	ones, _ := network.Mask.Size()
	return ones
}
