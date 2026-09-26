// Package testing wires up a *resource.UnitTest-friendly provider that talks
// to an in-memory mock Fluence server.
package testing

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/client/mock"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider"
)

// Harness bundles a mock server with a provider factory map ready to hand to
// resource.UnitTest.
type Harness struct {
	Mock      *mock.Server
	Client    *client.Client
	Factories map[string]func() (tfprotov6.ProviderServer, error)
}

// New starts a fresh mock + provider. Callers must defer h.Close().
//
// Every test that uses the harness fails on a contract violation recorded by
// the mock: without that assertion at teardown, a mock response that drifts
// from the spec leaves the whole suite green.
func New(t testing.TB) *Harness {
	t.Helper()
	srv := mock.New()
	t.Cleanup(func() {
		for _, v := range srv.ContractViolations() {
			t.Errorf("mock contract violation: %s", v)
		}
	})
	c := client.New(srv.URL, "test-key")
	return &Harness{
		Mock:   srv,
		Client: c,
		Factories: map[string]func() (tfprotov6.ProviderServer, error){
			"cloudless": providerserver.NewProtocol6WithError(provider.NewWithClient(c, "unit-test")()),
		},
	}
}

// Close shuts down the mock server.
func (h *Harness) Close() { h.Mock.Close() }

// Ctx returns a fresh background context for tests.
func (h *Harness) Ctx() context.Context { return context.Background() }
