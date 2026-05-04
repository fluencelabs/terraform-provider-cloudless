package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
)

func TestResolveClusterID_Explicit(t *testing.T) {
	srv := mock.New()
	defer srv.Close()
	c := client.New(srv.URL, "k")

	var diags diag.Diagnostics
	got := resolveClusterID(context.Background(), c, types.StringValue("cluster-A"), types.StringNull(), &diags)
	if got != "cluster-A" {
		t.Fatalf("got %q, want cluster-A", got)
	}
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
}

func TestResolveClusterID_DeriveFromVPC(t *testing.T) {
	srv := mock.New()
	defer srv.Close()
	srv.SeedVPC("vpc-1", "main", "cluster-X")
	c := client.New(srv.URL, "k")

	var diags diag.Diagnostics
	got := resolveClusterID(context.Background(), c, types.StringNull(), types.StringValue("vpc-1"), &diags)
	if got != "cluster-X" {
		t.Fatalf("got %q, want cluster-X", got)
	}
	if diags.HasError() {
		t.Fatalf("unexpected diags: %v", diags)
	}
}

func TestResolveClusterID_MismatchErrors(t *testing.T) {
	srv := mock.New()
	defer srv.Close()
	srv.SeedVPC("vpc-1", "main", "cluster-X")
	c := client.New(srv.URL, "k")

	var diags diag.Diagnostics
	_ = resolveClusterID(context.Background(), c, types.StringValue("cluster-Y"), types.StringValue("vpc-1"), &diags)
	if !diags.HasError() {
		t.Fatal("expected mismatch error, got none")
	}
}

func TestResolveClusterID_NeitherErrors(t *testing.T) {
	srv := mock.New()
	defer srv.Close()
	c := client.New(srv.URL, "k")

	var diags diag.Diagnostics
	_ = resolveClusterID(context.Background(), c, types.StringNull(), types.StringNull(), &diags)
	if !diags.HasError() {
		t.Fatal("expected error when neither cluster_id nor vpc_id is set")
	}
}
