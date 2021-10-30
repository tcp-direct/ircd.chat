.PHONY: all install build release capdefs test smoke gofmt irctest

GIT_COMMIT := $(shell git rev-parse HEAD 2> /dev/null)

capdef_file = ./irc/caps/defs.go

all: install

install:
	go install -v -ldflags "-X main.commit=$(GIT_COMMIT)"

build:
	go build -v -ldflags "-X main.commit=$(GIT_COMMIT)"

release:
	goreleaser --skip-publish --rm-dist

capdefs:
	python3 ./gencapdefs.py > ${capdef_file}

test:
	python3 ./gencapdefs.py | diff - ${capdef_file}
	cd irc && go test . && go vet .
	cd irc/caps && go test . && go vet .
	cd irc/cloaks && go test . && go vet .
	cd irc/connection_limits && go test . && go vet .
	cd irc/email && go test . && go vet .
	cd irc/flatip && go test . && go vet .
	cd irc/history && go test . && go vet .
	cd irc/isupport && go test . && go vet .
	cd irc/migrations && go test . && go vet .
	cd irc/modes && go test . && go vet .
	cd irc/passwd && go test . && go vet .
	cd irc/sno && go test . && go vet .
	cd irc/utils && go test . && go vet .
	./.check-gofmt.sh

smoke:
	ergo mkcerts --conf ./default.yaml || true
	ergo run --conf ./default.yaml --smoke

gofmt:
	./.check-gofmt.sh --fix

irctest:
	git submodule update --init
	cd irctest && make ergo
