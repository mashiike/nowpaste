# nowpaste

[![Documentation](https://godoc.org/github.com/mashiike/nowpaste?status.svg)](https://godoc.org/github.com/mashiike/nowpaste)
![Latest GitHub release](https://img.shields.io/github/release/mashiike/nowpaste.svg)
![Github Actions test](https://github.com/mashiike/nowpaste/workflows/Test/badge.svg?branch=main)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](https://github.com/mashiike/nowpaste/blob/master/LICENSE)

nowpaste is a http server for posting messages to slack

## Install 

### Binary packages

[Releases](https://github.com/mashiike/nowpaste/releases)

### as AWS Lambda function

The build binary runs as a bootstrap for AWS Lambda functions.
Rename the downloaded binary to `bootstarap` and create a zip archive. Deploy it as a Lambda function!

If a secret named `nowpaste/SLACK_TOKEN` is managed in AWS Secrets Manager and the appropriate access rights have been granted to the IAM Role, the Slack token can be passed by specifying the environment variable as follows

```
$ export NOWPASTE_SSM_NAMES=/aws/reference/secretsmanager/nowpaste/SLACK_TOKEN
```

For more information: prese refer to [lambda directory](lambda/) 

### slack token permissions

It is assumed that nowpaste is passed the bot token of the slack app.
An example of a slack app manifest is shown below.

```yaml
display_information:
  name: nowpaste
  description: http server for posting messages to slack
  background_color: "#0b50e3"
features:
  bot_user:
    display_name: nowpaste
    always_online: true
oauth_config:
  scopes:
    bot:
      - channels:join
      - chat:write
      - chat:write.public
      - chat:write.customize
      - files:write
settings:
  org_deploy_enabled: false
  socket_mode_enabled: false
  token_rotation_enabled: false
```


## Usage 

If you want to run locally, you can use the following options

```
nowpaste [options]
version: current
  -listen string
        http server run on (default ":8080")
  -log-level string
        log level (default "info")
  -path-prefix string
        endpoint path prefix (default "/")
  -slack-token string
        slack token
```

nowpaste runs http server on `http://#{listen}/` 
If you are using a Lambda Function URL, it will be `https://<url_id>.lambda-url.<region>.on.aws/`.


The simplest use case would be as follows

payload.json
```json 
{
   "channel": "<slack_channel_id>",
   "icon_emoji": ":tada:",
   "username": "nowpaste",
   "text": "Hello world!!"
}
```

```shell
$ cat payload.json  | curl "https://<url_id>.lambda-url.<region>.on.aws/" \
    -X POST \
    -H "Content-type: application/json" \
    -d @-
```

## Amazon SNS http endpoint

nowpaste can accept Amazon SNS notification messages.
Add a SNS topic http(s) endpoint to `https://<url_id>.lambda-url.<region>.on.aws/amazon-sns/{channel_id}`

## LICENSE

MIT License

Copyright (c) 2022 IKEDA Masashi
