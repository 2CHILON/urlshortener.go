FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/shortener ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/shortener /shortener
ENV ADDR=:8080 DATA_FILE=/data/links.json
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/shortener"]