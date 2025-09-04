# DNS Zones

MightyDNS supports the concept of DNS Zones for managing local DNS records with upstream fallback capabilities. Zones provide a way to define authoritative DNS records for specific domains while seamlessly integrating with the policy engine for client-specific DNS views.

## Overview

DNS Zones in MightyDNS allow you to:

- **Define local DNS records** for specific domains
- **Provide upstream fallback** when records don't exist locally
- **Override records per client group** using the policy engine
- **Support multiple zone types** (currently Forward Zones)
- **Configure zone-specific upstream servers**

## Zone Types

### Forward Zones

Forward Zones are the primary zone type, providing local record resolution with upstream fallback.

**Behavior**:
1. **Local Resolution**: If a record exists locally, return it immediately
2. **Upstream Fallback**: If no local record exists, query the configured upstream servers
3. **Zone Matching**: Only processes queries that match the zone's domain

**Use Cases**:
- Split-horizon DNS (different IPs for internal vs external clients)
- Local service discovery
- Development environment overrides
- Corporate DNS with external fallback

## Configuration

### Basic Zone Manager Configuration

```json
{
  "handler": "dns.zone.manager",
  "zones": [
    {
      "type": "forward",
      "zone": "internal.example.com.",
      "records": {
        "api.internal.example.com.": {
          "type": "A",
          "value": "192.168.1.10",
          "ttl": 300
        },
        "web.internal.example.com.": {
          "type": "A", 
          "value": "192.168.1.20",
          "ttl": 300
        }
      },
      "upstream": {
        "upstreams": ["10.0.0.1:53"],
        "timeout": "3s",
        "protocol": "udp"
      }
    }
  ],
  "default_upstream": {
    "upstreams": ["8.8.8.8:53", "1.1.1.1:53"],
    "timeout": "5s",
    "protocol": "udp"
  }
}
```

### Zone Configuration Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Zone type (currently only "forward") |
| `zone` | string | Yes | Domain name for the zone (must end with ".") |
| `records` | object | No | Local DNS records for the zone |
| `upstream` | object | No | Zone-specific upstream configuration |

### Record Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Record type (A, AAAA, CNAME, TXT) |
| `value` | string | Yes | Record value (IP address, domain, text) |
| `ttl` | number | No | Time-to-live in seconds (default: 300) |

### Upstream Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `upstreams` | array | Yes | List of upstream DNS servers (host:port) |
| `timeout` | string | No | Query timeout (default: "5s") |
| `protocol` | string | No | Protocol: "udp", "tcp", "tcp-tls" (default: "udp") |

## Supported Record Types

### A Records (IPv4)
```json
{
  "type": "A",
  "value": "192.168.1.10",
  "ttl": 300
}
```

### AAAA Records (IPv6)
```json
{
  "type": "AAAA", 
  "value": "2001:db8::1",
  "ttl": 300
}
```

### CNAME Records (Aliases)
```json
{
  "type": "CNAME",
  "value": "target.example.com.",
  "ttl": 300
}
```

### TXT Records (Text)
```json
{
  "type": "TXT",
  "value": "v=spf1 include:_spf.example.com ~all",
  "ttl": 300
}
```

## Policy Integration

The Zone Manager integrates seamlessly with MightyDNS's policy engine to provide client-specific DNS views. This enables split-horizon DNS where different client groups see different IP addresses for the same hostname.

### Policy Override Configuration

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
          "api.app.example.com.": {
            "type": "A",
            "value": "192.168.1.10"
          },
          "web.app.example.com.": {
            "type": "A",
            "value": "192.168.1.20"
          }
        }
      }
    ]
  },
  "client_groups": {
    "internal": {
      "sources": ["192.168.0.0/16", "10.0.0.0/8"],
      "priority": 10
    },
    "external": {
      "sources": ["0.0.0.0/0"],
      "priority": 100
    }
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
                "api.app.example.com.": {
                  "type": "A",
                  "value": "203.0.113.10"
                }
              }
            }
          ]
        }
      }
    }
  ]
}
```

### Deep Record Merging

The policy engine performs **deep merging** of zone records:

- **Internal clients** (192.168.x.x) see:
  - `api.app.example.com.` → `192.168.1.10` (base record)
  - `web.app.example.com.` → `192.168.1.20` (base record, preserved)

- **External clients** see:
  - `api.app.example.com.` → `203.0.113.10` (policy override)
  - `web.app.example.com.` → `192.168.1.20` (base record, preserved)

**Key Benefits**:
- Override individual records without losing others
- Different upstream servers per client group
- Granular control over DNS responses
- Maintains configuration inheritance

## Query Resolution Flow

1. **Zone Matching**: Check if query matches any configured zone
2. **Local Record Lookup**: Search for exact record match in zone
3. **Record Type Validation**: Ensure record type matches query type
4. **Local Response**: Return local record if found
5. **Upstream Fallback**: Query zone-specific upstream if no local record
6. **Default Upstream**: Query default upstream if zone has no upstream
7. **NXDOMAIN**: Return NXDOMAIN if no resolution possible

## Examples

### Simple Forward Zone

```json
{
  "handler": "dns.zone.manager",
  "zones": [
    {
      "type": "forward",
      "zone": "local.example.com.",
      "records": {
        "server.local.example.com.": {
          "type": "A",
          "value": "192.168.1.100"
        }
      }
    }
  ],
  "default_upstream": {
    "upstreams": ["8.8.8.8:53"]
  }
}
```

### Multi-Zone Configuration

```json
{
  "handler": "dns.zone.manager",
  "zones": [
    {
      "type": "forward",
      "zone": "internal.corp.",
      "records": {
        "mail.internal.corp.": {"type": "A", "value": "10.0.1.5"},
        "wiki.internal.corp.": {"type": "CNAME", "value": "server.internal.corp."}
      },
      "upstream": {
        "upstreams": ["10.0.0.1:53"]
      }
    },
    {
      "type": "forward", 
      "zone": "dev.corp.",
      "records": {
        "api.dev.corp.": {"type": "A", "value": "192.168.100.10"}
      }
    }
  ],
  "default_upstream": {
    "upstreams": ["8.8.8.8:53", "1.1.1.1:53"]
  }
}
```

### Split-Horizon DNS with Policies

```json
{
  "handler": "policy",
  "base_handler": {
    "handler": "dns.zone.manager",
    "zones": [
      {
        "type": "forward",
        "zone": "services.company.com.",
        "records": {
          "api.services.company.com.": {"type": "A", "value": "10.0.1.10"},
          "web.services.company.com.": {"type": "A", "value": "10.0.1.20"},
          "admin.services.company.com.": {"type": "A", "value": "10.0.1.30"}
        }
      }
    ]
  },
  "client_groups": {
    "internal": {"sources": ["10.0.0.0/8", "192.168.0.0/16"]},
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
              "zone": "services.company.com.",
              "records": {
                "api.services.company.com.": {"type": "A", "value": "203.0.113.10"},
                "web.services.company.com.": {"type": "A", "value": "203.0.113.20"}
              }
            }
          ]
        }
      }
    }
  ]
}
```

In this example:
- Internal clients access services via private IPs (10.0.1.x)
- External clients access services via public IPs (203.0.113.x)  
- `admin.services.company.com.` is only accessible internally (not overridden)

## Best Practices

### Zone Design

- **Use fully qualified domain names** ending with "."
- **Group related services** in the same zone
- **Keep zone hierarchies logical** and manageable
- **Use descriptive zone names** that reflect their purpose

### Record Management

- **Set appropriate TTL values** based on change frequency
- **Use CNAME records** for service aliases and load balancers
- **Validate IP addresses** and domain names before deployment
- **Document record purposes** for maintenance

### Policy Configuration

- **Test policy overrides** thoroughly before production
- **Use specific client group matching** to avoid unintended effects
- **Monitor DNS query patterns** to optimize zone configuration
- **Keep base configuration minimal** and use policies for variations

### Performance Optimization

- **Configure zone-specific upstreams** for better locality
- **Use shorter timeouts** for internal upstreams
- **Monitor upstream response times** and adjust as needed
- **Cache DNS responses** appropriately with TTL settings

## Troubleshooting

### Common Issues

**Zone not matching queries**:
- Ensure zone name ends with "."
- Check subdomain matching logic
- Verify query domain is within zone scope

**Records not resolving**:
- Validate record type matches query type
- Check IP address format for A/AAAA records
- Ensure target domains are fully qualified for CNAME

**Policy overrides not working**:
- Verify client group classification
- Check policy module ID (`dns.zone.manager`)
- Ensure override zone names match base zones

**Upstream fallback failing**:
- Test upstream server connectivity
- Check timeout configuration
- Verify upstream server format (host:port)

### Debug Logging

Enable debug logging to troubleshoot zone resolution:

```json
{
  "logging": {
    "level": "DEBUG",
    "handler": "logger.text"
  }
}
```

Look for log entries with:
- `module=dns.zone.manager` - Zone manager operations
- `zone=example.com.` - Zone-specific resolution
- `client_group=internal` - Policy-based routing
- `upstream=8.8.8.8:53` - Upstream query attempts

## Future Enhancements

The Zone system is designed for extensibility. Potential future enhancements include:

- **Additional record types** (MX, SRV, NS, SOA, PTR)
- **Reverse DNS zones** for PTR record management
- **Zone transfers** (AXFR/IXFR) for zone synchronization
- **Dynamic updates** for record modification
- **DNSSEC support** for authenticated responses
- **Zone file import/export** for compatibility with BIND
- **Wildcard records** for pattern-based matching
- **Health checking** for automatic record updates