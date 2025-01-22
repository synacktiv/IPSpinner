.DEFAULT_GOAL := build

pre-build:
	go mod tidy


build-all: build-linux build-windows build-collector-linux build-collector-windows


build: build-linux build-windows

build-linux: pre-build
	GOOS=linux GOARCH=amd64 go build -o ipspinner .

build-windows: pre-build
	GOOS=windows GOARCH=amd64 go build -o ipspinner.exe .
 

clean:
	rm ipspinner
	rm ipspinner.exe
