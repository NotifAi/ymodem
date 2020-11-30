CGO_ENABLED=0 GOOS=linux GOARCH=arm go build -v -ldflags="-s -w" main.go
