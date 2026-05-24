.PHONY: web build run dev dev-web typecheck

# Build the React UI into web/dist (embedded into the Go binary).
web:
	cd web && pnpm install && pnpm build

# Build the full single binary (web UI + bridge).
build: web
	go build -o whatsapp-bridge-v2 .

# Run the bridge directly (uses whatever is currently in web/dist).
run:
	go run .

# Frontend dev server with hot reload. Run `make run` in another terminal;
# Vite proxies /api to the bridge on :8082.
dev-web:
	cd web && pnpm dev

# Type-check the frontend without building.
typecheck:
	cd web && pnpm typecheck
