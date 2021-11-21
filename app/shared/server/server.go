package server

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/josephspurrier/gowebapp/app/shared/sse"
)

// Server stores the hostname and port number
type Server struct {
	Hostname  string `json:"Hostname"`  // Server name
	UseHTTP   bool   `json:"UseHTTP"`   // Listen on HTTP
	UseHTTPS  bool   `json:"UseHTTPS"`  // Listen on HTTPS
	HTTPPort  int    `json:"HTTPPort"`  // HTTP port
	HTTPSPort int    `json:"HTTPSPort"` // HTTPS port
	CertFile  string `json:"CertFile"`  // HTTPS certificate
	KeyFile   string `json:"KeyFile"`   // HTTPS private key
}

// Run starts the HTTP and/or HTTPS listener
func Run(httpHandlers http.Handler, httpsHandlers http.Handler, s Server, sseBroker *sse.Broker) {
	// << DEMO CODE  --------------------------------------------------------------
	go sseTest(sseBroker)
	// >> DEMO CODE  --------------------------------------------------------------

	if s.UseHTTP && s.UseHTTPS {
		go func() {
			startHTTPS(httpsHandlers, s)
		}()

		startHTTP(httpHandlers, s)
	} else if s.UseHTTP {
		startHTTP(httpHandlers, s)
	} else if s.UseHTTPS {
		startHTTPS(httpsHandlers, s)
	} else {
		log.Println("Config file does not specify a listener to start")
	}
}

// startHTTP starts the HTTP listener
func startHTTP(handlers http.Handler, s Server) {
	fmt.Println(time.Now().Format("2006-01-02 03:04:05 PM"), "Running HTTP "+httpAddress(s))

	// Start the HTTP listener
	log.Fatal(http.ListenAndServe(httpAddress(s), handlers))
}

// startHTTPs starts the HTTPS listener
func startHTTPS(handlers http.Handler, s Server) {
	fmt.Println(time.Now().Format("2006-01-02 03:04:05 PM"), "Running HTTPS "+httpsAddress(s))

	// Start the HTTPS listener
	log.Fatal(http.ListenAndServeTLS(httpsAddress(s), s.CertFile, s.KeyFile, handlers))
}

// httpAddress returns the HTTP address
func httpAddress(s Server) string {
	return s.Hostname + ":" + fmt.Sprintf("%d", s.HTTPPort)
}

// httpsAddress returns the HTTPS address
func httpsAddress(s Server) string {
	return s.Hostname + ":" + fmt.Sprintf("%d", s.HTTPSPort)
}

// << DEMO CODE  ---------------------------------------------------------------
// Send broadcast and user messages probabilistically at given rates
// for performance testing.

const (
	updateRate    = 10.0 // Hz
	broadcastRate = 1.0  // Hz
	userRate      = 0.1  // Hz (for each user)

	broadcastProbability = broadcastRate / updateRate
	userProbability      = userRate / updateRate
)

func sseTest(broker *sse.Broker) {
	broadcastCount := 0
	for {
		if rand.Float64() < broadcastProbability {
			broker.Broadcast(fmt.Sprintf("HELLO FROM SSE: %d", broadcastCount))
			fmt.Printf("DEMO: SSE broadcast message: %d\n", broadcastCount)
			broadcastCount++
		}
		for _, user := range broker.UserIDs() {
			if rand.Float64() < userProbability {
				broker.Send(user, fmt.Sprintf("HELLO, USER %s", user))
				fmt.Printf("DEMO: SSE user message to %s\n", user)
			}
		}
		time.Sleep(1.0E9 / updateRate)
	}
}

// >> DEMO CODE  ---------------------------------------------------------------
