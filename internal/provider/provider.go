// Package provider implements the cloudless Terraform provider, which
// manages resources on the Fluence compute marketplace.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudless/terraform-provider-cloudless/internal/client"
)

type cloudlessProvider struct {
	version        string
	overrideClient *client.Client // set by NewWithClient for tests
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &cloudlessProvider{version: version} }
}

// NewWithClient is used by unit tests to inject a pre-built client (typically
// pointed at a mock HTTP server). The returned provider skips the api_key
// resolution and uses the supplied client for every resource and data source.
func NewWithClient(c *client.Client, version string) func() provider.Provider {
	return func() provider.Provider { return &cloudlessProvider{version: version, overrideClient: c} }
}

func (p *cloudlessProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cloudless"
	resp.Version = p.version
}

type providerModel struct {
	APIKey   types.String `tfsdk:"api_key"`
	Endpoint types.String `tfsdk:"endpoint"`
}

func (p *cloudlessProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The cloudless provider manages compute resources on the Fluence decentralized compute marketplace.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Description: "Fluence API key. May also be set via the FLUENCE_API_KEY environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"endpoint": schema.StringAttribute{
				Description: "Override the API base URL. Defaults to https://api.fluence.dev. May also be set via FLUENCE_ENDPOINT.",
				Optional:    true,
			},
		},
	}
}

func (p *cloudlessProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	if p.overrideClient != nil {
		resp.DataSourceData = p.overrideClient
		resp.ResourceData = p.overrideClient
		return
	}

	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := data.APIKey.ValueString()
	if apiKey == "" {
		apiKey = os.Getenv("FLUENCE_API_KEY")
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing Fluence API key",
			"Set the api_key provider attribute or the FLUENCE_API_KEY environment variable.",
		)
		return
	}

	endpoint := data.Endpoint.ValueString()
	if endpoint == "" {
		endpoint = os.Getenv("FLUENCE_ENDPOINT")
	}

	c := client.New(endpoint, apiKey, client.WithUserAgent("terraform-provider-cloudless/"+p.version))
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *cloudlessProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewSSHKeyResource,
		NewVPCResource,
		NewSubnetResource,
		NewVMResource,
		NewSecurityGroupResource,
		NewStorageResource,
		NewPublicIPResource,
		NewVMPublicIPAttachmentResource,
		NewSecurityGroupAttachmentResource,
	}
}

func (p *cloudlessProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewClusterDataSource,
		NewClustersDataSource,
		NewVMConfigurationsDataSource,
		NewDefaultImagesDataSource,
	}
}
