PACKAGE := tkestack.io/aia-ip-controller

# Image URL to use all building/pushing image targets
REGISTRY_PREFIX := ccr.ccs.tencentyun.com/tkeimages

GO_LOCAL_VERSION = $(shell go version | cut -f3 -d' ' | cut -c 3-)

ifeq ($(origin GO_LOCAL_VERSION),undefined)
GO_LOCAL_VERSION := 1.15
endif

AIA_IP_CONTROLLER_NAME := aia-ip-controller
AIA_IP_CONTROLLER_BIN := target/${AIA_IP_CONTROLLER_NAME}

GIT_COMMIT:=$(shell git rev-parse "HEAD^{commit}" 2>/dev/null)

# the raw git version from `git describe` -- our starting point
GIT_VERSION_RAW:=$(shell git describe --tags --abbrev=14 "$(GIT_COMMIT)^{commit}" 2>/dev/null)

# use the number of dashes in the raw version to figure out what kind of
# version this is, and turn it into a semver-compatible version
DASHES_IN_VERSION:=$(shell echo "$(GIT_VERSION_RAW)" | sed "s/[^-]//g")

# just use the raw version by default
GIT_VERSION:=$(GIT_VERSION_RAW)

ifeq ($(DASHES_IN_VERSION), ---)
# we have a distance to a subversion (v1.1.0-subversion-1-gCommitHash)
GIT_VERSION:=$(shell echo "$(GIT_VERSION_RAW)" | sed "s/-\([0-9]\{1,\}\)-g\([0-9a-f]\{14\}\)$$/.\1\+\2/")
endif
ifeq ($(DASHES_IN_VERSION), --)
# we have distance to base tag (v1.1.0-1-gCommitHash)
GIT_VERSION:=$(shell echo "$(GIT_VERSION_RAW)" | sed "s/-g\([0-9a-f]\{14\}\)$$/+\1/")
endif

# figure out if we have new or changed files
ifeq ($(shell git status --porcelain 2>/dev/null),)
GIT_TREE_STATE:=clean
else
# append the -dirty manually, since `git describe --dirty` only considers
# changes to existing files
GIT_TREE_STATE:=dirty
GIT_VERSION:=$(GIT_VERSION)-dirty
endif

# construct a "shorter" version without the commit info, etc for use as container image tag, etc
VERSION?=$(shell echo "$(GIT_VERSION)" | grep -E -o '^v[[:digit:]]+\.[[:digit:]]+\.[[:digit:]]+(-(alpha|beta)\.[[:digit:]]+)?')

# construct the build date, taking into account SOURCE_DATE_EPOCH, which is
# used for the purpose of reproducible builds
ifdef SOURCE_DATE_EPOCH
BUILD_DATE:=$(shell date --date=@${SOURCE_DATE_EPOCH} -u +'%Y-%m-%dT%H:%M:%SZ')
else
BUILD_DATE:=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
endif

VERSION_PACKAGE := $(PACKAGE)/pkg/version
# set the build information version ldflags (but not other ldflags)
VERSION_LDFLAGS := -X $(VERSION_PACKAGE).gitVersion=$(GIT_VERSION) -X $(VERSION_PACKAGE).gitCommit=$(GIT_COMMIT) -X $(VERSION_PACKAGE).gitTreeState=$(GIT_TREE_STATE) -X $(VERSION_PACKAGE).buildDate=$(BUILD_DATE)

.PHONY: all
all: clean build

.PHONY: build
build: # @HELP build binaries
build: $(AIA_IP_CONTROLLER_BIN)

clean: # @HELP removes built binaries and temporary files
clean: bin-clean

.PHONY: image
image: # @HELP built image for aia-ip-controller
image: clean build image-build

image-build:
	@echo "building image '$(VERSION)'"
	@docker build --build-arg GO_VER=$(GO_LOCAL_VERSION) --build-arg ROOT_PACKAGE=$(PACKAGE) --build-arg VERSION=$(VERSION) --rm --no-cache --pull -t $(REGISTRY_PREFIX)/$(AIA_IP_CONTROLLER_NAME):$(VERSION) -f hack/docker/aia-ip-controller.dockerfile .


bin-clean:
	@rm -rf target

$(AIA_IP_CONTROLLER_BIN):
	@echo "Building aia-ip-controller binary '$(VERSION)'"
	@mkdir -p target
	@CGO_ENABLED=0 GOOS=linux go build \
		-o target/aia-ip-controller \
		-ldflags "$(VERSION_LDFLAGS)" \
		cmd/aia-ip-controller/*.go

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

help: # @HELP prints this message
help:
	@echo "TARGETS:"
	@grep -E '^.*: *# *@HELP' $(MAKEFILE_LIST)    \
	    | awk '                                   \
	        BEGIN {FS = ": *# *@HELP"};           \
	        { printf "  %-30s %s\n", $$1, $$2 };  \
	    '
