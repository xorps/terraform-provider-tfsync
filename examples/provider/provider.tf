# Copyright (c) HashiCorp, Inc.

provider "tfsync" {
  region = "us-east-1"

  assume_role_with_web_identity {
    role_arn                = "my-role-arn"
    web_identity_token_file = "my-token"
  }
}

data "tfe_workspace_ids" "all" {
  names        = ["*"]
  organization = var.organization
}

resource "tfsync_s3_object" "this" {
  for_each = data.tfe_workspace_ids.all.ids

  bucket       = var.bucket
  key          = "statefiles/${each.key}/terraform.tfstate"
  workspace_id = each.value
  ignore_empty = true # do nothing for workspaces with no state

  # tags to apply to s3 object
  tags = {
    MyTag = MyValue
  }
}
