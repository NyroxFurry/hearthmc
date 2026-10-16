# Hearth – Build-Ziele
#
#   make            Weboberfläche bauen und Binary für dieses System erzeugen
#   make release    Binaries für Linux (amd64/arm64), Windows und macOS in ./dist
#   make test       Go-Tests und Typprüfung der Oberfläche
#   make licenses   THIRD_PARTY_LICENSES.txt aus den Abhängigkeiten neu erzeugen
#   make dev        Panel auf :18080 starten, Oberfläche mit Hot-Reload auf :5173

VERSION ?= 1.2.0
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X hearth/internal/version.Version=$(VERSION) \
	-X hearth/internal/version.Commit=$(COMMIT) \
	-X hearth/internal/version.Date=$(DATE)

TARGETS := linux/amd64 linux/arm64 windows/amd64 darwin/arm64 darwin/amd64

.PHONY: all web licenses build release test dev clean

all: web build

web:
	cd web && npm ci
	go run ./tools/licenses
	cd web && npm run build

# Lizenztexte aller Go-Module, npm-Pakete und Schriften, die in Hearth landen.
# Bricht ab, wenn eine Abhängigkeit keine Lizenzdatei oder eine nicht
# freigegebene Lizenz hat. Braucht web/node_modules (npm ci).
licenses:
	go run ./tools/licenses

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/hearth ./cmd/hearth

release:
	@mkdir -p dist
	@for t in $(TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; [ "$$os" = windows ] && ext=".exe"; \
		echo "  $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/hearth-$(VERSION)-$$os-$$arch$$ext ./cmd/hearth || exit 1; \
	done
	@cp LICENSE THIRD_PARTY_LICENSES.txt dist/
	@cd dist && sha256sum hearth-$(VERSION)-* > SHA256SUMS

test:
	go vet ./...
	go test ./...
	cd web && npm run check

dev:
	@echo "Panel: http://127.0.0.1:18080  –  Oberfläche mit Hot-Reload: http://127.0.0.1:5173"
	go run ./cmd/hearth serve --data ./data-dev --listen 127.0.0.1:18080 & \
	cd web && npm run dev

clean:
	rm -rf dist
