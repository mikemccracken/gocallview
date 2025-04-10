
TOP_LEVEL=$(shell git rev-parse --show-toplevel)
LDFLAGS=--ldflags '-extldflags "-static"'
GOTAGS=
TOOLSDIR := $(TOP_LEVEL)/tools
GOLINTER_VERSION := v1.54.2
GOLINTER := $(TOOLSDIR)/bin/golangci-lint


gcv: *.go
	CGO_ENABLED=0 go build ${LDFLAGS} ${GOTAGS} -o gcv  ./...

$(GOLINTER):
	mkdir -p $(TOOLSDIR)/bin
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(TOOLSDIR)/bin $(GOLINTER_VERSION)
	$(GOLINTER) version


lint: *.go $(GOLINTER)
	$(GOLINTER) run --out-format=colored-line-number

.PHONY: clean
clean:
	rm -rf gcv
