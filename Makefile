test: test-go test-js

test-go:
	go test -race -timeout 30s ./pkg/...

test-js:
	bun run test:integ:node

test-discovery:
	tsx experiments/discovery/discover-path.menv.ts

test-discovery-node:
	tsx experiments/discovery/discover-path.menv.ts --env=node

test-debug-discovery-node:
	tsx experiments/discovery/discover-path.menv.ts --env=node --debug=libp2p:*

test-discovery-chrome:
	tsx experiments/discovery/discover-path.menv.ts --env=chrome

test-debug-discovery-chrome:
	tsx experiments/discovery/discover-path.menv.ts --env=chrome --debug=libp2p:*

test-discovery-firefox:
	tsx experiments/discovery/discover-path.menv.ts --env=firefox

test-browser-demo:
	go run cmd/util-server/main.go & bun experiments/discovery/discover-browser-demo.html

profile:
	go test -memprofile=mem.prof -cpuprofile=cpu.prof -timeout 60s github.com/omgolab/drpc/pkg/drpc/integration_test

analyze:
	@echo "=== Memory Analysis ==="
	@go tool pprof -top -cum mem.prof | head -15
	@echo "\n=== CPU Analysis ==="
	@go tool pprof -top -cum cpu.prof | head -15

clean:
	rm -f *.prof
