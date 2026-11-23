# SPDX-License-Identifier: GPL-3.0-only

.PHONY: all deps frontend backend test check install clean

all: frontend backend

deps:
	cd frontend && npm ci
	go mod download

frontend:
	cd frontend && npm run build

backend:
	go build -trimpath -o netrave .

test:
	python3 tools/check-i18n.py
	go test ./...
	cd frontend && npm run typecheck && npm test

check: test
	gofmt -l .
	go vet ./...

install: all
	sudo ./netrave install

clean:
	rm -f netrave
	rm -rf release frontend/test-results frontend/playwright-report
