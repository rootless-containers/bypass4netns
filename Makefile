GO ?= go
GO_BUILD := $(GO) build

.DEFAULT: all

all: bypass4netns bypass4netnsd

bypass4netns:
	$(GO_BUILD) -o $@ cmd/$@/*

bypass4netnsd:
	$(GO_BUILD) -o $@ cmd/$@/*

install: bypass4netns bypass4netnsd
	install bypass4netns /usr/local/bin/bypass4netns
	install bypass4netnsd /usr/local/bin/bypass4netnsd

uninstall:
	rm -rf /usr/local/bin/bypass4netns

clean:
	rm -rf bypass4netns

.PHONY: all bypass4netns bypass4netnsd install uninstall clean
