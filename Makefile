OS = Linux
VERSION = 0.0.1
ROOT_PACKAGE=github.com/hoorayman/popple

CURDIR = $(shell pwd)
SOURCEDIR = $(CURDIR)
COVER = $($3)

ECHO = echo
RM = rm -rf
MKDIR = mkdir

CLIENT = github.com/hoorayman/popple/cmd/popcli

.PHONY: test grpc

default: test lint vet

test:
	go test -v ./...

race:
	go test -cover=true -race $(PACKAGES)

# http://golang.org/cmd/go/#hdr-Run_gofmt_on_package_sources
fmt:
	go fmt ./...

# https://godoc.org/golang.org/x/tools/cmd/goimports
imports:
	goimports -e -d -w -local $(ROOT_PACKAGE) ./

# https://github.com/golang/lint
# go get github.com/golang/lint/golint
lint:
	golint ./...

# http://godoc.org/code.google.com/p/go.tools/cmd/vet
# go get code.google.com/p/go.tools/cmd/vet
vet:
	go vet ./...

tidy:
	go mod tidy

all: test

grpc:
	sh ./scripts/grpc.sh

#grpc-go:
#	bash ./scripts/grpc-go.sh
#

init:
	bash ./scripts/init.sh

BUILD_PATH = $(shell if [ "$(CI_DEST_DIR)" != "" ]; then echo "$(CI_DEST_DIR)" ; else echo "$(PWD)"; fi)

build:
	@$(ECHO) "Will build on "$(BUILD_PATH)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -ldflags "-w -s" -v -o $(BUILD_PATH)/bin/${MODULE} $(ROOT_PACKAGE)

client:
	@$(ECHO) "Will build on "$(BUILD_PATH)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -ldflags "-w -s" -v -o $(BUILD_PATH)/bin/${MODULE} ${CLIENT}

help:
	@$(ECHO) "Targets:"
	@$(ECHO) "all					- test"
	@$(ECHO) "test					- run all unit tests"
	@$(ECHO) "race					- run all unit tests in race condition"
	@$(ECHO) "fmt					- run go fmt command to format code"
	@$(ECHO) "lint					- run go lint command to check code style"
	@$(ECHO) "vet					- run go vet command to check code errors"
	@$(ECHO) "build				    - build and exports using CI_DEST_DIR"
	@$(ECHO) "grpc					- run 'grpc-gen-pb' and 'grpc-go-mock'"
	@$(ECHO) "init					- init the project"
