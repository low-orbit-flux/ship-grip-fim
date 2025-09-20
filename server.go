package main

import (
    "bufio"
    "fmt"
    "log"
    "net"
	"strings"
)

func startServer() {
	host := "localhost"
    port := "8080"
    protocol := "tcp"

    fmt.Println("Server starting " + host + ":" + port)
    l, err := net.Listen(protocol, host + ":" + port)
    if err != nil {
        panic(err)
    }
    defer l.Close()

    for {
        c, err := l.Accept()
        if err != nil {
            fmt.Println("ERROR - Failed to accept connection:", err.Error())
            return
        }
        fmt.Println("Client connected: " + c.RemoteAddr().String())

        go handleConnection(c)
    }
}

func handleConnection(connection1 net.Conn) {
    for {
        buffer, err := bufio.NewReader(connection1).ReadBytes('\n')
        if err != nil {
            fmt.Println("Client disconnected: " + connection1.RemoteAddr().String())
            connection1.Close()
            return
        }
		log.Println("Client message:", string(buffer[:len(buffer)-1]))
		switch strings.TrimSpace(string(buffer)) {
		    case "scan":
                connection1.Write([]byte("scan"))
			case "list":
				//output := listReports()
			    //connection1.Write([]byte(output))
                connection1.Write([]byte("test"))
			default:
				connection1.Write([]byte("unknown command"))
		}
    }
}