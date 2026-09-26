package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
)

// openAgentSession dials an agent over TLS and performs the "auth" handshake
// with the credentials in config (agentUser / agentPassword).
func openAgentSession(config configInfo, host, port string) (net.Conn, *bufio.Reader, error) {
	conn, err := dialAgent(config, host, port)
	if err != nil {
		return nil, nil, fmt.Errorf("could not connect to agent at %s: %w", net.JoinHostPort(host, port), err)
	}
	if _, err := conn.Write([]byte("auth " + config.agentUser + " " + config.agentPassword + "\n")); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("failed to send credentials: %w", err)
	}
	reader := bufio.NewReader(conn)
	reply, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("no reply to authentication: %w", err)
	}
	if !strings.HasPrefix(reply, "OK") {
		conn.Close()
		// The agent replies "ERROR - ..."; callers add their own prefix.
		return nil, nil, fmt.Errorf("%s", strings.TrimPrefix(strings.TrimSpace(reply), "ERROR - "))
	}
	return conn, reader, nil
}

// runRemoteCommand connects to a running agent, sends cmdArgs as a single
// command line, then prints every response line until the "---DONE---" sentinel.
func runRemoteCommand(config configInfo, host, port string, cmdArgs []string) {
	conn, reader, err := openAgentSession(config, host, port)
	if err != nil {
		fmt.Println("ERROR -", err)
		return
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(strings.Join(cmdArgs, " ") + "\n")); err != nil {
		fmt.Println("ERROR - failed to send command:", err)
		return
	}
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Print(line)
		}
		if err != nil || strings.TrimSpace(line) == "---DONE---" {
			break
		}
	}
}

// runRemoteCommandToString is the same as runRemoteCommand but returns the
// response as a string instead of printing it.  Used for bulk/ping/sync, the
// exporter and both GUIs.  The ---DONE--- sentinel is stripped from the result.
func runRemoteCommandToString(config configInfo, host, port string, cmdArgs []string) string {
	conn, reader, err := openAgentSession(config, host, port)
	if err != nil {
		return "ERROR - " + err.Error() + "\n"
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(strings.Join(cmdArgs, " ") + "\n")); err != nil {
		return "ERROR - failed to send command: " + err.Error() + "\n"
	}
	var sb strings.Builder
	for {
		line, err := reader.ReadString('\n')
		done := strings.TrimSpace(line) == "---DONE---"
		if len(line) > 0 && !done {
			sb.WriteString(line)
		}
		if err != nil || done {
			break
		}
	}
	return sb.String()
}
