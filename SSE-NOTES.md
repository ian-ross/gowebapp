# Notes on SSE broker implementation

These notes give a quick description of the SSE broker implementation
I've added to the `gowebapp` application.

## Overall structure

The additional code is all in `app/shared/sse/sse.go`, with a few
small changes in other places to create the SSE broker and to make the
SSE functionality available to the main server part of the code.

Although we originally talked about having an SSE broker that ran in a
single goroutine, I've implemented a slight extension of that, since
you were concerned about scalability. The SSE broker runs as a
selected number of goroutines, specified by the `SSE.BrokerInstances`
value in the JSON configuration file. (For the testing that I've done,
I've used 3 goroutines.)

Each of these goroutines is represented by an instance of a
`brokerInstance` data structure, which includes the following fields:

 - `label`: An integer label for the broker instance, used for
   logging.
 - `broadcastClients`: The set of clients managed by the broker
   instance for broadcast messages -- this includes all connected
   clients, authenticated and unauthenticated. Each client is
   represented by a channel over which messages can be sent to the
   client.
 - `authClients`, `authClientIDs`: The set of authenticated clients
   managed by the broker instance, as maps for looking up from user ID
   to message channel, and vice versa.
 - `defunctClients`: A channel for removing clients whose HTTP
   connections have been closed.
 - `messages`: A channel for injecting messages into the broker
   instance. Messages are either broadcast messages, or messages
   directed to a single user, distinguished the presence or absence of
   a user ID.
 - `done`: A channel to trigger broker shutdown.
 - `userIDReq`: A channel used to extract the list of user IDs for
   authenticated clients of the broker instance. This is currently
   only used by the demo code.

The broker instances share a channel for adding new clients to the
overall SSE broker. When a client requests the `/events` SSE endpoint,
one broker instance (selected at random using Go's usual channel
selection semantics) adds the client connection and subsequently
handles forwarding any messages to that client.


## Messaging

Messages are sent in standard SSE `text/stream` format, with a
`broadcast` event type being used for broadcast messages, and an
`individual` event type for messages directed to individual users.

In the client code (in `static/js/global.js`), a Javascript
`EventSource` is used to receive these messages:

``` javascript
var evtSource;

function sseStart()
{
  console.log('sseStart');
  evtSource = new EventSource('/events');
  console.log(evtSource);
  evtSource.addEventListener("broadcast", function(e) {
    console.log("SSE BROADCAST: " + e.data);
  })
  evtSource.addEventListener("individual", function(e) {
    console.log("SSE INDIVIDUAL: " + e.data);
  })
  evtSource.onerror = function() {
    console.log("EventSource ERROR");
  }
}
```

SSE messages are agnostic to the format of the message payload, so
either simple text messages or payloads encoded using JSON can be sent
to clients.


## Testing

I have included some code in the Go server process to demonstrate how
SSE messages can be generated. (See the sections delimited by comments
saying "`DEMO CODE`" in `app/shared/server/server.go`.) This
demonstration code generates broadcast messages randomly at an average
rate of one per second, and sends user-specific messages to each
authenticated user randomly at an average rate of one message ever 10
seconds.

I have run the Go server process using this demonstration code
connected to 5000 clients on my laptop without any trouble. There is a
Python demonstration client in the `demo-client` directory that can be
used for this. It connects to the Go server process, registers a user,
logs in, then connects to the SSE endpoint and logs messages received,
running multiple connections in multiple threads (i.e.
`./demo-client.py 5000` will create 5000 client connections).

(See the caveats below about file descriptors before trying this!)


## Caveats

There are a couple of caveats that go with this code:

1. File descriptor limits

Each client that wants to receive server-sent events must have a
long-lived HTTP connection to the server process. This consumes a file
descriptor for the server process, and it's easy to reach the
per-process file descriptor limit imposed by the operating system. On
Linux, the soft value of this limit is commonly 1024 file descriptors.
The hard limit is often much larger. To allow the Go server process
(and any test client) to use more file descriptors, do the following:

``` shell
$ # Find the current soft limit
$ ulimit -Sn
1024
$ # Find the hard limit
$ ulimit -Hn
524288
$ # Set the current limit to the hard limit
$ ulimit -n 524288
```

This will allow the server process to maintain up to 524288 open file
descriptors, allowing it to maintain open SSE connections to a large
number of clients.

(You will probably want to tune this, depending on how many clients
you expect. There will need to be one file descriptor available per
SSE connection, plus a number for the main server HTTP connection
pool, database connections, file output, standard output and so on.)

2. Horizontal scaling

As we discussed when scoping the work, the approach used here will
*not* work correctly if you horizontally scale your server process,
i.e. if you run a number of identical server processes behind a load
balancer. This is because persistent SSE connections are per-process,
and if there are multiple server processes, messages generated in one
server process will not be sent to clients connected to other server
processes.

If you need to scale in this way, an additional publish/subscribe
layer will be needed to communicate SSE messages between server
processes. There are a number of options for doing this, but the best
choice to do it depends on the infrastructure that you're already
using, and isn't in scope for making changes to the very general
demonstration application you have here.

