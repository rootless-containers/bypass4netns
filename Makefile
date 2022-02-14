GO ?= go
GO_BUILD_FLAGS += -trimpath
GO_BUILD := $(GO) build $(GO_BUILD_FLAGS)
GO_BUILD_STATIC := CGO_ENABLED=1 $(GO) build $(GO_BUILD_FLAGS) -tags "netgo osusergo" -ldflags "-extldflags -static"

STRIP ?= strip

.DEFAULT: all

all: dynamic

dynamic:
	$(GO_BUILD) ./cmd/bypass4netns
	$(GO_BUILD) ./cmd/bypass4netnsd

static:
	$(GO_BUILD_STATIC) ./cmd/bypass4netns
	$(GO_BUILD_STATIC) ./cmd/bypass4netnsd

strip:
	$(STRIP) bypass4netns bypass4netnsd

install:
	install bypass4netns /usr/local/bin/bypass4netns
	install bypass4netnsd /usr/local/bin/bypass4netnsd

uninstall:
	rm -rf /usr/local/bin/bypass4netns /usr/local/bin/bypass4netnsd

clean:
	rm -rf bypass4netns bypass4netnsd

.PHONY: all dynamic static strip install uninstall clean
