.PHONY: build dev css clean test

css:
	npx postcss web/static/input.css -o web/static/tailwind.css

build: css
	go build -o bedwetter .

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
