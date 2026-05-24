package provider

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

// Security-group HCL mode values.
const (
	modeAllowAll    = "allow_all"
	modeAllowListed = "allow_listed"
	modeDenyAll     = "deny_all"
)

// Protocol/port sentinel meaning "all ports" (no specific port range).
const protocolAll = "all"

// portRangeParts is the number of "min-max" fields in a port range spec.
const portRangeParts = 2

// rulesEqual reports whether two []sgRule slices are semantically equal.
func rulesEqual(a, b []sgRule) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Protocol.Equal(b[i].Protocol) ||
			!a[i].Ports.Equal(b[i].Ports) ||
			!a[i].CIDR.Equal(b[i].CIDR) ||
			!a[i].SecurityGroupID.Equal(b[i].SecurityGroupID) ||
			!a[i].Type.Equal(b[i].Type) {
			return false
		}
	}
	return true
}

// normalizeMode returns the effective mode for an Optional+Computed
// types.String. Defaults unset to "allow_all".
func normalizeMode(s types.String) (string, error) {
	if s.IsNull() || s.IsUnknown() || s.ValueString() == "" {
		return modeAllowAll, nil
	}
	v := s.ValueString()
	switch v {
	case modeAllowAll, modeAllowListed, modeDenyAll:
		return v, nil
	default:
		return "", fmt.Errorf("invalid mode: %q", v)
	}
}

// apiToMode classifies what we got back from the API.
func apiToMode(r client.SecurityGroupRules) string {
	switch r.Mode {
	case "allowAll":
		return modeAllowAll
	case "allow":
		if len(r.Rules) == 0 {
			return modeDenyAll
		}
		return modeAllowListed
	default:
		return modeAllowAll
	}
}

// buildRules constructs the wire SecurityGroupRules for the given mode.
func buildRules(mode string, blocks []sgRule) (client.SecurityGroupRules, error) {
	switch mode {
	case modeAllowAll:
		return client.SecurityGroupRules{Mode: "allowAll"}, nil
	case modeDenyAll:
		return client.SecurityGroupRules{Mode: "allow", Rules: []client.SecurityGroupRule{}}, nil
	case modeAllowListed:
		rules := make([]client.SecurityGroupRule, 0, len(blocks))
		for _, b := range blocks {
			rule, err := translateRule(b)
			if err != nil {
				return client.SecurityGroupRules{}, err
			}
			rules = append(rules, rule)
		}
		return client.SecurityGroupRules{Mode: "allow", Rules: rules}, nil
	default:
		return client.SecurityGroupRules{}, fmt.Errorf("invalid mode: %q", mode)
	}
}

func translateRule(b sgRule) (client.SecurityGroupRule, error) {
	r := client.SecurityGroupRule{Type: "ipv4"}
	if v := b.Type.ValueString(); v != "" {
		r.Type = v
	}

	pk := client.ProtocolKind{Kind: b.Protocol.ValueString()}
	switch pk.Kind {
	case protocolAll, "icmp":
		if b.Ports.ValueString() != "" {
			return r, fmt.Errorf("ports must be empty for protocol %q", pk.Kind)
		}
	case "tcp", "udp":
		ports, err := parsePorts(b.Ports.ValueString())
		if err != nil {
			return r, err
		}
		pk.Ports = ports
	default:
		return r, fmt.Errorf("invalid protocol: %q", pk.Kind)
	}
	r.ProtocolKind = pk

	hasCIDR := b.CIDR.ValueString() != ""
	hasSG := b.SecurityGroupID.ValueString() != ""
	if hasCIDR == hasSG {
		return r, errors.New("exactly one of cidr / security_group_id is required per rule")
	}
	if hasCIDR {
		v := b.CIDR.ValueString()
		r.Remote.Address = &v
	}
	if hasSG {
		v := b.SecurityGroupID.ValueString()
		r.Remote.SecurityGroup = &v
	}
	return r, nil
}

func parsePorts(s string) (client.Ports, error) {
	if s == "" || s == protocolAll {
		return client.Ports{All: true}, nil
	}
	if !strings.Contains(s, "-") {
		v, err := strconv.ParseUint(s, 10, 16)
		if err != nil {
			return client.Ports{}, fmt.Errorf("invalid port %q: %w", s, err)
		}
		p := uint16(v)
		return client.Ports{Exact: &p}, nil
	}
	parts := strings.SplitN(s, "-", portRangeParts)
	mn, err := strconv.ParseUint(parts[0], 10, 16)
	if err != nil {
		return client.Ports{}, fmt.Errorf("invalid range start %q: %w", parts[0], err)
	}
	mx, err := strconv.ParseUint(parts[1], 10, 16)
	if err != nil {
		return client.Ports{}, fmt.Errorf("invalid range end %q: %w", parts[1], err)
	}
	if mn > mx {
		return client.Ports{}, fmt.Errorf("invalid range %q (min > max)", s)
	}
	mn16, mx16 := uint16(mn), uint16(mx)
	return client.Ports{RangeMin: &mn16, RangeMax: &mx16}, nil
}

func rulesToModel(rules []client.SecurityGroupRule) []sgRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]sgRule, len(rules))
	for i, r := range rules {
		out[i] = sgRule{
			Type:     types.StringValue(r.Type),
			Protocol: types.StringValue(r.ProtocolKind.Kind),
			Ports:    types.StringValue(portsToString(r.ProtocolKind)),
		}
		if r.Remote.Address != nil {
			out[i].CIDR = types.StringValue(*r.Remote.Address)
		} else {
			out[i].CIDR = types.StringNull()
		}
		if r.Remote.SecurityGroup != nil {
			out[i].SecurityGroupID = types.StringValue(*r.Remote.SecurityGroup)
		} else {
			out[i].SecurityGroupID = types.StringNull()
		}
	}
	return out
}

func portsToString(pk client.ProtocolKind) string {
	switch pk.Kind {
	case protocolAll, "icmp":
		return ""
	case "tcp", "udp":
		if pk.Ports.All {
			return protocolAll
		}
		if pk.Ports.Exact != nil {
			return strconv.FormatUint(uint64(*pk.Ports.Exact), 10)
		}
		if pk.Ports.RangeMin != nil && pk.Ports.RangeMax != nil {
			return strconv.FormatUint(
				uint64(*pk.Ports.RangeMin),
				10,
			) + "-" + strconv.FormatUint(
				uint64(*pk.Ports.RangeMax),
				10,
			)
		}
	}
	return ""
}
