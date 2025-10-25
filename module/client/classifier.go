package client

import (
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"

	"github.com/miekg/dns"
)

// ClientGroup defines a group of clients by IP addresses and CIDR blocks
type ClientGroup struct {
	Sources  []string `json:"sources,omitempty"`
	Priority int      `json:"priority,omitempty"`
}

// compiledClientGroup holds the parsed and compiled CIDR blocks for efficient matching
type compiledClientGroup struct {
	name     string
	priority int
	networks []*net.IPNet
	ips      []net.IP
}

// ClientClassifier provides client IP classification based on configured groups
type ClientClassifier struct {
	Groups map[string]*ClientGroup `json:"client_groups,omitempty"`

	compiled map[string]*compiledClientGroup
	logger   *slog.Logger
}

// NewClientClassifier creates a new client classifier with the given groups
func NewClientClassifier(groups map[string]*ClientGroup, logger *slog.Logger) *ClientClassifier {
	return &ClientClassifier{
		Groups:   groups,
		compiled: make(map[string]*compiledClientGroup),
		logger:   logger.With("component", "client_classifier"),
	}
}

// Provision compiles the client groups for efficient matching
func (c *ClientClassifier) Provision() error {
	if len(c.Groups) == 0 {
		return fmt.Errorf("no client groups defined")
	}

	for name, group := range c.Groups {
		compiled := &compiledClientGroup{
			name:     name,
			priority: group.Priority,
			networks: make([]*net.IPNet, 0),
			ips:      make([]net.IP, 0),
		}

		for _, source := range group.Sources {
			if err := c.parseSource(source, compiled); err != nil {
				return fmt.Errorf("parsing source %s in group %s: %w", source, name, err)
			}
		}

		c.compiled[name] = compiled
		c.logger.Debug("compiled client group",
			"name", name,
			"priority", group.Priority,
			"networks", len(compiled.networks),
			"individual_ips", len(compiled.ips))
	}

	return nil
}

// parseSource parses a source string as either a CIDR block or individual IP
func (c *ClientClassifier) parseSource(source string, compiled *compiledClientGroup) error {
	// Check if it's a CIDR block
	if strings.Contains(source, "/") {
		_, network, err := net.ParseCIDR(source)
		if err != nil {
			return fmt.Errorf("invalid CIDR block %s: %w", source, err)
		}
		compiled.networks = append(compiled.networks, network)
	} else {
		// It's an individual IP address
		ip := net.ParseIP(source)
		if ip == nil {
			return fmt.Errorf("invalid IP address: %s", source)
		}
		compiled.ips = append(compiled.ips, ip)
	}

	return nil
}

// ExtractClientIP extracts the client IP from a DNS response writer
func (c *ClientClassifier) ExtractClientIP(w dns.ResponseWriter) net.IP {
	remoteAddr := w.RemoteAddr()

	// Handle different address types
	switch addr := remoteAddr.(type) {
	case *net.UDPAddr:
		return addr.IP
	case *net.TCPAddr:
		return addr.IP
	default:
		// Fallback: parse the string representation
		host, _, err := net.SplitHostPort(remoteAddr.String())
		if err != nil {
			c.logger.Warn("failed to parse client address", "addr", remoteAddr.String(), "error", err)
			return nil
		}

		ip := net.ParseIP(host)
		if ip == nil {
			c.logger.Warn("failed to parse client IP", "host", host)
		}
		return ip
	}
}

// ClassifyIP classifies an IP address and returns the matching client group name
func (c *ClientClassifier) ClassifyIP(clientIP net.IP) string {
	if clientIP == nil {
		return ""
	}

	// Create a list of all groups sorted by priority
	var groups []*compiledClientGroup
	for _, group := range c.compiled {
		groups = append(groups, group)
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].priority < groups[j].priority
	})

	// Check each group in priority order
	for _, group := range groups {
		// Check individual IPs first (more specific)
		for _, ip := range group.ips {
			if clientIP.Equal(ip) {
				c.logger.Debug("client IP matched individual IP",
					"client_ip", clientIP.String(),
					"matched_ip", ip.String(),
					"group", group.name)
				return group.name
			}
		}

		// Check CIDR networks
		for _, network := range group.networks {
			if network.Contains(clientIP) {
				c.logger.Debug("client IP matched CIDR block",
					"client_ip", clientIP.String(),
					"network", network.String(),
					"group", group.name)
				return group.name
			}
		}
	}

	c.logger.Debug("client IP did not match any group", "client_ip", clientIP.String())
	return ""
}

// ClassifyDNSRequest extracts the client IP from a DNS request and classifies it
func (c *ClientClassifier) ClassifyDNSRequest(w dns.ResponseWriter) string {
	clientIP := c.ExtractClientIP(w)
	return c.ClassifyIP(clientIP)
}

// GetGroupNames returns a list of all configured group names
func (c *ClientClassifier) GetGroupNames() []string {
	names := make([]string, 0, len(c.Groups))
	for name := range c.Groups {
		names = append(names, name)
	}
	return names
}

// GetGroupPriority returns the priority of a group, or -1 if not found
func (c *ClientClassifier) GetGroupPriority(groupName string) int {
	if compiled, exists := c.compiled[groupName]; exists {
		return compiled.priority
	}
	return -1
}
