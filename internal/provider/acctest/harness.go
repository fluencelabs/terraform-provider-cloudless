// Package acctest provides a setup helper for acceptance tests that hit the
// real Fluence API. Tests skip unless TF_ACC=1 and FLUENCE_API_KEY are set.
package acctest

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
	"github.com/cloudless/terraform-provider-cloudless/internal/provider"
)

// Setup ensures TF_ACC=1 and FLUENCE_API_KEY are set, returning provider
// factories pointed at the real API. Calls t.Skip() with a clear reason
// otherwise.
func Setup(t *testing.T) map[string]func() (tfprotov6.ProviderServer, error) {
	t.Helper()
	if os.Getenv("TF_ACC") != "1" {
		t.Skip("set TF_ACC=1 to run acceptance tests")
	}
	if os.Getenv("FLUENCE_API_KEY") == "" {
		t.Skip("set FLUENCE_API_KEY to run acceptance tests")
	}
	return map[string]func() (tfprotov6.ProviderServer, error){
		"cloudless": providerserver.NewProtocol6WithError(provider.New("acc")()),
	}
}

// RealClient returns a *client.Client pointed at the real Fluence API,
// authenticated via FLUENCE_API_KEY (which Setup already verified is set).
func RealClient() *client.Client {
	return client.New(
		os.Getenv("FLUENCE_ENDPOINT"),
		os.Getenv("FLUENCE_API_KEY"),
		client.WithUserAgent("terraform-provider-cloudless/acc"),
	)
}
