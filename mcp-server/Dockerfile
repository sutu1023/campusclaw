# Root build context: reuse the main Go module, never copy runtime .env files.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/mcp ./internal/mcp
COPY mcp-server ./mcp-server
RUN CGO_ENABLED=0 go build -trimpath -o /out/mcp-server ./mcp-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mcp-server /mcp-server
EXPOSE 8090
HEALTHCHECK --interval=5s --timeout=3s --start-period=5s --retries=5 CMD ["/mcp-server", "--healthcheck"]
ENTRYPOINT ["/mcp-server"]
