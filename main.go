package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Service struct {
	Name    string
	Address string
	Port    int
}

func (s Service) AddressAndPort() string {
	return fmt.Sprintf("%s:%d", s.Address, s.Port)
}

var timeout = flag.Int64("timeout", 60, "time to wait for all services to be up (seconds)")

func main() {
	flag.Parse()

	services := loadServicesFromEnv()
	log.Printf("Services: %#v", services)

	var wg sync.WaitGroup
	cancel := make(chan struct{})

	for _, service := range services {
		wg.Add(1)
		go func(service Service) {
			waitForTcpConn(service, cancel)
			wg.Done()
		}(service)
	}

	timer := time.AfterFunc(time.Duration(*timeout)*time.Second, func() {
		close(cancel)
	})

	wg.Wait()

	// There's a race here that might result in assuming that a timeout happend
	// although none happend. It appears when the timer fires after the connection
	// succeeded, but before the check via Stop() below.
	// That shouldn't happen very often and the service was pretty short of timing out
	// anyway, so I guess that's ok for now.
	if !timer.Stop() {
		log.Printf("Error: One or more services timed out after %d second(s)", *timeout)
		os.Exit(1)
	}
	log.Printf("All services are up!")
}

func loadServicesFromEnv() []Service {
	services := make([]Service, 0)
	for _, line := range os.Environ() {
		keyAndValue := strings.SplitN(line, "=", 2)
		addrKey := keyAndValue[0]
		if strings.HasSuffix(addrKey, "_TCP_ADDR") {
			addr := os.Getenv(addrKey)
			name := addrKey[:len(addrKey)-9] // cut off "_TCP_ADDR"

			portKey := name + "_TCP_PORT"
			portStr := os.Getenv(portKey)
			port, err := strconv.Atoi(portStr)
			if err != nil {
				log.Printf("Failed to convert '%v' to int, value: '%v' - skipping service '%v'",
					portKey, portStr, name)
				continue
			}
			services = append(services, Service{Name: name, Address: addr, Port: port})
		}
	}
	return services
}

func waitForTcpConn(service Service, cancel <-chan struct{}) {
	var cancelled int32 = 0
	go func() {
		<-cancel
		atomic.StoreInt32(&cancelled, 1)
	}()

	var conn net.Conn
	err := errors.New("init")
	for err != nil {
		conn, err = net.DialTimeout("tcp", service.AddressAndPort(), 1*time.Second)

		if cancelled == 1 && err != nil {
			log.Printf("Service %v (%v) timed out. Last error: %v",
				service.Name, service.AddressAndPort(), err)
			return
		}
	}
	conn.Close()
	log.Printf("Service %v (%v) is up", service.Name, service.AddressAndPort())
}