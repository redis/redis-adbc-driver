terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 6.0" }
  }
}

variable "bucket" { type = string }
variable "package" { type = string }
variable "hostname" { type = string }
variable "hosted_zone_id" { type = string }
variable "read_token_parameter" { type = string }
variable "schedules_enabled" { default = false }
variable "baseline_required" { default = true }

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

locals {
  name   = "redis-adbc-telemetry-s3"
  prefix = "serverless/"
  functions = {
    api       = { handler = "serverless.api", timeout = 10, memory = 256, concurrency = 10 }
    refresh   = { handler = "serverless.refresh", timeout = 900, memory = 2048, concurrency = 1 }
    downloads = { handler = "serverless.downloads", timeout = 300, memory = 256, concurrency = 1 }
  }
  bucket_arn = "arn:${data.aws_partition.current.partition}:s3:::${var.bucket}"
}

resource "aws_iam_role" "lambda" {
  for_each = local.functions
  name     = "${local.name}-${each.key}"
  assume_role_policy = jsonencode({ Version = "2012-10-17", Statement = [{
    Effect = "Allow", Principal = { Service = "lambda.amazonaws.com" }, Action = "sts:AssumeRole"
  }] })
}

resource "aws_cloudwatch_log_group" "lambda" {
  for_each          = local.functions
  name              = "/aws/lambda/${local.name}-${each.key}"
  retention_in_days = 14
}

resource "aws_iam_role_policy" "lambda" {
  for_each = local.functions
  role     = aws_iam_role.lambda[each.key].id
  policy = jsonencode({ Version = "2012-10-17", Statement = concat([
    { Effect = "Allow", Action = ["logs:CreateLogStream", "logs:PutLogEvents"], Resource = "${aws_cloudwatch_log_group.lambda[each.key].arn}:*" }
    ], jsondecode(each.key == "api" ? jsonencode([
      { Effect = "Allow", Action = ["s3:PutObject"], Resource = "${local.bucket_arn}/${local.prefix}reports/*" },
      { Effect = "Allow", Action = ["s3:GetObject"], Resource = ["${local.bucket_arn}/${local.prefix}manifest.json", "${local.bucket_arn}/${local.prefix}generations/*/summaries.json"] },
      { Effect = "Allow", Action = ["ssm:GetParameter"], Resource = "arn:${data.aws_partition.current.partition}:ssm:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:parameter${var.read_token_parameter}" }
      ]) : each.key == "refresh" ? jsonencode([
      { Effect = "Allow", Action = ["s3:ListBucket"], Resource = local.bucket_arn },
      { Effect = "Allow", Action = ["s3:GetObject"], Resource = "${local.bucket_arn}/${local.prefix}*" },
      { Effect = "Allow", Action = ["s3:PutObject"], Resource = ["${local.bucket_arn}/${local.prefix}manifest.json", "${local.bucket_arn}/${local.prefix}generations/*"] },
      { Effect = "Allow", Action = ["s3:DeleteObject"], Resource = "${local.bucket_arn}/${local.prefix}generations/*" }
      ]) : jsonencode([
      { Effect = "Allow", Action = ["s3:PutObject"], Resource = "${local.bucket_arn}/${local.prefix}downloads/*" }
  ]))) })
}

resource "aws_lambda_function" "collector" {
  for_each                       = local.functions
  function_name                  = "${local.name}-${each.key}"
  role                           = aws_iam_role.lambda[each.key].arn
  runtime                        = "python3.12"
  architectures                  = ["arm64"]
  handler                        = each.value.handler
  filename                       = var.package
  source_code_hash               = filebase64sha256(var.package)
  timeout                        = each.value.timeout
  memory_size                    = each.value.memory
  reserved_concurrent_executions = each.value.concurrency
  environment {
    variables = {
      ANALYTICS_BUCKET               = var.bucket
      ANALYTICS_PREFIX               = local.prefix
      ANALYTICS_READ_TOKEN_PARAMETER = var.read_token_parameter
      ANALYTICS_BASELINE_REQUIRED    = tostring(var.baseline_required)
    }
  }
  depends_on = [aws_iam_role_policy.lambda]
}

resource "aws_apigatewayv2_api" "collector" {
  name          = local.name
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "collector" {
  api_id                 = aws_apigatewayv2_api.collector.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.collector["api"].invoke_arn
  payload_format_version = "2.0"
  timeout_milliseconds   = 10000
}

resource "aws_apigatewayv2_route" "collector" {
  for_each  = toset(["POST /v1/usage", "GET /", "GET /dashboard.js", "GET /dashboard.css", "GET /health", "GET /api/summary"])
  api_id    = aws_apigatewayv2_api.collector.id
  route_key = each.value
  target    = "integrations/${aws_apigatewayv2_integration.collector.id}"
}

resource "aws_apigatewayv2_stage" "collector" {
  api_id      = aws_apigatewayv2_api.collector.id
  name        = "$default"
  auto_deploy = true
  # Global throttling bounds public ingestion. Unlike nginx's previous policy,
  # this is shared across clients, not per IP. No access logging is configured.
  default_route_settings {
    throttling_burst_limit = 20
    throttling_rate_limit  = 10
  }
}

resource "aws_lambda_permission" "api" {
  statement_id  = "AllowAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.collector["api"].function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.collector.execution_arn}/*/*"
}

resource "aws_acm_certificate" "collector" {
  domain_name       = var.hostname
  validation_method = "DNS"
  lifecycle { create_before_destroy = true }
}

resource "aws_route53_record" "certificate" {
  for_each = {
    for option in aws_acm_certificate.collector.domain_validation_options : option.domain_name => {
      name = option.resource_record_name, record = option.resource_record_value, type = option.resource_record_type
    }
  }
  zone_id = var.hosted_zone_id
  name    = each.value.name
  type    = each.value.type
  ttl     = 60
  records = [each.value.record]
}

resource "aws_acm_certificate_validation" "collector" {
  certificate_arn         = aws_acm_certificate.collector.arn
  validation_record_fqdns = [for record in aws_route53_record.certificate : record.fqdn]
}

resource "aws_apigatewayv2_domain_name" "collector" {
  domain_name = var.hostname
  domain_name_configuration {
    certificate_arn = aws_acm_certificate_validation.collector.certificate_arn
    endpoint_type   = "REGIONAL"
    security_policy = "TLS_1_2"
  }
}

resource "aws_apigatewayv2_api_mapping" "collector" {
  api_id      = aws_apigatewayv2_api.collector.id
  domain_name = aws_apigatewayv2_domain_name.collector.id
  stage       = aws_apigatewayv2_stage.collector.id
}

resource "aws_cloudwatch_event_rule" "schedule" {
  for_each            = { refresh = "rate(15 minutes)", downloads = "rate(1 day)" }
  name                = "${local.name}-${each.key}"
  schedule_expression = each.value
  state               = var.schedules_enabled ? "ENABLED" : "DISABLED"
}

resource "aws_cloudwatch_event_target" "schedule" {
  for_each = aws_cloudwatch_event_rule.schedule
  rule     = each.value.name
  arn      = aws_lambda_function.collector[each.key].arn
}

resource "aws_lambda_permission" "schedule" {
  for_each      = aws_cloudwatch_event_rule.schedule
  statement_id  = "AllowScheduledInvocation"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.collector[each.key].function_name
  principal     = "events.amazonaws.com"
  source_arn    = each.value.arn
}

resource "aws_cloudwatch_metric_alarm" "worker_errors" {
  alarm_name          = "${local.name}-summary-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 900
  statistic           = "Sum"
  threshold           = 0
  treat_missing_data  = "notBreaching"
  dimensions          = { FunctionName = aws_lambda_function.collector["refresh"].function_name }
  # No notification destination is invented. Wire the operator's existing alarm
  # destination when available; the alarm remains visible in CloudWatch.
}

output "staging_url" { value = aws_apigatewayv2_api.collector.api_endpoint }
output "domain_target" { value = aws_apigatewayv2_domain_name.collector.domain_name_configuration[0].target_domain_name }
output "domain_zone_id" { value = aws_apigatewayv2_domain_name.collector.domain_name_configuration[0].hosted_zone_id }
output "refresh_function" { value = aws_lambda_function.collector["refresh"].function_name }
