FROM --platform=$BUILDPLATFORM node:22-alpine AS css
WORKDIR /app
COPY package.json postcss.config.js ./
RUN npm install
COPY web/static/input.css web/static/input.css
RUN ./node_modules/.bin/postcss web/static/input.css -o web/static/tailwind.css

FROM golang:1.26-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=css /app/web/static/tailwind.css web/static/tailwind.css
RUN CGO_ENABLED=1 go build -ldflags="-s -w" -trimpath -o bedwetter .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/bedwetter .
COPY --from=builder /app/web/static web/static
COPY example-config.yaml config.yaml
EXPOSE 8080
ENTRYPOINT ["./bedwetter"]
CMD ["-config", "config.yaml"]
