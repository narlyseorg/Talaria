.PHONY: build build-intel build-apple build-universal clean run

BINARY := talaria

build:
	go mod tidy
	go build -ldflags="-s -w" -o $(BINARY) .

build-intel:
	go mod tidy
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o $(BINARY)-intel .

build-apple:
	go mod tidy
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o $(BINARY)-apple .

build-universal: build-intel build-apple
	lipo -create -output $(BINARY) $(BINARY)-intel $(BINARY)-apple
	rm -f $(BINARY)-intel $(BINARY)-apple

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY) $(BINARY)-intel $(BINARY)-apple
