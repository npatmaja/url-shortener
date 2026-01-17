# Infrastructure as Code

## Overview

This document provides Terraform configurations for deploying the URL Shortener to AWS and GCP. Both configurations follow these principles:

1. **Serverless compute** - No servers to manage
2. **Managed storage** - DynamoDB (AWS) or Firestore (GCP)
3. **Principle of Least Privilege** - Minimal IAM permissions
4. **Infrastructure security** - Non-root containers, encryption at rest

## AWS Infrastructure

### Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              AWS                                         │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│   ┌───────────────┐     ┌───────────────┐     ┌───────────────────┐    │
│   │  CloudWatch   │     │  API Gateway  │     │   Lambda          │    │
│   │    Logs       │◀────│   HTTP API    │────▶│   (Go Runtime)    │    │
│   └───────────────┘     └───────────────┘     └─────────┬─────────┘    │
│                                                          │              │
│                                                          ▼              │
│                                               ┌───────────────────┐    │
│                                               │    DynamoDB       │    │
│                                               │  - On-demand      │    │
│                                               │  - TTL enabled    │    │
│                                               │  - Encrypted      │    │
│                                               └───────────────────┘    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### Terraform Configuration (AWS)

```hcl
# terraform/aws/main.tf

terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# -----------------------------------------------------------------------------
# Variables
# -----------------------------------------------------------------------------

variable "aws_region" {
  description = "AWS region for deployment"
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Environment name (e.g., prod, staging)"
  type        = string
  default     = "prod"
}

variable "service_name" {
  description = "Name of the service"
  type        = string
  default     = "url-shortener"
}

locals {
  name_prefix = "${var.service_name}-${var.environment}"
}

# -----------------------------------------------------------------------------
# DynamoDB Table
# -----------------------------------------------------------------------------

resource "aws_dynamodb_table" "urls" {
  name         = "${local.name_prefix}-urls"
  billing_mode = "PAY_PER_REQUEST"  # On-demand capacity
  hash_key     = "short_code"

  attribute {
    name = "short_code"
    type = "S"
  }

  attribute {
    name = "expires_at"
    type = "N"
  }

  # TTL for automatic expiration
  ttl {
    attribute_name = "expires_at"
    enabled        = true
  }

  # Global Secondary Index for expiration queries
  global_secondary_index {
    name            = "expires_at-index"
    hash_key        = "expires_at"
    projection_type = "KEYS_ONLY"
  }

  # Encryption at rest
  server_side_encryption {
    enabled = true
  }

  # Point-in-time recovery
  point_in_time_recovery {
    enabled = true
  }

  tags = {
    Name        = "${local.name_prefix}-urls"
    Environment = var.environment
    Service     = var.service_name
  }
}

# -----------------------------------------------------------------------------
# IAM Role for Lambda
# -----------------------------------------------------------------------------

resource "aws_iam_role" "lambda_execution" {
  name = "${local.name_prefix}-lambda-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })

  tags = {
    Name        = "${local.name_prefix}-lambda-role"
    Environment = var.environment
  }
}

# Principle of Least Privilege: Only DynamoDB operations needed
resource "aws_iam_role_policy" "lambda_dynamodb" {
  name = "${local.name_prefix}-dynamodb-policy"
  role = aws_iam_role.lambda_execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:UpdateItem",
          "dynamodb:DeleteItem",
          "dynamodb:Query"
        ]
        Resource = [
          aws_dynamodb_table.urls.arn,
          "${aws_dynamodb_table.urls.arn}/index/*"
        ]
      }
    ]
  })
}

# CloudWatch Logs permissions
resource "aws_iam_role_policy_attachment" "lambda_logs" {
  role       = aws_iam_role.lambda_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# -----------------------------------------------------------------------------
# Lambda Function
# -----------------------------------------------------------------------------

resource "aws_lambda_function" "api" {
  function_name = "${local.name_prefix}-api"
  role          = aws_iam_role.lambda_execution.arn
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  architectures = ["arm64"]  # Cost-effective ARM

  filename         = "${path.module}/function.zip"
  source_code_hash = filebase64sha256("${path.module}/function.zip")

  memory_size = 256
  timeout     = 10

  environment {
    variables = {
      DYNAMODB_TABLE = aws_dynamodb_table.urls.name
      BASE_URL       = "https://${aws_apigatewayv2_api.api.id}.execute-api.${var.aws_region}.amazonaws.com"
      LOG_LEVEL      = "info"
    }
  }

  tags = {
    Name        = "${local.name_prefix}-api"
    Environment = var.environment
  }
}

# -----------------------------------------------------------------------------
# API Gateway (HTTP API)
# -----------------------------------------------------------------------------

resource "aws_apigatewayv2_api" "api" {
  name          = "${local.name_prefix}-api"
  protocol_type = "HTTP"

  cors_configuration {
    allow_origins = ["*"]
    allow_methods = ["GET", "POST", "OPTIONS"]
    allow_headers = ["Content-Type"]
    max_age       = 300
  }

  tags = {
    Name        = "${local.name_prefix}-api"
    Environment = var.environment
  }
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.api.id
  name        = "$default"
  auto_deploy = true

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api_gateway.arn
    format = jsonencode({
      requestId      = "$context.requestId"
      requestTime    = "$context.requestTime"
      httpMethod     = "$context.httpMethod"
      path           = "$context.path"
      status         = "$context.status"
      responseLength = "$context.responseLength"
      # Note: No IP address logged (privacy by design)
    })
  }
}

resource "aws_apigatewayv2_integration" "lambda" {
  api_id             = aws_apigatewayv2_api.api.id
  integration_type   = "AWS_PROXY"
  integration_uri    = aws_lambda_function.api.invoke_arn
  integration_method = "POST"
}

resource "aws_apigatewayv2_route" "shorten" {
  api_id    = aws_apigatewayv2_api.api.id
  route_key = "POST /shorten"
  target    = "integrations/${aws_apigatewayv2_integration.lambda.id}"
}

resource "aws_apigatewayv2_route" "redirect" {
  api_id    = aws_apigatewayv2_api.api.id
  route_key = "GET /s/{code}"
  target    = "integrations/${aws_apigatewayv2_integration.lambda.id}"
}

resource "aws_apigatewayv2_route" "stats" {
  api_id    = aws_apigatewayv2_api.api.id
  route_key = "GET /stats/{code}"
  target    = "integrations/${aws_apigatewayv2_integration.lambda.id}"
}

resource "aws_apigatewayv2_route" "health" {
  api_id    = aws_apigatewayv2_api.api.id
  route_key = "GET /health"
  target    = "integrations/${aws_apigatewayv2_integration.lambda.id}"
}

# Lambda permission for API Gateway
resource "aws_lambda_permission" "api_gateway" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.api.execution_arn}/*/*"
}

# -----------------------------------------------------------------------------
# CloudWatch Log Groups
# -----------------------------------------------------------------------------

resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/${aws_lambda_function.api.function_name}"
  retention_in_days = 30

  tags = {
    Name        = "${local.name_prefix}-lambda-logs"
    Environment = var.environment
  }
}

resource "aws_cloudwatch_log_group" "api_gateway" {
  name              = "/aws/apigateway/${local.name_prefix}"
  retention_in_days = 30

  tags = {
    Name        = "${local.name_prefix}-apigw-logs"
    Environment = var.environment
  }
}

# -----------------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------------

output "api_endpoint" {
  description = "API Gateway endpoint URL"
  value       = aws_apigatewayv2_api.api.api_endpoint
}

output "dynamodb_table_name" {
  description = "DynamoDB table name"
  value       = aws_dynamodb_table.urls.name
}

output "lambda_function_name" {
  description = "Lambda function name"
  value       = aws_lambda_function.api.function_name
}
```

---

## GCP Infrastructure

### Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              GCP                                         │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│   ┌───────────────┐     ┌───────────────┐     ┌───────────────────┐    │
│   │  Cloud        │     │  Cloud Load   │     │   Cloud Run       │    │
│   │  Logging      │◀────│  Balancer     │────▶│   (Container)     │    │
│   └───────────────┘     └───────────────┘     └─────────┬─────────┘    │
│                                                          │              │
│                                                          ▼              │
│                                               ┌───────────────────┐    │
│                                               │    Firestore      │    │
│                                               │  - Native mode    │    │
│                                               │  - TTL policies   │    │
│                                               └───────────────────┘    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### Terraform Configuration (GCP)

```hcl
# terraform/gcp/main.tf

terraform {
  required_version = ">= 1.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# -----------------------------------------------------------------------------
# Variables
# -----------------------------------------------------------------------------

variable "project_id" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "prod"
}

variable "service_name" {
  description = "Name of the service"
  type        = string
  default     = "url-shortener"
}

locals {
  name_prefix = "${var.service_name}-${var.environment}"
}

# -----------------------------------------------------------------------------
# Enable Required APIs
# -----------------------------------------------------------------------------

resource "google_project_service" "run" {
  service            = "run.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "firestore" {
  service            = "firestore.googleapis.com"
  disable_on_destroy = false
}

resource "google_project_service" "artifactregistry" {
  service            = "artifactregistry.googleapis.com"
  disable_on_destroy = false
}

# -----------------------------------------------------------------------------
# Firestore Database
# -----------------------------------------------------------------------------

resource "google_firestore_database" "main" {
  project     = var.project_id
  name        = "(default)"
  location_id = var.region
  type        = "FIRESTORE_NATIVE"

  depends_on = [google_project_service.firestore]
}

# TTL Policy for automatic expiration
resource "google_firestore_field" "urls_expires_at" {
  project    = var.project_id
  database   = google_firestore_database.main.name
  collection = "urls"
  field      = "expires_at"

  ttl_config {}

  depends_on = [google_firestore_database.main]
}

# Index for expiration queries
resource "google_firestore_index" "urls_expires_at" {
  project    = var.project_id
  database   = google_firestore_database.main.name
  collection = "urls"

  fields {
    field_path = "expires_at"
    order      = "ASCENDING"
  }

  depends_on = [google_firestore_database.main]
}

# -----------------------------------------------------------------------------
# Artifact Registry
# -----------------------------------------------------------------------------

resource "google_artifact_registry_repository" "main" {
  location      = var.region
  repository_id = local.name_prefix
  format        = "DOCKER"

  depends_on = [google_project_service.artifactregistry]
}

# -----------------------------------------------------------------------------
# Service Account (Principle of Least Privilege)
# -----------------------------------------------------------------------------

resource "google_service_account" "cloud_run" {
  account_id   = "${local.name_prefix}-run"
  display_name = "Cloud Run Service Account for ${var.service_name}"
}

# Firestore permissions only
resource "google_project_iam_member" "firestore_user" {
  project = var.project_id
  role    = "roles/datastore.user"
  member  = "serviceAccount:${google_service_account.cloud_run.email}"
}

# -----------------------------------------------------------------------------
# Cloud Run Service
# -----------------------------------------------------------------------------

resource "google_cloud_run_v2_service" "api" {
  name     = local.name_prefix
  location = var.region

  template {
    service_account = google_service_account.cloud_run.email

    scaling {
      min_instance_count = 0
      max_instance_count = 100
    }

    containers {
      image = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.main.repository_id}/${var.service_name}:latest"

      resources {
        limits = {
          cpu    = "1"
          memory = "256Mi"
        }
        cpu_idle = true  # Scale to zero
      }

      env {
        name  = "GCP_PROJECT"
        value = var.project_id
      }

      env {
        name  = "FIRESTORE_COLLECTION"
        value = "urls"
      }

      env {
        name  = "LOG_LEVEL"
        value = "info"
      }

      startup_probe {
        http_get {
          path = "/health"
        }
        initial_delay_seconds = 0
        period_seconds        = 10
        failure_threshold     = 3
      }

      liveness_probe {
        http_get {
          path = "/health"
        }
        period_seconds = 30
      }
    }
  }

  traffic {
    type    = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
    percent = 100
  }

  depends_on = [
    google_project_service.run,
    google_artifact_registry_repository.main
  ]
}

# Allow unauthenticated access (public API)
resource "google_cloud_run_v2_service_iam_member" "public" {
  project  = var.project_id
  location = var.region
  name     = google_cloud_run_v2_service.api.name
  role     = "roles/run.invoker"
  member   = "allUsers"
}

# -----------------------------------------------------------------------------
# Outputs
# -----------------------------------------------------------------------------

output "service_url" {
  description = "Cloud Run service URL"
  value       = google_cloud_run_v2_service.api.uri
}

output "artifact_registry" {
  description = "Artifact Registry repository"
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.main.repository_id}"
}
```

---

## Deployment Commands

### AWS

```bash
cd terraform/aws

# Initialize
terraform init

# Plan
terraform plan -var="environment=prod"

# Apply
terraform apply -var="environment=prod"
```

### GCP

```bash
cd terraform/gcp

# Initialize
terraform init

# Plan
terraform plan -var="project_id=my-project" -var="environment=prod"

# Apply
terraform apply -var="project_id=my-project" -var="environment=prod"
```

## Security Highlights

| Feature | AWS | GCP |
|---------|-----|-----|
| **Encryption at rest** | DynamoDB default encryption | Firestore default encryption |
| **IAM least privilege** | Custom policy with only needed actions | `roles/datastore.user` only |
| **No IP logging** | Configured in API Gateway logs | Cloud Run default behavior |
| **Container security** | Lambda managed | Non-root user in Dockerfile |
| **Network isolation** | VPC optional | VPC connector optional |
