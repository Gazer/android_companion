BINARY  := android-companion
MODULE  := github.com/rmarkiewicz/android-companion
PROTOC  := protoc
PROTO_DIR := gen

.PHONY: all build run proto clean tidy install

all: build

build:
	go build -o $(BINARY) .

run: build
	./$(BINARY)

# Regenerate Go code from the emulator_controller.proto copied from the SDK.
proto:
	PATH="$$HOME/go/bin:$$PATH" $(PROTOC) \
		--go_out=. --go_opt=module=$(MODULE) \
		--go-grpc_out=. --go-grpc_opt=module=$(MODULE) \
		-I. -I/usr/local/include \
		$(PROTO_DIR)/emulator_controller.proto

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)

install: build
ifeq ($(OS),Windows_NT)
	@echo "Windows is not supported by 'make install'. Copy $(BINARY).exe to a directory in your PATH manually."
else
	@mkdir -p $(HOME)/.local/bin
	@cp $(BINARY) $(HOME)/.local/bin/$(BINARY)
	@echo "Installed to $(HOME)/.local/bin/$(BINARY)"
	@echo "Make sure $(HOME)/.local/bin is in your PATH."
endif

# Linux deps: sudo apt install libgl1-mesa-dev xorg-dev libavcodec-dev libavutil-dev libswscale-dev pkg-config
# macOS deps: brew install ffmpeg pkg-config
# Without FFmpeg: go build -tags nomirror (no device mirroring)
