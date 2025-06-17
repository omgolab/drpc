test: test-go test-js

test-go:
	go test -timeout 30s ./pkg/...

test-js:
	bun run test:integ:node

test-race:
	go test -race -timeout 30s ./pkg/...

profile:
	go test -memprofile=mem.prof -cpuprofile=cpu.prof -timeout 60s github.com/omgolab/drpc/pkg/drpc/integration_test

analyze:
	@echo "=== Memory Analysis ==="
	@go tool pprof -top -cum mem.prof | head -15
	@echo "\n=== CPU Analysis ==="
	@go tool pprof -top -cum cpu.prof | head -15

clean:
	rm -f *.prof
