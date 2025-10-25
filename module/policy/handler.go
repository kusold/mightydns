package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/miekg/dns"

	"github.com/kusold/mightydns"
	"github.com/kusold/mightydns/module/client"
	"github.com/kusold/mightydns/module/dns/zone"
)

func init() {
	mightydns.RegisterModule(&PolicyHandler{})
}

// PolicyHandler provides client-based routing with selective override pattern
type PolicyHandler struct {
	BaseHandler  json.RawMessage                `json:"base_handler,omitempty"`
	ClientGroups map[string]*client.ClientGroup `json:"client_groups,omitempty"`
	Policies     []*PolicyOverride              `json:"policies,omitempty"`

	// Internal fields
	classifier  *client.ClientClassifier
	baseHandler mightydns.DNSHandler
	policyTrees map[string]mightydns.DNSHandler // client_group -> handler tree
	logger      *slog.Logger
	ctx         mightydns.Context
}

// PolicyOverride defines selective overrides for specific client groups
type PolicyOverride struct {
	Match     *PolicyMatch               `json:"match,omitempty"`
	Overrides map[string]json.RawMessage `json:"overrides,omitempty"`
}

// PolicyMatch defines the conditions for applying a policy
type PolicyMatch struct {
	ClientGroup string `json:"client_group,omitempty"`
}

func (PolicyHandler) MightyModule() mightydns.ModuleInfo {
	return mightydns.ModuleInfo{
		ID:  "policy",
		New: func() mightydns.Module { return new(PolicyHandler) },
	}
}

func (p *PolicyHandler) Provision(ctx mightydns.Context) error {
	p.ctx = ctx
	p.logger = ctx.Logger().With("module", "policy")
	p.policyTrees = make(map[string]mightydns.DNSHandler)

	// Enhanced configuration validation
	if err := p.validateConfiguration(); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Set up client classifier
	p.classifier = client.NewClientClassifier(p.ClientGroups, p.logger)
	if err := p.classifier.Provision(); err != nil {
		return fmt.Errorf("provisioning client classifier: %w", err)
	}

	// Provision the base handler
	baseHandler, err := p.provisionHandler(p.BaseHandler, "base")
	if err != nil {
		return fmt.Errorf("provisioning base handler: %w", err)
	}
	p.baseHandler = baseHandler

	// Validate and provision policy overrides
	if err := p.provisionPolicyOverrides(); err != nil {
		return fmt.Errorf("provisioning policy overrides: %w", err)
	}

	p.logger.Info("policy handler provisioned",
		"client_groups", len(p.ClientGroups),
		"policies", len(p.Policies),
		"policy_trees", len(p.policyTrees))

	return nil
}

// validateConfiguration performs comprehensive validation of the policy configuration
func (p *PolicyHandler) validateConfiguration() error {
	// Validate base handler is provided
	if len(p.BaseHandler) == 0 {
		return fmt.Errorf("base_handler is required")
	}

	// Validate base handler is valid JSON
	var baseConfig map[string]interface{}
	if err := json.Unmarshal(p.BaseHandler, &baseConfig); err != nil {
		return fmt.Errorf("base_handler must be valid JSON: %w", err)
	}

	// Validate base handler has required 'handler' field
	if _, exists := baseConfig["handler"]; !exists {
		return fmt.Errorf("base_handler must specify a 'handler' field")
	}

	// Validate client groups are provided and non-empty
	if len(p.ClientGroups) == 0 {
		return fmt.Errorf("client_groups are required")
	}

	// Validate each client group
	for groupName, group := range p.ClientGroups {
		if err := p.validateClientGroup(groupName, group); err != nil {
			return fmt.Errorf("invalid client group '%s': %w", groupName, err)
		}
	}

	// Validate policies
	groupNames := make(map[string]bool)
	for name := range p.ClientGroups {
		groupNames[name] = true
	}

	for i, policy := range p.Policies {
		if err := p.validatePolicy(policy, groupNames, i); err != nil {
			return fmt.Errorf("invalid policy at index %d: %w", i, err)
		}
	}

	// Validate that policies don't have conflicting priorities
	if err := p.validatePolicyPriorities(); err != nil {
		return fmt.Errorf("policy priority validation failed: %w", err)
	}

	return nil
}

// validateClientGroup validates a single client group configuration
func (p *PolicyHandler) validateClientGroup(_ string, group *client.ClientGroup) error {
	if group == nil {
		return fmt.Errorf("client group cannot be nil")
	}

	if len(group.Sources) == 0 {
		return fmt.Errorf("client group must have at least one source")
	}

	// Validate each source
	for i, source := range group.Sources {
		if err := p.validateSource(source); err != nil {
			return fmt.Errorf("invalid source at index %d (%s): %w", i, source, err)
		}
	}

	// Validate priority is non-negative
	if group.Priority < 0 {
		return fmt.Errorf("priority must be non-negative, got %d", group.Priority)
	}

	return nil
}

// validateSource validates a single IP source (IP address or CIDR block)
func (p *PolicyHandler) validateSource(source string) error {
	if source == "" {
		return fmt.Errorf("source cannot be empty")
	}

	// Check if it's a CIDR block
	if strings.Contains(source, "/") {
		_, _, err := net.ParseCIDR(source)
		if err != nil {
			return fmt.Errorf("invalid CIDR block: %w", err)
		}
	} else {
		// It's an individual IP address
		ip := net.ParseIP(source)
		if ip == nil {
			return fmt.Errorf("invalid IP address")
		}
	}

	return nil
}

// validatePolicy validates a single policy configuration
func (p *PolicyHandler) validatePolicy(policy *PolicyOverride, validGroups map[string]bool, _ int) error {
	if policy == nil {
		return fmt.Errorf("policy cannot be nil")
	}

	if policy.Match == nil {
		return fmt.Errorf("policy must have a match condition")
	}

	if policy.Match.ClientGroup == "" {
		return fmt.Errorf("policy must specify a client_group to match")
	}

	// Validate that the referenced client group exists
	if !validGroups[policy.Match.ClientGroup] {
		return fmt.Errorf("references unknown client group: %s", policy.Match.ClientGroup)
	}

	// Validate overrides if present
	for handlerType, override := range policy.Overrides {
		if handlerType == "" {
			return fmt.Errorf("override handler type cannot be empty")
		}

		if len(override) == 0 {
			return fmt.Errorf("override configuration for handler '%s' cannot be empty", handlerType)
		}

		// Validate that override is valid JSON
		var overrideConfig map[string]interface{}
		if err := json.Unmarshal(override, &overrideConfig); err != nil {
			return fmt.Errorf("override configuration for handler '%s' must be valid JSON: %w", handlerType, err)
		}
	}

	return nil
}

// validatePolicyPriorities ensures no conflicting group assignments
func (p *PolicyHandler) validatePolicyPriorities() error {
	groupsSeen := make(map[string]int)

	for i, policy := range p.Policies {
		groupName := policy.Match.ClientGroup
		if prevIndex, exists := groupsSeen[groupName]; exists {
			return fmt.Errorf("client group '%s' is used by multiple policies (indices %d and %d)", groupName, prevIndex, i)
		}
		groupsSeen[groupName] = i
	}

	return nil
}

func (p *PolicyHandler) provisionHandler(handlerConfig json.RawMessage, name string) (mightydns.DNSHandler, error) {
	var config map[string]interface{}
	if err := json.Unmarshal(handlerConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshaling handler config for %s: %w", name, err)
	}

	handlerType, exists := config["handler"].(string)
	if !exists {
		return nil, fmt.Errorf("handler config for %s must specify a 'handler' field", name)
	}

	// Load the handler module
	handlerModule, err := mightydns.LoadModule(p.ctx, config, name, handlerType)
	if err != nil {
		return nil, fmt.Errorf("loading handler %s for %s: %w", handlerType, name, err)
	}

	// Ensure it implements DNSHandler
	handler, ok := handlerModule.(mightydns.DNSHandler)
	if !ok {
		return nil, fmt.Errorf("handler %s for %s does not implement DNSHandler", handlerType, name)
	}

	return handler, nil
}

func (p *PolicyHandler) provisionPolicyOverrides() error {
	if len(p.Policies) == 0 {
		p.logger.Info("no policy overrides defined, using base handler for all clients")
		return nil
	}

	for i, policy := range p.Policies {
		if err := p.provisionPolicyOverride(policy, fmt.Sprintf("policy_%d", i)); err != nil {
			return fmt.Errorf("provisioning policy %d: %w", i, err)
		}
	}

	return nil
}

func (p *PolicyHandler) provisionPolicyOverride(policy *PolicyOverride, name string) error {
	if policy.Match == nil || policy.Match.ClientGroup == "" {
		return fmt.Errorf("policy %s must specify a client_group to match", name)
	}

	// Validate that the referenced client group exists
	if _, exists := p.ClientGroups[policy.Match.ClientGroup]; !exists {
		return fmt.Errorf("policy %s references unknown client group: %s", name, policy.Match.ClientGroup)
	}

	// If no overrides, use the base handler
	if len(policy.Overrides) == 0 {
		p.policyTrees[policy.Match.ClientGroup] = p.baseHandler
		p.logger.Debug("policy uses base handler (no overrides)",
			"policy", name,
			"client_group", policy.Match.ClientGroup)
		return nil
	}

	// Create a modified handler tree with selective overrides
	modifiedConfig, err := p.applyOverrides(p.BaseHandler, policy.Overrides)
	if err != nil {
		return fmt.Errorf("applying overrides for policy %s: %w", name, err)
	}

	// Provision the modified handler
	modifiedHandler, err := p.provisionHandler(modifiedConfig, fmt.Sprintf("%s_%s", name, policy.Match.ClientGroup))
	if err != nil {
		return fmt.Errorf("provisioning modified handler for policy %s: %w", name, err)
	}

	p.policyTrees[policy.Match.ClientGroup] = modifiedHandler

	p.logger.Debug("provisioned policy override",
		"policy", name,
		"client_group", policy.Match.ClientGroup,
		"overrides", len(policy.Overrides))

	return nil
}

func (p *PolicyHandler) applyOverrides(baseConfig json.RawMessage, overrides map[string]json.RawMessage) (json.RawMessage, error) {
	// Parse the base configuration
	var config map[string]interface{}
	if err := json.Unmarshal(baseConfig, &config); err != nil {
		return nil, fmt.Errorf("unmarshaling base config: %w", err)
	}

	// Apply overrides recursively
	modified := p.applyOverridesToConfig(config, overrides)

	// Marshal back to JSON
	result, err := json.Marshal(modified)
	if err != nil {
		return nil, fmt.Errorf("marshaling modified config: %w", err)
	}

	return result, nil
}

func (p *PolicyHandler) applyOverridesToConfig(config map[string]interface{}, overrides map[string]json.RawMessage) map[string]interface{} {
	// Create a deep copy of the config
	result := p.deepCopyMap(config)

	// Check if this is a handler that should be overridden
	if handlerType, exists := result["handler"].(string); exists {
		if override, hasOverride := overrides[handlerType]; hasOverride {
			// Parse the override
			var overrideConfig map[string]interface{}
			if err := json.Unmarshal(override, &overrideConfig); err != nil {
				p.logger.Warn("failed to parse override config", "handler", handlerType, "error", err)
				return result
			}

			// Merge override into the result
			for key, value := range overrideConfig {
				// Special handling for zones array - merge instead of replace
				if key == "zones" && handlerType == "dns.zone.manager" {
					result[key] = p.mergeZones(result[key], value)
				} else {
					result[key] = value
				}
			}

			p.logger.Debug("applied override",
				"handler", handlerType,
				"override_keys", len(overrideConfig))
		}
	}

	// Recursively apply to nested configurations
	for key, value := range result {
		switch v := value.(type) {
		case map[string]interface{}:
			result[key] = p.applyOverridesToConfig(v, overrides)
		case []interface{}:
			result[key] = p.applyOverridesToSlice(v, overrides)
		}
	}

	return result
}

func (p *PolicyHandler) applyOverridesToSlice(slice []interface{}, overrides map[string]json.RawMessage) []interface{} {
	result := make([]interface{}, len(slice))
	for i, item := range slice {
		switch v := item.(type) {
		case map[string]interface{}:
			result[i] = p.applyOverridesToConfig(v, overrides)
		default:
			result[i] = v
		}
	}
	return result
}

func (p *PolicyHandler) deepCopyMap(original map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{})
	for k, v := range original {
		copy[k] = p.deepCopyValue(v)
	}
	return copy
}

// mergeZones merges base zones with override zones, keeping all base zones and adding override zones
func (p *PolicyHandler) mergeZones(baseZones interface{}, overrideZones interface{}) interface{} {
	// Convert both to slices
	baseSlice, ok := baseZones.([]interface{})
	if !ok {
		p.logger.Warn("base zones is not a slice, using override zones only")
		return overrideZones
	}

	overrideSlice, ok := overrideZones.([]interface{})
	if !ok {
		p.logger.Warn("override zones is not a slice, using base zones only")
		return baseZones
	}

	// Create a map to track zones by their "zone" field to avoid duplicates
	zoneMap := make(map[string]interface{})

	// Add all base zones first
	for _, zone := range baseSlice {
		if zoneConfig, ok := zone.(map[string]interface{}); ok {
			if zoneName, exists := zoneConfig["zone"].(string); exists {
				zoneMap[zoneName] = zone
			}
		}
	}

	// Add override zones, which may replace base zones with same name
	for _, zone := range overrideSlice {
		if zoneConfig, ok := zone.(map[string]interface{}); ok {
			if zoneName, exists := zoneConfig["zone"].(string); exists {
				zoneMap[zoneName] = zone
			}
		}
	}

	// Convert back to slice
	result := make([]interface{}, 0, len(zoneMap))
	for _, zone := range zoneMap {
		result = append(result, zone)
	}

	p.logger.Debug("merged zones",
		"base_zones", len(baseSlice),
		"override_zones", len(overrideSlice),
		"merged_zones", len(result))

	return result
}

func (p *PolicyHandler) deepCopyValue(original interface{}) interface{} {
	switch v := original.(type) {
	case map[string]interface{}:
		return p.deepCopyMap(v)
	case []interface{}:
		copySlice := make([]interface{}, len(v))
		for i, item := range v {
			copySlice[i] = p.deepCopyValue(item)
		}
		return copySlice
	default:
		// For primitive types, assignment creates a copy
		return v
	}
}

func (p *PolicyHandler) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) error {
	// Extract query details for logging
	var qname, qtype string
	if len(r.Question) > 0 {
		qname = r.Question[0].Name
		qtype = dns.TypeToString[r.Question[0].Qtype]
	}

	// Classify the client
	clientGroup := p.classifier.ClassifyDNSRequest(w)
	clientIP := p.classifier.ExtractClientIP(w)
	clientIPStr := "unknown"
	if clientIP != nil {
		clientIPStr = clientIP.String()
	}

	p.logger.Debug("processing DNS query",
		"query_id", r.Id,
		"query_name", qname,
		"query_type", qtype,
		"client_ip", clientIPStr,
		"client_group", clientGroup)

	// Select the appropriate handler
	var selectedHandler mightydns.DNSHandler
	var handlerName string

	if clientGroup != "" {
		if policyHandler, exists := p.policyTrees[clientGroup]; exists {
			selectedHandler = policyHandler
			handlerName = fmt.Sprintf("policy_%s", clientGroup)
		}
	}

	// Fall back to base handler if no policy match
	if selectedHandler == nil {
		selectedHandler = p.baseHandler
		handlerName = "base"
		p.logger.Debug("using base handler (no policy match)",
			"query_id", r.Id,
			"client_ip", clientIPStr,
			"client_group", clientGroup,
			"handler", handlerName)
	} else {
		p.logger.Debug("matched client to policy",
			"query_id", r.Id,
			"client_ip", clientIPStr,
			"client_group", clientGroup,
			"handler", handlerName)
	}

	// Route to the selected handler with client group in context
	ctxWithClientGroup := context.WithValue(ctx, zone.ClientGroupKey{}, clientGroup)
	return selectedHandler.ServeDNS(ctxWithClientGroup, w, r)
}

func (p *PolicyHandler) Cleanup() error {
	p.logger.Debug("cleaning up policy handler")

	var cleanupErrors []error

	// Cleanup base handler
	if p.baseHandler != nil {
		if cleaner, ok := p.baseHandler.(mightydns.CleanerUpper); ok {
			if err := cleaner.Cleanup(); err != nil {
				p.logger.Error("error cleaning up base handler", "error", err)
				cleanupErrors = append(cleanupErrors, fmt.Errorf("base handler: %w", err))
			}
		}
	}

	// Cleanup policy handlers (but avoid double cleanup if they share instances)
	cleaned := make(map[mightydns.DNSHandler]bool)
	for group, handler := range p.policyTrees {
		if handler != nil && !cleaned[handler] && handler != p.baseHandler {
			if cleaner, ok := handler.(mightydns.CleanerUpper); ok {
				if err := cleaner.Cleanup(); err != nil {
					p.logger.Error("error cleaning up policy handler", "group", group, "error", err)
					cleanupErrors = append(cleanupErrors, fmt.Errorf("policy %s: %w", group, err))
				}
			}
			cleaned[handler] = true
		}
	}

	if len(cleanupErrors) > 0 {
		return fmt.Errorf("cleanup errors: %v", cleanupErrors)
	}

	return nil
}
