variable "serverless_package" {
  type        = string
  nullable    = false
  description = "Absolute path to the tested Lambda zip. A zip that is not byte-identical to the deployed package updates all three functions."
}
variable "serverless_schedules_enabled" { default = false }
variable "serverless_read_token_parameter" {
  type        = string
  default     = "/redis-adbc-analytics/read-token"
  description = "Pre-created SecureString containing the existing dashboard token. Never put its value in tfvars."
}

module "serverless" {
  source               = "./serverless"
  bucket               = aws_s3_bucket.backups.id
  package              = var.serverless_package
  hostname             = var.hostname
  hosted_zone_id       = var.hosted_zone_id
  read_token_parameter = var.serverless_read_token_parameter
  schedules_enabled    = var.serverless_schedules_enabled
}

# The module was optional while the legacy collector served traffic.
moved {
  from = module.serverless[0]
  to   = module.serverless
}

output "serverless_staging_url" { value = module.serverless.staging_url }
output "serverless_refresh_function" { value = module.serverless.refresh_function }
