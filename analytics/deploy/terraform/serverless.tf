variable "serverless_package" {
  type        = string
  default     = null
  nullable    = true
  description = "Absolute path to the tested Lambda zip; null preserves the legacy deployment."
}
variable "serverless_cutover" {
  default     = false
  description = "Switch the existing DNS record only after importing and checking historical data."
}
variable "serverless_schedules_enabled" { default = false }
variable "serverless_stop_legacy" {
  default     = false
  description = "After migration and DNS-cache drain, stop legacy compute and release its unused Elastic IP; retain its disk for rollback."
  validation {
    condition     = !var.serverless_stop_legacy || var.serverless_cutover
    error_message = "Verify and switch serverless DNS before stopping the legacy host."
  }
}
variable "serverless_migration_access" {
  default     = false
  description = "Temporary, scoped legacy-host access for the one-time migration. Turn off after verification."
}
variable "serverless_read_token_parameter" {
  type        = string
  default     = "/redis-adbc-analytics/read-token"
  description = "Pre-created SecureString containing the existing dashboard token. Never put its value in tfvars."
}

module "serverless" {
  source               = "./serverless"
  count                = var.serverless_package == null ? 0 : 1
  bucket               = aws_s3_bucket.backups.id
  package              = var.serverless_package
  hostname             = var.hostname
  hosted_zone_id       = var.hosted_zone_id
  read_token_parameter = var.serverless_read_token_parameter
  schedules_enabled    = var.serverless_schedules_enabled
}

output "serverless_staging_url" { value = try(module.serverless[0].staging_url, null) }
output "serverless_refresh_function" { value = try(module.serverless[0].refresh_function, null) }

moved {
  from = aws_eip.collector
  to   = aws_eip.collector[0]
}

resource "aws_ec2_instance_state" "legacy" {
  count       = var.serverless_stop_legacy ? 1 : 0
  instance_id = aws_instance.collector.id
  state       = "stopped"
}

resource "aws_iam_role_policy" "serverless_migration" {
  count = var.serverless_migration_access ? 1 : 0
  name  = "one-time-serverless-migration"
  role  = aws_iam_role.collector.id
  policy = jsonencode({ Version = "2012-10-17", Statement = [
    { Effect = "Allow", Action = ["s3:ListBucket"], Resource = aws_s3_bucket.backups.arn },
    { Effect = "Allow", Action = ["s3:GetObject"], Resource = ["${aws_s3_bucket.backups.arn}/serverless/manifest.json", "${aws_s3_bucket.backups.arn}/backups/serverless-migration/code.zip"] },
    { Effect = "Allow", Action = ["s3:PutObject"], Resource = "${aws_s3_bucket.backups.arn}/serverless/baseline.json.gz" },
    { Effect = "Allow", Action = ["ssm:PutParameter"], Resource = "arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter${var.serverless_read_token_parameter}" }
  ] })
}
