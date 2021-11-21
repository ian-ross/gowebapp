package sse

import (
	"fmt"
	"net/http"

	"github.com/josephspurrier/gowebapp/app/shared/session"
)

var (
	sse Info

	Broker *broker
)

// Info has the details for configuring the SSE broker.
type Info struct {
	BrokerInstances int
}

// Configure adds the settings for SSE broker.
func Configure(c Info) {
	sse = c
	Broker = newBroker()
}

// SendMsgToAll sends an SSE message to all connected clients.
func SendMsgToAll(msg string) {
	Broker.broadcast(msg)
}

// SendMsgToUser sends an SSE message to a single clients.
func SendMsgToUser(userID string, msg string) {
	Broker.send(userID, msg)
}

// UserIDs retrieves all the connected authenticated user IDs.
func UserIDs() []string {
	return Broker.UserIDs()
}

// clientMessage is the data passed to each client's message handling
// goroutine.
type clientMessage struct {
	broadcast bool
	data      []byte
}

// message wraps the message data with an optional destination user ID
// for input to the SSE broker.
type message struct {
	id   *string
	data []byte
}

// clientData is the information passed to add a new client, which is
// an optional user ID (for authenticated clients) and a message
// channel.
type clientData struct {
	id *string
	ch chan clientMessage
}

// clientSet is a set of client message channels.
type clientSet map[chan clientMessage]bool

// brokerInstance is responsible for keeping a list of which clients
// are currently attached to a broker goroutine and directing messages
// to those clients.
type brokerInstance struct {
	// Integer label for broker instances. Used in log messages.
	label int

	// Set of clients for broadcast messages: keys of the map are the
	// channels over which we can push messages to attached clients.
	broadcastClients clientSet

	// Map from user IDs to a set of clients for message sending to
	// authenticated clients: keys of the map are user IDs, values are
	// the sets of channels over which we can push messages to attached
	// clients. We do things this way to deal with the case where an
	// authenticated client has multiple requests to the /events
	// endpoint active at once.
	authClients map[string]clientSet

	// Set of authenticated client IDs for channel close lookup: keys of
	// the map are the channels over which we can push messages to
	// attached clients, values are user IDs.
	authClientIDs map[chan clientMessage]string

	// Channel into which disconnected clients should be pushed.
	defunctClients chan chan clientMessage

	// Channel into which messages are pushed to be sent out to attached
	// clients (either broadcast or individual: which is which is
	// controlled by the user ID field in the message struct).
	messages chan message

	// Channel to trigger broker shutdown.
	done chan bool

	// Channel for user ID list requests.
	userIDReq chan chan []string
}

// broker is the top-level structure that ties together a set of SSE
// broker instances.
type broker struct {
	// There is one brokerInstance per broker goroutine.
	instances []*brokerInstance

	// The new connection channel is shared between the instances, and
	// each instance takes new clients from the channel randomly.
	newClients chan clientData
}

// Start starts a new goroutine. It handles the addition and removal
// of clients, as well as the broadcasting of messages out to clients
// that are currently attached.
func (bi *brokerInstance) start(b *broker) {
	go func() {
		for {
			select {
			case <-bi.done:
				break

			case s := <-b.newClients:
				// There is a new client attached and we want to start sending
				// them messages.
				bi.broadcastClients[s.ch] = true
				if s.id != nil {
					bi.authClientIDs[s.ch] = *s.id
					if _, exists := bi.authClients[*s.id]; !exists {
						bi.authClients[*s.id] = make(clientSet)
					}
					bi.authClients[*s.id][s.ch] = true
				}
				fmt.Printf("Added new SSE client (SSE broker %d)\n", bi.label)

			case s := <-bi.defunctClients:
				// A client has detached and we want to stop sending them
				// messages.
				if _, exists := bi.broadcastClients[s]; exists {
					delete(bi.broadcastClients, s)
					if auth, ok := bi.authClientIDs[s]; ok {
						delete(bi.authClients[auth], s)
						if len(bi.authClients[auth]) == 0 {
							delete(bi.authClients, auth)
						}
						delete(bi.authClientIDs, s)
					}
					close(s)
					fmt.Printf("Removed SSE client (SSE broker %d)\n", bi.label)
				}

			case msg := <-bi.messages:
				// There is a new message to send. Set up client message with
				// flag to mark whether or not it's a broadcast message.
				m := clientMessage{msg.id == nil, msg.data}
				if msg.id == nil {
					// Broadcast message: for each attached client, push the new
					// message into the client's message channel.
					for s := range bi.broadcastClients {
						s <- m
					}
				} else {
					// Message to individual client: look the client up and send
					// the message if known.
					if ss, ok := bi.authClients[*msg.id]; ok {
						for s := range ss {
							s <- m
						}
					}
				}

			case userIDChan := <-bi.userIDReq:
				// Retrieve user ID list for the instance. Done over a channel
				// to avoid potential concurrent map iteration and writes
				// between Broker.UserIDs and new client processing.
				retids := []string{}
				for id := range bi.authClients {
					retids = append(retids, id)
				}
				userIDChan <- retids
			}
		}
	}()
}

// Stop shuts down the SSE broker.
func (b *broker) stop() {
	for _, bi := range b.instances {
		bi.done <- true
	}
}

// Broadcast sends an SSE message for broadcast to all clients.
func (b *broker) broadcast(msg string) {
	// Forward the message to all broker instances, so that the
	// instances can send the message on to all of their connected
	// clients.
	for _, bi := range b.instances {
		bi.messages <- message{nil, []byte(msg)}
	}
}

// Send sends an SSE message to a single client.
func (b *broker) send(user string, msg string) {
	// Forward the message to all broker instances, so that the
	// instances can send the message on to the requested user if there
	// is a connection from that user. (Note that multiple requests from
	// the same user to the /events endpoint may result in multiple
	// connections to multiple broker instances, so messages for all
	// users are forwarded to all broker instances to ensure that the
	// message is sent to all relevant client connections.)
	for _, bi := range b.instances {
		bi.messages <- message{&user, []byte(msg)}
	}
}

// ServeHTTP handles HTTP requests for the "/events/" endpoint.
func (b *broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Make sure that the writer supports streaming.
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Look up user ID from session.
	sess := session.Instance(r)
	var user *string
	if sess.Values["id"] != nil {
		tmp := fmt.Sprintf("%s", sess.Values["id"])
		user = &tmp
	}

	// Create a new channel, over which the broker can send this client
	// messages, and send the new client connection to the broker's new
	// clients channel, where it will be picked up by a broker instances
	// for processing.
	messageChan := make(chan clientMessage)
	b.newClients <- clientData{user, messageChan}

	// Listen to the closing of the HTTP connection via the
	// CloseNotifier and notify the broker instances about the closure.
	notify := w.(http.CloseNotifier).CloseNotify()
	go func() {
		<-notify
		// Remove this client from the map of attached clients. We send
		// this information to all broker instances, because we don't know
		// which instance is servicing this connection.
		for _, bi := range b.instances {
			bi.defunctClients <- messageChan
		}
		fmt.Println("HTTP connection just closed.")
	}()

	// Set HTTP headers related to event streaming.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Accel-Buffering", "no")

	// Repeatedly receive messages from the broker and send them to the
	// client in text/stream format.
	for {
		msg, open := <-messageChan
		if !open {
			// If our messageChan was closed, this means that the client has
			// disconnected.
			break
		}

		// Broadcast messages and messages for individual users are
		// distinguished by the text/stream "event" field.
		if msg.broadcast {
			fmt.Fprintf(w, "event: broadcast\n")
		} else {
			fmt.Fprintf(w, "event: individual\n")
		}
		fmt.Fprintf(w, "data: %s\n\n", msg.data)
		f.Flush()
	}

	fmt.Println("Finished HTTP request at ", r.URL.Path)
}

func handler(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("ok"))
}

// NewbrokerInstance creates and starts a new SSE broker instance.
func newBrokerInstance(label int) *brokerInstance {
	bi := &brokerInstance{
		label:            label,
		broadcastClients: make(clientSet),
		authClients:      make(map[string]clientSet),
		authClientIDs:    make(map[chan clientMessage]string),
		defunctClients:   make(chan (chan clientMessage)),
		messages:         make(chan message),
		done:             make(chan bool),
		userIDReq:        make(chan chan []string),
	}
	return bi
}

// newBroker creates and starts a new SSE broker.
func newBroker() *broker {
	numInstances := sse.BrokerInstances
	if numInstances < 1 {
		numInstances = 1
	}

	b := &broker{
		instances:  make([]*brokerInstance, numInstances),
		newClients: make(chan clientData),
	}

	for i := 0; i < numInstances; i++ {
		b.instances[i] = newBrokerInstance(i + 1)
		b.instances[i].start(b)
	}

	return b
}

// UserIDs returns a list of user IDs connected to the SSE broker.
func (b *broker) UserIDs() []string {
	ids := []string{}
	ch := make(chan []string)
	for _, bi := range b.instances {
		bi.userIDReq <- ch
		ids = append(ids, <-ch...)
	}
	return ids
}
