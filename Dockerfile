FROM --platform=$BUILDPLATFORM node:22-alpine AS css
WORKDIR /app
COPY package.json package-lock.json postcss.config.js ./
RUN npm ci
COPY web/static/input.css web/static/input.css
COPY web/templates web/templates
RUN ./node_modules/.bin/postcss web/static/input.css -o web/static/tailwind.css

FROM golang:1.26-alpine AS builder
ARG VERSION=dev
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=css /app/web/static/tailwind.css web/static/tailwind.css
RUN echo "${VERSION}" > web/VERSION.txt && CGO_ENABLED=1 go build -ldflags="-s -w" -trimpath -o bedwetter .

FROM alpine:3.22
ARG VERSION=dev
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
ENV VERSION=${VERSION}
LABEL org.opencontainers.image.title="BedWetter" \
      org.opencontainers.image.description="A garden irrigation controller" \
      org.opencontainers.image.version="${VERSION}"
COPY --from=builder /app/bedwetter .
COPY --from=builder /app/web/static web/static
COPY example-config.yaml config.yaml
EXPOSE 8080
ENTRYPOINT ["./bedwetter"]
CMD ["-config", "config.yaml"]
