# Copyright (c) HashiCorp, Inc.

provider "tfsync" {
  region = "us-east-1"

  assume_role_with_web_identity {
    role_arn                = "my-role-arn"
    web_identity_token_file = "my-token"
  }
}
