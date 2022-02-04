GO ?= go
GO_BUILD := $(GO) build

.DEFAULT: bypass4netns

bypass4netns:
	$(GO_BUILD) -o $@ cmd/$@/main.go

install: bypass4netns
	install bypass4netns /usr/local/bin/bypass4netns

uninstall:
	rm -rf /usr/local/bin/bypass4netns

clean:
	rm -rf bypass4netns

.PHONY: clean install