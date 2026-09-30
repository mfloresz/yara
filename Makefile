BIN    := translator-server
VERSION ?= dev
LDFLAGS := -s -w -X main.Version=$(VERSION)

.PHONY: frontend build android android-armv7 linux-arm64 linux-armv7 compress parsers-manifest dev run clean

frontend:
	cd frontend && npm install && npm run build

build: frontend
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BIN)-linux-amd64-$(VERSION) ./cmd/server

linux-arm64: frontend
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BIN)-linux-arm64-$(VERSION) ./cmd/server

linux-armv7: frontend
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BIN)-linux-armv7-$(VERSION) ./cmd/server

android: frontend
	CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BIN)-android-arm64-$(VERSION) ./cmd/server

## android-armv7: Requiere NDK o CGO habilitado con cross-compiler.
## No funciona en CI sin el NDK de Android.
android-armv7: frontend
	CGO_ENABLED=1 \
	CC=arm-linux-androideabi-clang \
	CXX=arm-linux-androideabi-clang++ \
	GOOS=android GOARCH=arm GOARM=7 \
	go build -trimpath -ldflags="$(LDFLAGS)" \
		-o bin/$(BIN)-android-armv7-$(VERSION) ./cmd/server

## all: Compila para todas las plataformas
all: build linux-arm64 linux-armv7 android android-armv7

## compress: Comprime el binario con UPX (máxima compresión)
compress:
	@echo "Comprimiendo binario con UPX..."
	@if command -v upx >/dev/null 2>&1; then \
		upx --best --lzma bin/$(BIN)-*; \
		echo "Compresión completada"; \
		ls -lh bin/; \
	else \
		echo "Error: UPX no está instalado. Instálalo con: apt install upx-ucl o brew install upx"; \
		exit 1; \
	fi

## parsers-manifest: Regenera parsers/index.json, el manifiesto que el servidor
## consulta para auto-actualizar los parsers instalados antes de ejecutarlos.
## Ejecutarlo tras añadir o editar un parser del repositorio. También firma el
## manifiesto con la clave de .parsers-signing.key (o PARSERS_SIGNING_KEY):
## sin firma válida, las instalaciones ignoran el manifiesto y ejecutan lo
## instalado. Ver tools/sign-parsers-manifest (usa -keygen para rotar la clave
## y PARSERS_MANIFEST_PUBKEY en el servidor para el despliegue).
parsers-manifest:
	go run ./tools/gen-parser-manifest && go run ./tools/sign-parsers-manifest

dev:
	@echo "Run in two terminals:"
	@echo "  1) cd frontend && npm run dev"
	@echo "  2) go run ./cmd/server --addr :8080"

run:
	./bin/$(BIN)-linux-amd64-$(VERSION)

clean:
	rm -f bin/$(BIN)-*
