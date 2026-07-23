package main

import (
	"log"
	"net"
	"time"

	"github.com/grandcat/zeroconf"
)

func main() {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Fatal(err)
	}
	for _, iface := range ifaces {
		if iface.Name == "VMware Network Adapter VMnet8" {
			log.Printf("Found VMnet8")
			server, err := zeroconf.Register("test", "_localmesh._tcp", "local.", 1234, nil, []net.Interface{iface})
			if err != nil {
				log.Fatal(err)
			}
			defer server.Shutdown()
			time.Sleep(2 * time.Second)
		}
	}
}
