// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Ensure TfSyncProvider satisfies various provider interfaces.
var _ provider.Provider = &TfSyncProvider{}

// TfSyncProvider defines the provider implementation.
type TfSyncProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// TfSyncProviderModel describes the provider data model.
type TfSyncProviderModel struct {
	Region                    types.String                    `tfsdk:"region"`
	AssumeRoleWithWebIdentity *assumeRoleWithWebIdentityBlock `tfsdk:"assume_role_with_web_identity"`
}

type assumeRoleWithWebIdentityBlock struct {
	RoleARN              types.String `tfsdk:"role_arn"`
	WebIdentityTokenFile types.String `tfsdk:"web_identity_token_file"`
}

func (p *TfSyncProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "tfsync"
	resp.Version = p.version
}

func (p *TfSyncProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"region": schema.StringAttribute{
				MarkdownDescription: "aws region",
				Description:         "aws region",
				Optional:            true,
			},
		},
		Blocks: map[string]schema.Block{
			"assume_role_with_web_identity": schema.SingleNestedBlock{
				MarkdownDescription: "configure assume-role-with-web-identity for aws s3 client",
				Description:         "configure assume-role-with-web-identity for aws s3 client",
				Attributes: map[string]schema.Attribute{
					"role_arn": schema.StringAttribute{
						MarkdownDescription: "role arn to assume",
						Description:         "role arn to assume",
						Required:            true,
					},
					"web_identity_token_file": schema.StringAttribute{
						MarkdownDescription: "path to web identity token file",
						Description:         "path to web identity token file",
						Required:            true,
					},
				},
			},
		},
	}
}

type ResourceConfigureData struct {
	tfeClient *tfe.Client
	s3Client  *s3.Client
}

func NewResourceConfigureData(tfeClient *tfe.Client, s3Client *s3.Client) *ResourceConfigureData {
	return &ResourceConfigureData{tfeClient: tfeClient, s3Client: s3Client}
}

func (p *TfSyncProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	tflog.Info(ctx, "Configuring tfsync provider")

	var data TfSyncProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tfeClient, err := tfe.NewClient(tfe.DefaultConfig())
	if err != nil {
		resp.Diagnostics.AddError("tfe client", fmt.Sprintf("failed to create tfe client: %s", err))
		return
	}

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(data.Region.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("aws client", fmt.Sprintf("failed to load AWS configuration: %s", err))
		return
	}

	stsClient := sts.NewFromConfig(cfg)

	if data.AssumeRoleWithWebIdentity != nil {
		cfg.Credentials = aws.NewCredentialsCache(stscreds.NewWebIdentityRoleProvider(stsClient, data.AssumeRoleWithWebIdentity.RoleARN.ValueString(), stscreds.IdentityTokenFile(data.AssumeRoleWithWebIdentity.WebIdentityTokenFile.ValueString())))
	}

	s3Client := s3.NewFromConfig(cfg)

	cd := NewResourceConfigureData(tfeClient, s3Client)

	resp.DataSourceData = cd
	resp.ResourceData = cd

	tflog.Info(ctx, "Configured tfsync client", map[string]any{"aws_region": s3Client.Options().Region})
}

func (p *TfSyncProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewS3ObjectResource,
	}
}

func (p *TfSyncProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return nil
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &TfSyncProvider{
			version: version,
		}
	}
}
