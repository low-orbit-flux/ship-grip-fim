package main

import (
    "bufio"
    "fmt"
    "log"
    "net"
	"strings"
)

func startAgentServer(config configInfo) {
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

        go handleConnection(config, c)
    }
}

func handleConnection(config configInfo, connection1 net.Conn) {
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
                connection1.Write([]byte("\nscanning...\n"))
                callScan(config)
                connection1.Write([]byte("\nScan Complete\n"))
			case "list":
				//output := listReports()
			    //connection1.Write([]byte(output))
                connection1.Write([]byte("\nlisting reports ....\n"))
	
    			output := listReports(config)
                connection1.Write([]byte(output))
                connection1.Write([]byte("\nList Complete\n"))
			
	    	//case "data":
			//    listReportData(config, id1)
			
		    //case "compare":
    	    //    compareReports(config, id1, id2)  

                
			default:
				connection1.Write([]byte("unknown command"))
		}
    }
}