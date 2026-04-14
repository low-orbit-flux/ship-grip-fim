package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
)

// runRemoteCommand connects to a running agent, sends cmdArgs as a single
// command line, then prints every response line until the "---DONE---" sentinel.
func runRemoteCommand(host, port string, cmdArgs []string) {
	addr := host + ":" + port
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Println("ERROR - could not connect to agent at " + addr + ": " + err.Error())
		return
	}
	defer conn.Close()

	cmd := strings.Join(cmdArgs, " ") + "\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		fmt.Println("ERROR - failed to send command:", err)
		return
	}

	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Print(line)
		}
		if err != nil {
			break
		}
		if strings.TrimSpace(line) == "---DONE---" {
			break
		}
	}
}

// runRemoteCommandToString is the same as runRemoteCommand but returns the
// response as a string instead of printing it.  Used for bulk/ping/sync.
// The ---DONE--- sentinel is stripped from the result.
func runRemoteCommandToString(host, port string, cmdArgs []string) string {
	addr := host + ":" + port
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return "ERROR - could not connect to " + addr + ": " + err.Error() + "\n"
	}
	defer conn.Close()

	cmd := strings.Join(cmdArgs, " ") + "\n"
	if _, err := conn.Write([]byte(cmd)); err != nil {
		return "ERROR - failed to send command: " + err.Error() + "\n"
	}

	var sb strings.Builder
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 && strings.TrimSpace(line) != "---DONE---" {
			sb.WriteString(line)
		}
		if err != nil {
			break
		}
		if strings.TrimSpace(line) == "---DONE---" {
			break
		}
	}
	return sb.String()
}
