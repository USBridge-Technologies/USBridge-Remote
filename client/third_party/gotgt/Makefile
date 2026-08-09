BIN_DIR=_output/cmd/bin
REPO_PATH="github.com/gostor/gotgt"
REL_OSARCH="linux/amd64"
GitSHA=`git rev-parse HEAD`
Date=`date "+%Y-%m-%d %H:%M:%S"`
RELEASE_VERSION=$(shell git describe --tags --always --dirty)
IMG_BUILDER=docker
LD_FLAGS=" \
    -X '${REPO_PATH}/pkg/version.GitSHA=${GitSHA}' \
    -X '${REPO_PATH}/pkg/version.Built=${Date}'   \
    -X '${REPO_PATH}/pkg/version.Version=${RELEASE_VERSION}'"

all: init build

build: init
	go build -ldflags ${LD_FLAGS} -o ${BIN_DIR}/gotgt gotgt.go

build-nocgo: init
	CGO_ENABLED=0 go build -ldflags ${LD_FLAGS} -o ${BIN_DIR}/gotgt gotgt.go

verify:
	hack/verify-gofmt.sh

init:
	mkdir -p ${BIN_DIR}
clean:
	rm -fr ${BIN_DIR}

.PHONY: clean

