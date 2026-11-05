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
    tags = { Project = "redis-adbc-driver", Purpose = "Product usage feedback", ManagedBy = "Terraform", Owner = "Albert Wong" }
  }
}
variable "region" { default = "us-east-1" }
variable "vpc_id" { default = "vpc-9054f0f4" }
variable "subnet_id" { default = "subnet-41211f18" }
variable "hosted_zone_id" { default = "Z08723911XXA3M3FQCZ4E" }
variable "hostname" { default = "adbc-telemetry.sa.redislabs.com" }
variable "source_commit" {
  type        = string
  description = "Reviewed immutable redis/redis-adbc-driver commit containing analytics and bootstrap."
  validation {
    condition     = can(regex("^[0-9a-f]{40}$", var.source_commit))
    error_message = "Use a full immutable Git commit SHA."
  }
}
data "aws_caller_identity" "current" {}
data "aws_ssm_parameter" "ubuntu_ami" {
  name = "/aws/service/canonical/ubuntu/server/24.04/stable/current/amd64/hvm/ebs-gp3/ami-id"
}
locals {
  name   = "redis-adbc-telemetry"
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
}
resource "aws_s3_bucket_policy" "backups" {
  bucket = aws_s3_bucket.backups.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Deny", Principal = "*", Action = "s3:*", Resource = [aws_s3_bucket.backups.arn, "${aws_s3_bucket.backups.arn}/*"], Condition = { Bool = { "aws:SecureTransport" = "false" } } }] })
}
resource "aws_iam_role" "collector" {
  tags               = { Name = local.name }
  name               = local.name
  assume_role_policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Principal = { Service = "ec2.amazonaws.com" }, Action = "sts:AssumeRole" }] })
}
resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.collector.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}
resource "aws_iam_role_policy" "backup_writer" {
  name   = "write-analytics-backups"
  role   = aws_iam_role.collector.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [{ Effect = "Allow", Action = ["s3:PutObject", "s3:AbortMultipartUpload"], Resource = "${aws_s3_bucket.backups.arn}/backups/*" }] })
}
resource "aws_iam_instance_profile" "collector" {
  tags = { Name = local.name }
  name = local.name
  role = aws_iam_role.collector.name
}
resource "aws_security_group" "collector" {
  name        = "${local.name}-https"
  tags        = { Name = "${local.name}-https" }
  description = "HTTPS analytics; HTTP only for certificate validation and redirects"
  vpc_id      = var.vpc_id
  ingress {
    description = "Public HTTPS aggregate ingestion"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    description = "ACME certificate validation and HTTPS redirect"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
resource "aws_instance" "collector" {
  ami           = nonsensitive(data.aws_ssm_parameter.ubuntu_ami.value)
  instance_type = "t3.small"
  credit_specification { cpu_credits = "standard" }
  subnet_id                   = var.subnet_id
  vpc_security_group_ids      = [aws_security_group.collector.id]
  iam_instance_profile        = aws_iam_instance_profile.collector.name
  associate_public_ip_address = true
  disable_api_termination     = true
  metadata_options { http_tokens = "required" }
  volume_tags = { Name = "${local.name}-data" }
  root_block_device {
    volume_size = 30
    volume_type = "gp3"
    encrypted   = true
  }
  user_data  = templatefile("${path.module}/user-data.sh.tftpl", { source_commit = var.source_commit, hostname = var.hostname, bucket = aws_s3_bucket.backups.id, region = var.region })
  tags       = { Name = local.name }
  depends_on = [aws_iam_role_policy_attachment.ssm, aws_iam_role_policy.backup_writer, aws_s3_bucket_public_access_block.backups, aws_s3_bucket_server_side_encryption_configuration.backups]
  lifecycle { prevent_destroy = true }
}
resource "aws_eip" "collector" {
  domain   = "vpc"
  instance = aws_instance.collector.id
  tags     = { Name = local.name }
}
resource "aws_route53_record" "collector" {
  zone_id         = var.hosted_zone_id
  name            = var.hostname
  type            = "A"
  ttl             = 60
  records         = [aws_eip.collector.public_ip]
  allow_overwrite = false
}
output "endpoint" { value = "https://${var.hostname}/v1/usage" }
output "dashboard" { value = "https://${var.hostname}/" }
output "instance_id" { value = aws_instance.collector.id }
output "backup_bucket" { value = aws_s3_bucket.backups.id }
output "public_ip" { value = aws_eip.collector.public_ip }
