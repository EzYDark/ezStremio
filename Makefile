build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ezstremio-linux-amd64 .

clean:
	rm -f ezstremio-linux-amd64
