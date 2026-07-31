.PHONY: build dev css clean test approve

css:
	npx postcss web/static/input.css -o web/static/tailwind.css

VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

build: css
	echo "$(VERSION)" > web/VERSION.txt
	go build -ldflags="-s -w" -o bedwetter .

dev:
	@which air > /dev/null 2>&1 || \
		test -x "$(shell go env GOPATH)/bin/air" > /dev/null 2>&1 || { \
		echo "Installing air (live-reload)…"; \
		go install github.com/air-verse/air@latest; \
	}
	PATH="$(shell go env GOPATH)/bin:$$PATH" air

clean:
	rm -f bedwetter tmp/bedwetter build-errors.log bedwetter.db

test:
	go test -count=1 -timeout 120s -cover ./...

approve:
	cd web && for f in *.received.txt; do cp "$$f" "$${f/.received./.approved.}"; done
