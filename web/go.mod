// Marks web/ as a separate (empty) Go module so `go test ./...` and the linters
// skip web/node_modules, where some npm packages ship stray .go files.
module github.com/Arif-Karim/mcp-hub/web-not-go
