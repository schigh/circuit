FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /server ./_functional_tests/scenarios/http/server

FROM alpine:3.19
COPY --from=build /server /server
EXPOSE 8080 6060
CMD ["/server"]
