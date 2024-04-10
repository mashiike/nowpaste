
export AWS_REGION := ap-northeast-1
export AWS_DEFAULT_REGION := $(AWS_REGION)
export GOOS := linux
export GOARCH := amd64

.PHONY: all
all: plan

.PHONY: help
help:
	@make2help

.PHONY: init
init:
	terraform providers lock \
		-platform=darwin_amd64 \
		-platform=darwin_arm64 \
		-platform=linux_amd64 \
		-platform=linux_arm64 \
		-platform=windows_amd64
	terraform init

.PHONY: apply
apply:
	terraform apply

.PHONY: plan
plan:
	terraform plan

download/%:
	curl -sL https://github.com/mashiike/nowpaste/releases/download/v$*/nowpaste_$*_${GOOS}_${GOARCH}.tar.gz -o  nowpaste_$*_${GOOS}_${GOARCH}.tar.gz
	tar xf nowpaste_$*_${GOOS}_${GOARCH}.tar.gz -C ./
	mv ./nowpaste $@

bootstrap: ../cmd/nowpaste/*.go ../*.go ../go.*
	go build -o bootstrap ../cmd/nowpaste/main.go

.PHONY: plan
deploy: bootstrap
	lambroll deploy --tfstate .terraform/terraform.tfstate

logs:
	lambroll logs --follow --format=short --tfstate .terraform/terraform.tfstate

clean:
	@rm -f bootstrap
	@rm -f nowpaste_*.tar.gz

send/%:
	cat sample.txt | curl -d @- -H "Content-Type: text/plain" $(shell tfstate-lookup --state .terraform/terraform.tfstate aws_lambda_function_url.nowpaste.function_url)?code_block_text=true\&channel=$*

send_as_file/%:
	cat sample.txt | curl -d @- -H "Content-Type: text/plain" $(shell tfstate-lookup --state .terraform/terraform.tfstate aws_lambda_function_url.nowpaste.function_url)?as_file=true\&channel=$*
