terraform {
  required_version = ">= 1.10"
  backend "s3" {
    bucket       = "redis-adbc-telemetry-735486936198-us-east-1"
    key          = "terraform/terraform.tfstate"
    region       = "us-east-1"
    encrypt      = true
    use_lockfile = true
  }
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}
provider "aws" {
  region = var.region
  default_tags {
    tags = { Project = "redis-adbc-driver", Purpose = "Product usage feedback", ManagedBy = "Terraform", owner = var.owner_email, RedisLabsIdentifier = "redis-adbc-telemetry", skip_deletion = "true" }
  }
}
variable "region" { default = "us-east-1" }
variable "hosted_zone_id" { default = "Z08723911XXA3M3FQCZ4E" }
variable "hostname" { default = "adbc-telemetry.sa.redislabs.com" }
data "aws_caller_identity" "current" {}
locals {
  bucket = "redis-adbc-telemetry-${data.aws_caller_identity.current.account_id}-${var.region}"
}
resource "aws_s3_bucket" "backups" {
  bucket        = local.bucket
  force_destroy = false
  tags          = { Name = "redis-adbc-telemetry-backups" }
  lifecycle { prevent_destroy = true }
}
resource "aws_s3_bucket_public_access_block" "backups" {
  bucket                  = aws_s3_bucket.backups.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}
resource "aws_s3_bucket_versioning" "backups" {
  bucket = aws_s3_bucket.backups.id
  versioning_configuration { status = "Enabled" }
}
resource "aws_s3_bucket_server_side_encryption_configuration" "backups" {
  bucket = aws_s3_bucket.backups.id
  rule {
    apply_server_side_encryption_by_default { sse_algorithm = "AES256" }
  }
}
resource "aws_s3_bucket_lifecycle_configuration" "backups" {
  bucket     = aws_s3_bucket.backups.id
  depends_on = [aws_s3_bucket_versioning.backups]
  rule {
    id     = "hourly-backups-90-days"
    status = "Enabled"
    filter { prefix = "backups/" }
    expiration { days = 90 }
    noncurrent_version_expiration { noncurrent_days = 7 }
    abort_incomplete_multipart_upload { days_after_initiation = 1 }
  }
  rule {
    id     = "retired-serverless-generations"
    status = "Enabled"
    filter { prefix = "serverless/generations/" }
    noncurrent_version_expiration { noncurrent_days = 7 }
  }
}
resource "aws_s3_bucket_policy" "backups" {
  bucket = aws_s3_bucket.backups.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Deny", Principal = "*", Action = "s3:*", Resource = [aws_s3_bucket.backups.arn, "${aws_s3_bucket.backups.arn}/*"], Condition = { Bool = { "aws:SecureTransport" = "false" } } }] })
}
resource "aws_route53_record" "collector" {
  zone_id = var.hosted_zone_id
  name    = var.hostname
  type    = "A"
  alias {
    name                   = module.serverless.domain_target
    zone_id                = module.serverless.domain_zone_id
    evaluate_target_health = false
  }
  allow_overwrite = false
}
output "endpoint" { value = "https://${var.hostname}/v1/usage" }
output "dashboard" { value = "https://${var.hostname}/" }
output "backup_bucket" { value = aws_s3_bucket.backups.id }


variable "owner_email" {
  type        = string
  description = "Account-required lowercase owner tag. Supply privately; do not commit operational tfvars."
}

# The legacy EC2 collector was retired after the 2026-11-19 serverless cutover.
# Its instance and root volume no longer exist, so forget their state entries
# instead of planning a destroy. Its unused security group, instance role,
# profile and FinOps lifecycle exception are no longer declared, so an approved
# apply deletes them.
removed {
  from = aws_instance.collector
  lifecycle { destroy = false }
}
removed {
  from = aws_ec2_instance_state.legacy
  lifecycle { destroy = false }
}
