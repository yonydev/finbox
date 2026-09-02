FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0
RUN go vet ./... && go test ./internal/config/ ./internal/money/ ./internal/monthtok/ ./internal/imgtype/ ./internal/validate/
RUN go build -ldflags="-s -w" -o /finbox ./cmd/finbox

FROM alpine:3.22
RUN adduser -D -u 10001 finbox
USER finbox
COPY --from=build /finbox /usr/local/bin/finbox
ENTRYPOINT ["finbox"]
CMD ["serve"]
