# Acknowledgements

Giridhar discussed causal metadata with Professor Lindsey Kuper. Furkan used Shun Kashiwa's suggestion on Zulip to implement down detection with broadcasting write operations. Besides that, we discussed the concepts of the assignment only within our group.

# Citations

- [Docker Docs for Go](https://docs.docker.com/language/golang/build-images/) for general assistance constructing the Dockerfile
- [Echo Docs](https://echo.labstack.com/docs) for the core implementation details and questions related to the Echo HTTP Framework.
- [Echo Middleware Explanation](https://medium.com/@rayato159/building-a-custom-middleware-in-go-echo-864acdecbe87) to implement custom logging middleware for debugging purposes.
- [Go Standard Library Docs](https://pkg.go.dev/std) for debugging concurrency issues with channels, waitgroups, and goroutines, syntax help, and discovering essential functions in the standard library.
- More granular Go References:
  - Used [this](https://builtin.com/software-engineering-perspectives/golang-enum) article as reference to implement ComparisonResult enum for comparing VectorClocks.
  - Used [this](https://gobyexample.com/variadic-functions) to implement helper that merges keys of two maps into a slice using variadic functions in Go.
  - Used [this](https://gobyexample.com/mutexes) to implement locks over vector clock changes.
  - Used [this](https://www.reddit.com/r/golang/comments/sbjgfp/remove_element_from_a_slice/) to remove elements from slices.
  - Used [this](https://stackoverflow.com/questions/27234861/correct-way-of-getting-clients-ip-addresses-from-http-request) to identify the IP address of the sending client in order to identify them in our Vector Clock implementation.
 
  Also consulted with [Gemini](https://gemini.google.com/app/) on the behavior of Echo's context.Bind (used with caution and cross-verified) due to a lack of easily findable documentation. In particular, this was to see if it was possible to bind to two different structs one after another (to be precise, one if the other failed) to handle a potential extra parameter. However, this approach became apparently incorrect, so it was not used. No application code was generated at any point.


# Team Contributions

## Furkan Ercevik

- Implemented write broadcasting for the key-value and data handlers synchronously with the use of arrays, by buffering at the sender, delaying for 200ms, and retrying the requests that received a 503 response with the same payload.
- Unit-tested broadcast implementation to ensure proper functionality
- Implemented down detection by leveraging the broadcast implementation to check which requests timed out (replica down) 
- Refactored view and key-value data handlers to consolidate logic and function as needed by the assignment specification
- Helped to implement a procedure to select the most up to date data from existing replicas when a new replica joined the network

## Giridhar Vadhul

- Devised vector clock strategy to maintain causal consistency
- Implemented Vector Clock Struct + Operations and Unit-Tested these.
- Integrated Vector Clock operations with existing Key-Value Operation Handlers (from Assignment 2).
- Devised and helped implement procedure for selecting the most up-to-date replica to copy data from (as a new replica) and implemented a corresponding handler to send all data to a new replica.
- Refactored View Handlers to use a centralized Broadcast routine.

## Alec Raymond

- Worked on Views Implementation
- Worked on registering a new view via /PUT to other replicas in the network.
- Worked on Heartbeat Down Detection (Deprecated)

# Design Decisions

## Down Detection

We initially decided to do down detection by utilizing a heartbeat mechanism. The idea was that upon startup, the replica would start sending requests to the `/views/health` endpoint of the other replicas in its view to ensure that they were alive. If at any point a replica failed the healthcheck, it would be removed from the current replica's view and this delete request would be broadcasted to the other 'live' replicas. The replica would perform these heartbeats at an interval of ~3 seconds in a separate goroutine/thread.  

This didn't work out well because it was too aggressive on startup and would result in the first replica that was launched to insufficiently wait for the last replica to startup, and subsequently initiate a delete view broadcast.  

To remedy this we used Shun's suggestion on Zulip to try down detection via broadcasted writes. This didn't involve changing much of our current broadcast implementation. All we had to do was delete views that didn't respond to the broadcasted write request. Our implementation of `deleteView` also prevented infinite broadcasting because it was idempotent and would not broadcast if the view state had already been updated. Upon broadcasting a write to other replicas in a separate thread, the sending replica would wait for a response from the target replica within a given timeout. If it failed to respond in that time, the target request's address would be appended to an array of structs containing addresses to retry, but with the error flag set. The caller would then iterate over this array and observe this flag and instead of retrying this request, it would delete this view and broadcast that deletion to the other live replicas. Since deletions don't infinitely broadcast, this process would eventually terminate. Had it received a 503 error status, the error flag would not be set and it would wait a little before retrying. 

A false-positive or false-negative might occur if there was an issue with the network or the replica (a traffic overload at the responding replica, for example) that resulted in a delay that was either greater or lesser than the timeouts/delays that we had configured in our Broadcast and/or BufferAtSender functions causing the Down Detection algorithm to incorrectly categorize the status of a replica.

## Causal Consistency


### Mechanism:

Use Vector Clocks at the replica level to track only PUT's and DELETE's to guarantee both causal consistency properties (GET's are not tracked).

### Data Structures:

Implemented Vector Clock as a Struct containing a Map from IP addresses of clients to their corresponding entry in the vector, as the number of clients may always change over time. As far as dependencies are concerned, clients reading from the Key-Value store are dependent on all clients who previously wrote, even if they are not live any more, so it makes sense to persist all clients who have a non-zero entry in this map and pass it around as causal metadata. In order to make it easy to identify the client when verifying if the replica is causally ready for write/read, we also include a string field in this struct to specify the client's IP.

### Design:

A replica is ready for an incoming read request if the causal metadata in the request has a) no keys (other clients) that the replica is unaware of (as this would mean the replica has not yet processed a necessary write) and b) the replica has the same number in the client's entry as the client has itself (meaning that the replica has seen all the client's prior writes, which is necessary to ensure it does not violate read-your-writes consistency). 

A replica is ready for an incoming write request if the causal metadata in the request has a) no keys (other clients) that the replica is unaware of (as that would mean the write request depends - probably through a read - on a write by another client, but the replica does not know of it), b) the replica has an equal entry in the client's entry as the client itself (the replica has seen all the client's prior writes, as the update for the current request will only be made at the client by the replica's response, and this write depends causally on previous writes by virtue of coming from the same process) and c) the replica has a greater or equal entry than the sending client for all other clients (all causal dependencies on previous writes are at least satisfied).

  - When a replica receives an acceptable read request (one who's dependencies are satisfied), it will transfer its vector clock to the requesting client (i.e. the reading client becomes dependent on all prior writes on the key-value store) without incrementing its own (as we don't count reads as events).
  - When a replica receives an acceptable write request, it will increment the client's own entry in both the client's and its own vector clocks. It is not necessary to make the client dependent on previous writes from other clients that it is unaware of, because the second part of our redefinition of happens-before indicates that two writes may be causally related only if a) they ensue from the same process or b) are causally attached by some intermediate read, which retrieves the entire causal history of the key-value store.

As implemented in Data Handlers, all requests for which the replica does not have sufficient causal information are responded to with a 503 status code.

## View

- When a new node joins the network it sends a PUT-view request which is then broadcasted to all existing replicas
- When a view isn't reachable in a broadcasted write, we send a DELETE-view request to the broadcaster, which is then broadcasted to all other replicas in the view.
- PUT-view and DELETE-view are idempotent (i.e. if a replica is already in the view, it will not be added again and if it is already not, it cannot be deleted again, but the requester will not be made aware of this status.)

## Broadcasting

- Implement buffering at the sender, i.e. wait a bit then send, and retry as
  needed upon receiving 503 error.
- Do not retry requests that time out or respond with a non-503 error code
- Delete replicas that fail to respond to the broadcast
