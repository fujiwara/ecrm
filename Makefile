.PHONY: clean test test-e2e

EMULATOR_PORT ?= 9297
EMULATOR_DIR ?= $(HOME)/.aws/emulator/$(EMULATOR_PORT)
ECR_PUSH := test/cmd/ecr-push/ecr-push

ecrm: go.* *.go cmd/ecrm/main.go
	go build -o $@ cmd/ecrm/main.go

$(ECR_PUSH): go.* test/cmd/ecr-push/*.go
	go build -o $@ ./test/cmd/ecr-push

clean:
	rm -rf ecrm $(ECR_PUSH) dist/

test:
	go test -v ./...

# requires AWS Emulator (https://github.com/aws/aws-emulator) running on EMULATOR_PORT,
# runn (https://github.com/k1LoW/runn) and awslim (https://github.com/fujiwara/awslim)
test-e2e: ecrm $(ECR_PUSH)
	env -u AWS_PROFILE -u AWS_ACCESS_KEY_ID -u AWS_SECRET_ACCESS_KEY -u AWS_SESSION_TOKEN \
		AWS_ENDPOINT_URL=https://127.0.0.1:$(EMULATOR_PORT) \
		AWS_SHARED_CREDENTIALS_FILE=$(EMULATOR_DIR)/emulator_credentials \
		AWS_CONFIG_FILE=$(EMULATOR_DIR)/config \
		AWS_CA_BUNDLE=$(EMULATOR_DIR)/ca.pem \
		AWS_EC2_METADATA_DISABLED=true \
		AWS_REGION=us-east-1 \
		runn run test/e2e.yml --scopes run:exec --verbose < /dev/null

install:
	go install github.com/fujiwara/ecrm/cmd/ecrm

dist:
	goreleaser build --snapshot --clean
