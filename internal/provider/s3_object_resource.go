// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/hashicorp/go-tfe"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &S3ObjectResource{}
var _ resource.ResourceWithImportState = &S3ObjectResource{}

func NewS3ObjectResource() resource.Resource {
	return &S3ObjectResource{}
}

type S3ObjectResource struct {
	tfeClient *tfe.Client
	s3Client  *s3.Client
}

type S3ObjectResourceModel struct {
	Id                   types.String `tfsdk:"id"`
	WorkspaceId          types.String `tfsdk:"workspace_id"`
	Bucket               types.String `tfsdk:"bucket"`
	Key                  types.String `tfsdk:"key"`
	StateContentsSha256  types.String `tfsdk:"state_contents_sha256"`
	BucketContentsSha256 types.String `tfsdk:"bucket_contents_sha256"`
	KmsKeyId             types.String `tfsdk:"kms_key_id"`
}

func (r *S3ObjectResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_s3_object"
}

func (r *S3ObjectResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Resource to sync tf-state to an s3 object",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Example identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"workspace_id": schema.StringAttribute{
				MarkdownDescription: "terraform workspace id",
				Required:            true,
			},
			"bucket": schema.StringAttribute{
				MarkdownDescription: "s3 bucket",
				Required:            true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "s3 bucket key",
				Required:            true,
			},
			"state_contents_sha256": schema.StringAttribute{
				MarkdownDescription: "sha256 sum of tf state",
				Computed:            true,
			},
			"bucket_contents_sha256": schema.StringAttribute{
				MarkdownDescription: "sha256 sum of s3 bucket object contents",
				Computed:            true,
			},
			"kms_key_id": schema.StringAttribute{
				MarkdownDescription: "kms key id",
				Optional:            true,
			},
		},
	}
}

func (r *S3ObjectResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	data, ok := req.ProviderData.(*ResourceConfigureData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *ResourceConfigureData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.tfeClient = data.tfeClient
	r.s3Client = data.s3Client
}

func (r *S3ObjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	resp.Diagnostics.Append(validateS3ObjectResource(r)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var data S3ObjectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, d := getStateFile(ctx, r.tfeClient, data.WorkspaceId.ValueString())
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	o := &putObjectOptions{
		Bucket:   data.Bucket.String(),
		Key:      data.Key.String(),
		KmsKeyId: data.KmsKeyId.String(),
		Contents: state,
	}

	resp.Diagnostics.Append(putS3ObjectContents(ctx, r.s3Client, o)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Id = newS3ObjectResourceID(&data)
	data.StateContentsSha256 = sha256Contents(state)
	data.BucketContentsSha256 = sha256Contents(state)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *S3ObjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	resp.Diagnostics.Append(validateS3ObjectResource(r)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var data S3ObjectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, d := getStateFile(ctx, r.tfeClient, data.WorkspaceId.ValueString())
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	contents, d := getS3ObjectContents(ctx, r.s3Client, data.Bucket.ValueString(), data.Key.ValueString())
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Id = newS3ObjectResourceID(&data)
	data.StateContentsSha256 = sha256Contents(state)
	data.BucketContentsSha256 = sha256Contents(contents)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *S3ObjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.Append(validateS3ObjectResource(r)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var data S3ObjectResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	state, d := getStateFile(ctx, r.tfeClient, data.WorkspaceId.ValueString())
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	o := &putObjectOptions{
		Bucket:   data.Bucket.String(),
		Key:      data.Key.String(),
		KmsKeyId: data.KmsKeyId.String(),
		Contents: state,
	}

	resp.Diagnostics.Append(putS3ObjectContents(ctx, r.s3Client, o)...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.StateContentsSha256 = sha256Contents(state)
	data.BucketContentsSha256 = sha256Contents(state)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *S3ObjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.Append(validateS3ObjectResource(r)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var data S3ObjectResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(deleteS3Object(ctx, r.s3Client, data.Bucket.ValueString(), data.Key.ValueString())...)
}

func (r *S3ObjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(validateS3ObjectResource(r)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func sha256Contents(contents []byte) basetypes.StringValue {
	hash := sha256.Sum256(contents)
	hashString := hex.EncodeToString(hash[:])
	return types.StringValue(hashString)
}

func newS3ObjectResourceID(data *S3ObjectResourceModel) basetypes.StringValue {
	return types.StringValue(fmt.Sprintf("%s/%s/%s", data.WorkspaceId.ValueString(), data.Bucket.ValueString(), data.Key.ValueString()))
}

func getStateFile(ctx context.Context, client *tfe.Client, workspaceId string) (state []byte, diag diag.Diagnostics) {
	ver, err := client.StateVersions.ReadCurrent(ctx, workspaceId)
	if err != nil {
		diag.AddError("tfe client", fmt.Sprintf("failed to get state version: %s", err))
		return
	}

	state, err = client.StateVersions.Download(ctx, ver.DownloadURL)
	if err != nil {
		diag.AddError("tfe client", fmt.Sprintf("failed to download state: %s", err))
		return
	}

	return
}

func getS3ObjectContents(ctx context.Context, client *s3.Client, bucket string, key string) (contents []byte, diag diag.Diagnostics) {
	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		diag.AddError("s3 client", fmt.Sprintf("failed to get object: %s", err))
		return
	}
	defer resp.Body.Close()

	contents, err = io.ReadAll(resp.Body)
	if err != nil {
		diag.AddError("s3 client", fmt.Sprintf("failed to read body: %s", err))
		return
	}

	return
}

type putObjectOptions struct {
	// S3 Bucket Name
	Bucket string

	// S3 Bucket Key
	Key string

	// KMS Key ID
	KmsKeyId string

	// Object Contents
	Contents []byte
}

func putS3ObjectContents(ctx context.Context, client *s3.Client, o *putObjectOptions) (diag diag.Diagnostics) {
	if len(o.Contents) == 0 {
		diag.AddError("s3 client", "empty contents")
		return
	}

	input := &s3.PutObjectInput{
		Bucket:            aws.String(o.Bucket),
		Key:               aws.String(o.Key),
		Body:              io.NopCloser(bytes.NewReader(o.Contents)),
		ContentLength:     aws.Int64(int64(len(o.Contents))),
		ContentType:       aws.String("application/json"),
		ChecksumAlgorithm: s3types.ChecksumAlgorithmSha256,
	}

	if o.KmsKeyId != "" {
		input.ServerSideEncryption = s3types.ServerSideEncryptionAwsKms
		input.SSEKMSKeyId = aws.String(o.KmsKeyId)
	}

	_, err := client.PutObject(ctx, input)
	if err != nil {
		diag.AddError("s3 client", fmt.Sprintf("failed s3 put object: %s", err))
		return
	}

	return
}

func validateS3ObjectResource(r *S3ObjectResource) (diag diag.Diagnostics) {
	if r == nil {
		diag.AddError("provider", "nil receiver")
		return
	}

	if r.s3Client == nil {
		diag.AddError("provider", "nil s3 client")
		return
	}

	if r.tfeClient == nil {
		diag.AddError("provider", "nil tfe client")
		return
	}

	return
}

func deleteS3Object(ctx context.Context, client *s3.Client, bucket string, key string) (diag diag.Diagnostics) {
	_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		diag.AddError("s3 client", fmt.Sprintf("failed to delete s3 object: %s", err))
		return
	}

	return
}
