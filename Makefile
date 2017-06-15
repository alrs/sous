SHELL := /usr/bin/env bash

SQLITE_URL := https://sqlite.org/2017/sqlite-autoconf-3160200.tar.gz
GO_VERSION := 1.7.3

TAG_TEST := git describe --exact-match --abbrev=0 2>/dev/null
ifeq ($(shell $(TAG_TEST) ; echo $$?), 128)
GIT_TAG := 0.0.0
else
GIT_TAG := $(shell $(TAG_TEST))
endif

# install-dev uses DESC and DATE to make a git described, timestamped dev build.
DESC := $(shell git describe)
DATE := $(shell date +%Y-%m-%dT%H-%M-%S)
DEV_VERSION := "$(DESC)-devbuild-$(DATE)"

# Sous releases are tagged with format v0.0.0. semv library
# does not understand the v prefix, so this lops it off.
SOUS_VERSION := $(shell echo $(GIT_TAG) | sed 's/^v//')

ifeq ($(shell git diff-index --quiet HEAD ; echo $$?),0)
COMMIT := $(shell git rev-parse HEAD)
else
COMMIT := DIRTY
endif

ifndef SOUS_QA_DESC
QA_DESC := `pwd`/qa_desc.json
else
QA_DESC := $(SOUS_QA_DESC)
endif

FLAGS := "-X 'main.Revision=$(COMMIT)' -X 'main.VersionString=$(SOUS_VERSION)'"
BIN_DIR := artifacts/bin
DARWIN_RELEASE_DIR := sous-darwin-amd64_$(SOUS_VERSION)
LINUX_RELEASE_DIR := sous-linux-amd64_$(SOUS_VERSION)
RELEASE_DIRS := $(DARWIN_RELEASE_DIR) $(LINUX_RELEASE_DIR)
DARWIN_TARBALL := $(DARWIN_RELEASE_DIR).tar.gz
LINUX_TARBALL := $(LINUX_RELEASE_DIR).tar.gz
CONCAT_XGO_ARGS := -go $(GO_VERSION) -branch master -deps $(SQLITE_URL) --dest $(BIN_DIR) --ldflags $(FLAGS)
COVER_DIR := /tmp/sous-cover
TEST_VERBOSE := $(if $(VERBOSE),-v,)
SOUS_PACKAGES:= $(shell go list -f '{{ range .Deps }}{{.}}{{printf "\n"}}{{end}}' | grep '^github.com/opentable' | grep -v 'vendor')
SOUS_CONTAINER_IMAGES:= "docker images | egrep '127.0.0.1:5000|testregistry_'"

help:
	@echo --- options:
	@echo make clean
	@echo "make clean-containers: Destroy and delete local testing containers."
	@echo make coverage
	@echo make legendary
	@echo "make release:  Both linux and darwin"
	@echo "make setup-containers: pull and start containers for integration testing."
	@echo "make test: all tests"
	@echo "make test-unit: unit tests"
	@echo "make test-gofmt: gofmt tests"
	@echo "make test-integration: integration tests"
	@echo "make test-staticcheck: runs static code analysis against project packages."
	@echo "make wip: puts a marker file into workspace to prevent Travis from passing the build."
	@echo
	@echo "Add VERBOSE=1 for tons of extra output."

clean:
	rm -rf $(COVER_DIR)
	git ls-files -z -o --exclude=.cleanprotect --exclude-per-directory=.cleanprotect | xargs -0 rm -rf
	rm -f $(QA_DESC)

clean-containers: clean-container-certs clean-running-containers clean-container-images

clean-container-images:
	@if (( $$("$(SOUS_CONTAINER_IMAGES)" | wc -l) > 0 )); then echo 'found docker images, deleting:'; "$(SOUS_CONTAINER_IMAGES)" | awk '{ print $$3 }' | xargs docker rmi -f; fi

clean-container-certs:
	rm -f ./integration/test-registry/docker-registry/testing.crt

clean-running-containers:
	@if (( $$(docker ps -q | wc -l) > 0 )); then echo 'found running containers, killing:'; docker ps -q | xargs docker kill; fi
	@if (( $$(docker ps -aq | wc -l) > 0 )); then echo 'found container instances, deleting:'; docker ps -aq | xargs docker rm; fi

gitlog:
	git log `git describe --abbrev=0`..HEAD

install-dev:
	brew uninstall opentable/public/sous || true
	rm "$$(which sous)" || true
	go install -ldflags "-X main.VersionString=$(DEV_VERSION)"
	echo "Now run 'hash -r && sous version' to make sure you are using the dev version of sous."

install-brew:
	rm "$$(which sous)" || true
	brew uninstall opentable/public/sous || true
	brew install opentable/public/sous
	echo "Now run 'hash -r && sous version' to make sure you are using the homebrew-installed sous."

install-ggen:
	cd bin/ggen && go install ./

install-stringer:
	go get golang.org/x/tools/cmd/stringer

install-xgo:
	go get github.com/karalabe/xgo

install-govendor:
	go get github.com/kardianos/govendor

install-engulf:
	go get github.com/nyarly/engulf

install-staticcheck:
	go get honnef.co/go/tools/cmd/staticcheck

install-build-tools: install-xgo install-govendor install-engulf install-staticcheck

release: artifacts/$(DARWIN_TARBALL) artifacts/$(LINUX_TARBALL)

linux-build: artifacts/$(LINUX_RELEASE_DIR)/sous
	ln -sf ../$< dev_support/sous_linux

semvertagchk:
	@echo "$(SOUS_VERSION)" | egrep ^[0-9]+\.[0-9]+\.[0-9]+

sous-qa-setup: ./dev_support/sous_qa_setup/*.go ./util/test_with_docker/*.go
	go build $(EXTRA_GO_FLAGS) ./dev_support/sous_qa_setup


reject-wip:
	test ! -f workinprogress

wip:
	touch workinprogress
	git add workinprogress
	git commit --squash=HEAD -m "Making WIP" --no-gpg-sign --no-verify

coverage: $(COVER_DIR)
	engulf -s --coverdir=$(COVER_DIR) \
		--exclude '/vendor,integration/?,/bin/?,/dev_support/?,/util/test_with_docker/?,/examples/?,/util/cmdr/cmdr-example/?'\
		--exclude-files='raw_client.go$$,_generated.go$$'\
		--merge-base=_merged.txt ./...

legendary: coverage
	legendary --hitlist .cadre/coverage.vim /tmp/sous-cover/*_merged.txt

test: test-gofmt test-staticcheck test-unit test-integration 

test-staticcheck: install-staticcheck
	staticcheck -ignore "$$(cat staticcheck.ignore)" $(SOUS_PACKAGES)

test-gofmt:
	bin/check-gofmt

test-unit:
	go test $(EXTRA_GO_FLAGS) $(TEST_VERBOSE) -timeout 2m ./...

test-integration: setup-containers
	SOUS_QA_DESC=$(QA_DESC) go test -timeout 20m $(EXTRA_GO_FLAGS)  $(TEST_VERBOSE) ./integration --tags=integration

$(QA_DESC): sous-qa-setup
	./sous_qa_setup --compose-dir ./integration/test-registry/ --out-path=$(QA_DESC)

setup-containers:  $(QA_DESC)

test-cli: setup-containers linux-build
	rm -rf integration/raw_shell_output/0*
	@date
	SOUS_QA_DESC=$(QA_DESC) go test $(EXTRA_GO_FLAGS) $(TEST_VERBOSE) -timeout 20m ./integration --tags=commandline

$(BIN_DIR):
	mkdir -p $@

$(COVER_DIR):
	mkdir -p $@

$(RELEASE_DIRS):
	mkdir -p artifacts/$@
	cp -R doc/ artifacts/$@/doc
	cp README.md artifacts/$@
	cp LICENSE artifacts/$@

artifacts/$(DARWIN_RELEASE_DIR)/sous: $(DARWIN_RELEASE_DIR) $(BIN_DIR) install-build-tools
	xgo $(CONCAT_XGO_ARGS) --targets=darwin/amd64  ./
	mv $(BIN_DIR)/sous-darwin-10.6-amd64 $@

artifacts/$(LINUX_RELEASE_DIR)/sous: $(LINUX_RELEASE_DIR) $(BIN_DIR) install-build-tools
	xgo $(CONCAT_XGO_ARGS) --targets=linux/amd64  ./
	mv $(BIN_DIR)/sous-linux-amd64 $@

artifacts/$(LINUX_TARBALL): artifacts/$(LINUX_RELEASE_DIR)/sous
	cd artifacts && tar czv $(LINUX_RELEASE_DIR) > $(LINUX_TARBALL)

artifacts/$(DARWIN_TARBALL): artifacts/$(DARWIN_RELEASE_DIR)/sous
	cd artifacts && tar czv $(DARWIN_RELEASE_DIR) > $(DARWIN_TARBALL)

xgo:
	go get github.com/karalabe/xgo
	docker pull karalabe/xgo-$(GO_VERSION):latest
	
.PHONY: clean clean-containers clean-container-certs clean-running-containers clean-container-images coverage install-ggen legendary release semvertagchk test test-gofmt test-integration setup-containers test-unit reject-wip wip staticcheck xgo
