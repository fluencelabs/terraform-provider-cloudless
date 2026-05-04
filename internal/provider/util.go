package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

// stringsFromList extracts the underlying []string from a types.List of
// strings. Null/Unknown lists yield nil. Use for plan/state read paths.
func stringsFromList(l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	elems := l.Elements()
	out := make([]string, 0, len(elems))
	for _, e := range elems {
		s, ok := e.(types.String)
		if !ok || s.IsNull() || s.IsUnknown() {
			continue
		}
		out = append(out, s.ValueString())
	}
	return out
}

// listFromStrings returns a types.List of types.StringType with the supplied
// string values. nil input becomes an empty list.
func listFromStrings(in []string) types.List {
	if in == nil {
		in = []string{}
	}
	vals := make([]attr.Value, len(in))
	for i, s := range in {
		vals[i] = types.StringValue(s)
	}
	return types.ListValueMust(types.StringType, vals)
}

// clientFromProviderData extracts the *client.Client a provider passes through
// ResourceData/DataSourceData and reports a friendly error if the assertion
// fails.
func clientFromProviderData(providerData any, diags *diag.Diagnostics) *client.Client {
	if providerData == nil {
		return nil
	}
	c, ok := providerData.(*client.Client)
	if !ok {
		diags.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a provider bug.", providerData),
		)
		return nil
	}
	return c
}

// nullableString turns a types.String into a *string suitable for the API.
// Null and unknown both serialize as nil so the field is omitted.
func nullableString(s types.String) *string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	v := s.ValueString()
	return &v
}

// nullableBool mirrors nullableString for booleans.
func nullableBool(b types.Bool) *bool {
	if b.IsNull() || b.IsUnknown() {
		return nil
	}
	v := b.ValueBool()
	return &v
}

// stringFromPtr converts a *string from the API into a types.String, mapping
// nil to a null value.
func stringFromPtr(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

// boolFromPtr converts a *bool from the API into a types.Bool.
func boolFromPtr(p *bool) types.Bool {
	if p == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*p)
}

// toStringList wraps a []string as a slice of types.String. Used by data
// source models that hold list-of-string attributes as []types.String for
// historical reasons; new resources should prefer types.List for Computed
// list fields.
func toStringList(in []string) []types.String {
	out := make([]types.String, len(in))
	for i, s := range in {
		out[i] = types.StringValue(s)
	}
	return out
}

// pollOptions controls a wait loop. All resources use the same cadence today;
// expose this so individual resources can extend it later.
type pollOptions struct {
	Timeout  time.Duration
	Interval time.Duration
}

func defaultPoll() pollOptions {
	return pollOptions{Timeout: 30 * time.Minute, Interval: 5 * time.Second}
}

// errStopPolling is returned by a poll func when the resource has reached a
// terminal state and the loop should exit successfully.
var errStopPolling = errors.New("stop polling")

// waitFor calls fn repeatedly until it returns errStopPolling, a non-nil
// error, or the context/timeout is exhausted.
func waitFor(ctx context.Context, opts pollOptions, fn func(context.Context) error) error {
	deadline := time.Now().Add(opts.Timeout)
	for {
		err := fn(ctx)
		tflog.Debug(ctx, "waitFor iteration", map[string]any{
			"interval_ms": opts.Interval.Milliseconds(),
			"remaining":   time.Until(deadline).Truncate(time.Second).String(),
		})
		if err == errStopPolling {
			return nil
		}
		if err != nil {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s waiting for resource to converge", opts.Timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opts.Interval):
		}
	}
}

// terminalFailure returns true for status strings the API uses to signal a
// non-recoverable end state.
func terminalFailure(status string) bool {
	switch status {
	case "failed":
		return true
	}
	return false
}

// isReady reports whether a status string indicates the resource is fully
// provisioned. Both "ready" (VPC/subnet/SG/storage/public-ip) and "launched"
// (VM) are considered ready.
func isReady(status string) bool {
	switch status {
	case "ready", "launched":
		return true
	}
	return false
}

// isRemoved reports whether the resource has finished teardown.
func isRemoved(status string) bool {
	switch status {
	case "removed", "terminated":
		return true
	}
	return false
}

// resolveClusterID returns the effective cluster_id for a resource.
//
// Behavior:
//   - explicit only           → return explicit (no API call)
//   - vpc_id only             → fetch parent VPC, return its cluster_id
//   - both set                → fetch parent VPC, verify match, return; error on mismatch
//   - neither                 → diags error
func resolveClusterID(ctx context.Context, c *client.Client, explicit, vpcID types.String, diags *diag.Diagnostics) string {
	hasExplicit := !explicit.IsNull() && !explicit.IsUnknown() && explicit.ValueString() != ""
	hasVPC := !vpcID.IsNull() && !vpcID.IsUnknown() && vpcID.ValueString() != ""

	if !hasExplicit && !hasVPC {
		diags.AddError(
			"Missing cluster_id",
			"set cluster_id explicitly, or supply vpc_id so it can be derived from the parent VPC",
		)
		return ""
	}

	if hasExplicit && !hasVPC {
		return explicit.ValueString()
	}

	vpc, err := c.GetVPC(ctx, vpcID.ValueString())
	if err != nil {
		diags.AddError("Resolve cluster_id from vpc_id failed", err.Error())
		return ""
	}

	if hasExplicit && explicit.ValueString() != vpc.ClusterID {
		diags.AddError(
			"cluster_id / vpc_id mismatch",
			"the explicit cluster_id ("+explicit.ValueString()+") does not match the parent VPC's cluster_id ("+vpc.ClusterID+")",
		)
		return ""
	}

	return vpc.ClusterID
}
