NEBULA_VERSION := $(shell grep slackhq/nebula go.mod | cut -d ' ' -f 2)



build:
	go build -ldflags="-X 'main.NebulaVersion=$(NEBULA_VERSION)'" -o nebula-tray