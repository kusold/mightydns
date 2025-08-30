package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"

	"github.com/miekg/dns"

	"github.com/kusold/mightydns"
)

func init() {
	mightydns.RegisterModule(&SplitHorizonResolver{})
}

type SplitHorizonResolver struct {
	ClientGroups  map[string]*ClientGroup `json:"client_groups,omitempty"`
	Policies      []*Policy               `json:"policies,omitempty"`
	DefaultPolicy *Policy                 `json:"default_policy,omitempty"`

	// Internal fields
	compiledGroups map[string]*compiledClientGroup
	logger         *slog.Logger
	ctx            mightydns.Context
}

type ClientGroup struct {
	Sources  []string `json:"sources,omitempty"`
	Priority int      `json:"priority,omitempty"`
}

type Policy struct {
	Match    *PolicyMatch    `json:"match,omitempty"`
	Upstream json.RawMessage `json:"upstream,omitempty"`

	// Internal fields
	handler mightydns.DNSHandler
}

type PolicyMatch struct {
	ClientGroup string `json:"client_group,omitempty"`
}

// compiledClientGroup holds the parsed and compiled CIDR blocks for efficient matching
type compiledClientGroup struct {
	name     string
	priority int
	networks []*net.IPNet
	ips      []net.IP
}

func (SplitHorizonResolver) MightyModule() mightydns.ModuleInfo {
	return mightydns.ModuleInfo{
		ID:  "dns.resolver.split_horizon",
		New: func() mightydns.Module { return new(SplitHorizonResolver) },
	}
}

func (s *SplitHorizonResolver) Provision(ctx mightydns.Context) error {
	s.ctx = ctx
	s.logger = ctx.Logger().With("module", "dns.resolver.split_horizon")
	s.compiledGroups = make(map[string]*compiledClientGroup)

	// Validate and compile client groups
	if err := s.compileClientGroups(); err != nil {
		return fmt.Errorf("compiling client groups: %w", err)
	}

	// Provision upstream handlers for policies
	if err := s.provisionPolicies(); err != nil {
		return fmt.Errorf("provisioning policies: %w", err)
	}

	// Provision default policy if specified
	if s.DefaultPolicy != nil {
		if err := s.provisionPolicy(s.DefaultPolicy, "default"); err != nil {
			return fmt.Errorf("provisioning default policy: %w", err)
		}
	}

	s.logger.Info("split-horizon resolver provisioned",
		"client_groups", len(s.ClientGroups),
		"policies", len(s.Policies),
		"has_default_policy", s.DefaultPolicy != nil)

	return nil
}

func (s *SplitHorizonResolver) compileClientGroups() error {
	if len(s.ClientGroups) == 0 {
		return fmt.Errorf("no client groups defined")
	}

	for name, group := range s.ClientGroups {
		compiled := &compiledClientGroup{
			name:     name,
			priority: group.Priority,
			networks: make([]*net.IPNet, 0),
			ips:      make([]net.IP, 0),
		}

		for _, source := range group.Sources {
			if err := s.parseSource(source, compiled); err != nil {
				return fmt.Errorf("parsing source %s in group %s: %w", source, name, err)
			}
		}

		s.compiledGroups[name] = compiled
		s.logger.Debug("compiled client group",
			"name", name,
			"priority", group.Priority,
			"networks", len(compiled.networks),
			"individual_ips", len(compiled.ips))
	}

	return nil
}

func (s *SplitHorizonResolver) parseSource(source string, compiled *compiledClientGroup) error {
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

func (s *SplitHorizonResolver) provisionPolicies() error {
	if len(s.Policies) == 0 {
		return fmt.Errorf("no policies defined")
	}

	for i, policy := range s.Policies {
		if err := s.provisionPolicy(policy, fmt.Sprintf("policy_%d", i)); err != nil {
			return fmt.Errorf("provisioning policy %d: %w", i, err)
		}
	}

	return nil
}

func (s *SplitHorizonResolver) provisionPolicy(policy *Policy, name string) error {
	// Default policy doesn't need a match condition
	if name != "default" {
		if policy.Match == nil || policy.Match.ClientGroup == "" {
			return fmt.Errorf("policy %s must specify a client_group to match", name)
		}

		// Validate that the referenced client group exists
		if _, exists := s.ClientGroups[policy.Match.ClientGroup]; !exists {
			return fmt.Errorf("policy %s references unknown client group: %s", name, policy.Match.ClientGroup)
		}
	}

	if len(policy.Upstream) == 0 {
		return fmt.Errorf("policy %s must specify an upstream configuration", name)
	}

	// Parse and provision the upstream handler
	var upstreamConfig map[string]interface{}
	if err := json.Unmarshal(policy.Upstream, &upstreamConfig); err != nil {
		return fmt.Errorf("parsing upstream config for policy %s: %w", name, err)
	}

	handlerType, exists := upstreamConfig["handler"].(string)
	if !exists {
		return fmt.Errorf("upstream config for policy %s must specify a 'handler' field", name)
	}

	// Load the upstream module
	handlerModule, err := mightydns.LoadModule(s.ctx, upstreamConfig, "upstream", handlerType)
	if err != nil {
		return fmt.Errorf("loading upstream handler %s for policy %s: %w", handlerType, name, err)
	}

	// Ensure it implements DNSHandler
	handler, ok := handlerModule.(mightydns.DNSHandler)
	if !ok {
		return fmt.Errorf("upstream handler %s for policy %s does not implement DNSHandler", handlerType, name)
	}

	policy.handler = handler

	clientGroup := "none"
	if policy.Match != nil {
		clientGroup = policy.Match.ClientGroup
	}

	s.logger.Debug("provisioned policy",
		"name", name,
		"client_group", clientGroup,
		"handler_type", handlerType)

	return nil
}

func (s *SplitHorizonResolver) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error {
	// Extract query details for logging
	var qname, qtype string
	if len(r.Question) > 0 {
		qname = r.Question[0].Name
		qtype = dns.TypeToString[r.Question[0].Qtype]
	}

	// Extract client IP
	clientIP := s.getClientIP(w)
	clientIPStr := clientIP.String()

	s.logger.Debug("processing DNS query",
		"query_id", r.Id,
		"query_name", qname,
		"query_type", qtype,
		"client_ip", clientIPStr)

	// Match client to a group
	matchedGroup := s.matchClientGroup(clientIP)

	// Find the corresponding policy
	var selectedPolicy *Policy
	var policyName string

	if matchedGroup != "" {
		for i, policy := range s.Policies {
			if policy.Match != nil && policy.Match.ClientGroup == matchedGroup {
				selectedPolicy = policy
				policyName = fmt.Sprintf("policy_%d_%s", i, matchedGroup)
				break
			}
		}
	}

	// Fall back to default policy if no match
	if selectedPolicy == nil {
		selectedPolicy = s.DefaultPolicy
		policyName = "default"
		s.logger.Debug("using default policy",
			"query_id", r.Id,
			"client_ip", clientIPStr,
			"matched_group", matchedGroup)
	} else {
		s.logger.Debug("matched client to policy",
			"query_id", r.Id,
			"client_ip", clientIPStr,
			"matched_group", matchedGroup,
			"policy", policyName)
	}

	// If still no policy, return server failure
	if selectedPolicy == nil || selectedPolicy.handler == nil {
		s.logger.Error("no policy available for client",
			"query_id", r.Id,
			"client_ip", clientIPStr,
			"matched_group", matchedGroup)

		m := new(dns.Msg)
		m.SetReply(r)
		m.SetRcode(r, dns.RcodeServerFailure)
		return w.WriteMsg(m)
	}

	// Route to the selected upstream handler
	s.logger.Debug("routing to upstream handler",
		"query_id", r.Id,
		"client_ip", clientIPStr,
		"policy", policyName)

	return selectedPolicy.handler.ServeDNS(ctx, w, r)
}

func (s *SplitHorizonResolver) getClientIP(w dns.ResponseWriter) net.IP {
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
			s.logger.Warn("failed to parse client address", "addr", remoteAddr.String(), "error", err)
			return nil
		}

		ip := net.ParseIP(host)
		if ip == nil {
			s.logger.Warn("failed to parse client IP", "host", host)
		}
		return ip
	}
}

func (s *SplitHorizonResolver) matchClientGroup(clientIP net.IP) string {
	if clientIP == nil {
		return ""
	}

	// Create a list of all groups sorted by priority
	var groups []*compiledClientGroup
	for _, group := range s.compiledGroups {
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
				s.logger.Debug("client IP matched individual IP",
					"client_ip", clientIP.String(),
					"matched_ip", ip.String(),
					"group", group.name)
				return group.name
			}
		}

		// Check CIDR networks
		for _, network := range group.networks {
			if network.Contains(clientIP) {
				s.logger.Debug("client IP matched CIDR block",
					"client_ip", clientIP.String(),
					"network", network.String(),
					"group", group.name)
				return group.name
			}
		}
	}

	s.logger.Debug("client IP did not match any group", "client_ip", clientIP.String())
	return ""
}

func (s *SplitHorizonResolver) Cleanup() error {
	s.logger.Debug("cleaning up split-horizon resolver")

	// Cleanup all policy handlers
	for i, policy := range s.Policies {
		if policy.handler != nil {
			if cleaner, ok := policy.handler.(mightydns.CleanerUpper); ok {
				if err := cleaner.Cleanup(); err != nil {
					s.logger.Error("error cleaning up policy handler",
						"policy", i,
						"error", err)
				}
			}
		}
	}

	// Cleanup default policy handler
	if s.DefaultPolicy != nil && s.DefaultPolicy.handler != nil {
		if cleaner, ok := s.DefaultPolicy.handler.(mightydns.CleanerUpper); ok {
			if err := cleaner.Cleanup(); err != nil {
				s.logger.Error("error cleaning up default policy handler", "error", err)
			}
		}
	}

	return nil
}
