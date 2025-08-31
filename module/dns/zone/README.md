# DNS Zone Module

This module implements DNS zones for MightyDNS, providing local DNS record management with upstream fallback capabilities.

## Features

- **Forward Zones**: Local records with upstream fallback
- **Policy Integration**: Client-specific DNS views using the policy engine
- **Deep Record Merging**: Override individual records without losing others
- **Multiple Record Types**: A, AAAA, CNAME, TXT records supported
- **Zone-Specific Upstreams**: Different upstream servers per zone
- **Extensible Design**: Easy to add new zone types and record types

## Module Structure

- `zone.go` - Core zone interface and common utilities
- `forward.go` - Forward zone implementation
- `manager.go` - Zone manager DNS handler
- `zone_test.go` - Comprehensive test suite

## Module Registration

The Zone Manager registers as `dns.zone.manager` and can be used directly or as a base handler with the policy engine.

## Quick Start

### Basic Configuration

```json
{
  "handler": "dns.zone.manager",
  "zones": [
    {
      "type": "forward",
      "zone": "example.com.",
      "records": {
        "api.example.com.": {"type": "A", "value": "192.168.1.10"}
      }
    }
  ]
}
```

### With Policy Overrides

```json
{
  "handler": "policy",
  "base_handler": {
    "handler": "dns.zone.manager",
    "zones": [...]
  },
  "policies": [
    {
      "match": {"client_group": "external"},
      "overrides": {
        "dns.zone.manager": {
          "zones": [
            {
              "zone": "example.com.",
              "records": {
                "api.example.com.": {"type": "A", "value": "203.0.113.10"}
              }
            }
          ]
        }
      }
    }
  ]
}
```

## Documentation

See the main documentation for detailed configuration options and examples:

- [DNS Zones Documentation](../../docs/zones.md)
- [DNS Zones Quick Reference](../../docs/zones-quickref.md)

## Testing

Run the test suite:

```bash
go test ./module/dns/zone/...
```

The tests cover:
- Zone matching logic
- Record resolution
- DNS response generation
- Record merging functionality