# DNS Zones Quick Reference

## Basic Zone Configuration

```json
{
  "handler": "dns.zone.manager",
  "zones": [
    {
      "type": "forward",
      "zone": "example.com.",
      "records": {
        "api.example.com.": {"type": "A", "value": "192.168.1.10"},
        "web.example.com.": {"type": "A", "value": "192.168.1.20"},
        "alias.example.com.": {"type": "CNAME", "value": "web.example.com."}
      },
      "upstream": {
        "upstreams": ["10.0.0.1:53"],
        "timeout": "3s"
      }
    }
  ],
  "default_upstream": {
    "upstreams": ["8.8.8.8:53", "1.1.1.1:53"]
  }
}
```

## Split-Horizon DNS with Policies

```json
{
  "handler": "policy",
  "base_handler": {
    "handler": "dns.zone.manager",
    "zones": [
      {
        "type": "forward",
        "zone": "app.example.com.",
        "records": {
          "api.app.example.com.": {"type": "A", "value": "192.168.1.10"},
          "web.app.example.com.": {"type": "A", "value": "192.168.1.20"}
        }
      }
    ]
  },
  "client_groups": {
    "internal": {"sources": ["192.168.0.0/16"]},
    "external": {"sources": ["0.0.0.0/0"]}
  },
  "policies": [
    {
      "match": {"client_group": "external"},
      "overrides": {
        "dns.zone.manager": {
          "zones": [
            {
              "type": "forward",
              "zone": "app.example.com.",
              "records": {
                "api.app.example.com.": {"type": "A", "value": "203.0.113.10"}
              }
            }
          ]
        }
      }
    }
  ]
}
```

## Supported Record Types

| Type | Purpose | Example Value |
|------|---------|---------------|
| A | IPv4 address | `"192.168.1.10"` |
| AAAA | IPv6 address | `"2001:db8::1"` |
| CNAME | Alias/canonical name | `"target.example.com."` |
| TXT | Text record | `"v=spf1 include:_spf.example.com ~all"` |

## Resolution Flow

1. **Zone Matching** - Check if query matches zone domain
2. **Local Lookup** - Search for record in zone
3. **Type Validation** - Ensure record type matches query
4. **Local Response** - Return local record if found
5. **Zone Upstream** - Query zone-specific upstream
6. **Default Upstream** - Query default upstream servers
7. **NXDOMAIN** - Return not found if no resolution

## Key Features

✅ **Local DNS Records** - Define authoritative records for domains  
✅ **Upstream Fallback** - Forward unresolved queries to upstream servers  
✅ **Policy Integration** - Client-specific DNS views using existing policy engine  
✅ **Deep Record Merging** - Override individual records without losing others  
✅ **Multiple Zones** - Support for multiple domains in single configuration  
✅ **Zone-Specific Upstreams** - Different upstream servers per zone  

## Common Use Cases

- **Split-Horizon DNS** - Different IPs for internal vs external clients
- **Local Service Discovery** - Internal service name resolution
- **Development Overrides** - Test environment DNS configuration
- **Corporate DNS** - Internal domains with external fallback
- **Load Balancer Aliases** - CNAME records for service endpoints

## Testing Your Configuration

```bash
# Test with MightyDNS
./mightydns -config test-zone-config.json

# Query from internal network
dig @localhost -p 15353 api.app.example.com

# Query from external network (use external IP)
dig @your-external-ip -p 15353 api.app.example.com
```