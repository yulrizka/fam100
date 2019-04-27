BTIME     ?= $(shell date '+%Y%m%d%H%M%S')
BTIME_H   ?= $(shell date '+%Y-%m-%dT%H:%M:%S_%z')
HASH = $(shell git rev-parse --short HEAD)
TAGS = $(shell git tag -l --points-at HEAD)
TAGS_V = $(shell [ ! -z "$(TAGS)" ] && echo "_$(TAGS)")
VERSION = "$(HASH)$(TAGS_V)"
GOFLAGS   = GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0
GOLDFLAGS += -X main.VERSION=$(VERSION)
GOLDFLAGS += -X main.BUILDTIME=$(BTIME_H)
GOOPTS    = -ldflags "$(GOLDFLAGS)"

.PHONY: linux
linux:
	@echo $(GOOPTS)
	@GOOS=linux GOARCH=amd64 go build -tags netgo $(GOOPTS) -o fam100
