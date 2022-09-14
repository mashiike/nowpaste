#######################################################
# IAM Role
#######################################################

resource "aws_iam_role" "nowpaste" {
  name        = "NowpasteLambda"
  path        = "/"
  description = "github.com/mashiike/nowpaste lambda function iam role"

  assume_role_policy = jsonencode(
    {
      Version = "2012-10-17"
      Statement = [
        {
          Action = "sts:AssumeRole"
          Effect = "Allow"
          Principal = {
            Service = "lambda.amazonaws.com"
          }
          Sid = ""
        },
      ]
    }
  )
}

resource "aws_iam_role_policy_attachment" "nowpaste-attach-AWSLambdaBasicExecutionRole" {
  role       = aws_iam_role.nowpaste.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "nowpaste-attach-AmazonSSMReadOnlyAccess" {
  role       = aws_iam_role.nowpaste.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMReadOnlyAccess"
}

#######################################################
# lambda Function
#######################################################

data "archive_file" "nowpaste_dummy" {
  type        = "zip"
  output_path = "${path.module}/nowpaste_dummy.zip"
  source {
    content  = "nowpaste_dummy"
    filename = "bootstrap"
  }
  depends_on = [
    null_resource.nowpaste_dummy
  ]
}

resource "null_resource" "nowpaste_dummy" {}

resource "aws_lambda_function" "nowpaste" {
  lifecycle {
    ignore_changes = all
  }

  function_name = "nowpaste"
  role          = aws_iam_role.nowpaste.arn

  handler  = "bootstrap"
  runtime  = "provided.al2"
  filename = data.archive_file.nowpaste_dummy.output_path
}

resource "aws_lambda_alias" "nowpaste_current" {
  lifecycle {
    ignore_changes = all
  }
  name             = "current"
  function_name    = aws_lambda_function.nowpaste.arn
  function_version = aws_lambda_function.nowpaste.version
}

resource "aws_lambda_function_url" "nowpaste" {
  function_name      = aws_lambda_function.nowpaste.function_name
  qualifier          = aws_lambda_alias.nowpaste_current.name
  authorization_type = "NONE"

  cors {
    allow_credentials = true
    allow_origins     = ["*"]
    allow_methods     = ["POST"]
    expose_headers    = ["keep-alive", "date"]
    max_age           = 0
  }
}

#######################################################
# SSM Parameter
#######################################################

resource "aws_ssm_parameter" "nowpaste_slack_token" {
  name        = "/nowpaste/SLACK_TOKEN"
  description = "slack token for nowpaste"
  type        = "SecureString"
  value       = local.NOWPASTE_SLACK_TOKEN
}

#######################################################
# SNS
#######################################################

resource "aws_sns_topic" "nowpaste" {
  name = "nowpaste"
}

resource "aws_sns_topic_subscription" "nowpaste" {
  topic_arn            = aws_sns_topic.nowpaste.arn
  protocol             = "https"
  raw_message_delivery = false
  endpoint             = "${aws_lambda_function_url.nowpaste.function_url}amazon-sns/${local.SLACK_CHANNEL}"
}
