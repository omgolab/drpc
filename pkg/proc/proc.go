package proc

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// KillPort frees a TCP port by terminating its process.
// It tries binding to the port up to 5 times and returns nil if successful.
func KillPort(port string, morePorts ...string) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(append(morePorts, port)))

	for _, p := range append(morePorts, port) {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			for i := range 5 {
				// Check if the port is still in use by trying to listen on it
				ln, err := net.Listen("tcp", ":"+p)
				if err != nil {
					err := kill(p)
					if err != nil && i == 4 {
						errChan <- err
						return
					}
					// Port is still in use, wait for a few seconds
					time.Sleep(500 * time.Millisecond)
					continue
				}
				ln.Close()
				return
			}
		}(p)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			return err
		}
	}

	return nil
}

func kill(port string) error {
	if runtime.GOOS == "windows" {
		// Find the process ID using netstat
		findCmd := exec.Command("cmd", "/C",
			fmt.Sprintf(`netstat -ano | find "LISTENING" | find ":%s"`, port))
		output, err := findCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to find process on port %s: %w", port, err)
		}

		// Parse the output to get PID
		// Output format: TCP    0.0.0.0:8080    0.0.0.0:0    LISTENING    1234
		lines := strings.Split(string(output), "\n")
		if len(lines) == 0 {
			return fmt.Errorf("no process found listening on port %s", port)
		}

		// Extract PID from the last column
		fields := strings.Fields(lines[0])
		if len(fields) < 5 {
			return fmt.Errorf("unexpected netstat output format")
		}
		pid := fields[len(fields)-1]

		// Kill the process
		killCmd := exec.Command("taskkill", "/F", "/PID", pid)
		if err := killCmd.Run(); err != nil {
			return fmt.Errorf("failed to kill process %s: %w", pid, err)
		}
		return nil
	}

	// For Unix-like systems (macOS, Linux)
	// First check if any process is using the port
	checkCmd := exec.Command("bash", "-c",
		fmt.Sprintf("lsof -i tcp:%s | grep LISTEN | awk '{print $2}'", port))
	output, _ := checkCmd.Output()

	if len(strings.TrimSpace(string(output))) == 0 {
		// No process found using the port, so nothing to kill
		return nil
	}

	// Process found, proceed with kill using lsof + kill
	killCmd := exec.Command("bash", "-c",
		fmt.Sprintf("lsof -i tcp:%s | grep LISTEN | awk '{print $2}' | xargs kill -9", port))

	if err := killCmd.Run(); err != nil {
		// If kill fails, try pkill as a fallback method
		// pkill -f can match against the entire command line
		pkillCmd := exec.Command("bash", "-c",
			fmt.Sprintf("pkill -f 'LISTEN.*:%s'", port))

		if pkillErr := pkillCmd.Run(); pkillErr != nil {
			return fmt.Errorf("failed to kill process on port %s (both kill and pkill failed): %w", port, err)
		}
	}

	return nil
}
