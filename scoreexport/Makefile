BTIME     ?= $(shell date '+%Y%m%d%H%M%S')
BTIME_H   ?= $(shell date '+%Y-%m-%dT%H:%M:%S_%z')
VERSION   = $(shell git log --pretty=format:'%h %D' --abbrev=10 | head -1 | sed -e 's/^\([0-9a-f]*\) .* tag: \([^,]*\),.*/\1_(\2)/')
GOFLAGS   = GOOS=$(GOOS) GOARCH=$(GOARCH)
GOLDFLAGS += -X main.VERSION=$(VERSION)
GOLDFLAGS += -X main.BUILDTIME=$(BTIME_H)
GOOPTS    = -ldflags "$(GOLDFLAGS)"

.PHONY: linux
linux:
	@echo $(GOOPTS)
	@GOOS=linux GOARCH=amd64 go build $(GOOPTS) -o scoreexport
