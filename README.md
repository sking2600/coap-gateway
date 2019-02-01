# Coap-Gateway

## Overview

This is an implementation of the OCF 2.0 spec extension "CoAP Native Cloud" for Kubernetes.
The OCF cloud spec in some ways is a simple spec with relatively few methods (see below) but contains subtle implicit complexities that have a major impact on the architectural requirements. 



# Architecture
## CoAP Interface: (this is the only part of the cloud architecture that is thoroughly spec'd by the OCF)

The OCF cloud specification stipulates that devices must maintain an arbitrarily long-lived connection to the cloud in order to be available for handling requests originating from remote mobile/web applications. These "mandatory sticky-sessions" introduce unique challenges to the pods running the CoAP interface with regard to dealing with load balancing (L7 load balancers don't support CoAP and L4 load balancers can't reroute existing TCP connections, which can result in an uneven distribution of load) and routing requests from an mobile or cloud app to the device (how do you know which pod is connected to your target device?). 
### Message Routing: 
As part of handling devices "logging in" via oic/sec/session, the CoAP interface registers the device-uuid in redis as a key which maps to The pod IP address. This is used, in conjunction with Kubernetes' DNS service, to send requests to the correct pod (ex: POST "http://10-168-42-1.default.pod.cluster.local:8081/device-uuid/target-href). NOTE: In order to implement support for asynchronous requests and responses, I plan on putting those requests into a worker queue. I'll be using the [CloudEvents specification for http webhooks](https://github.com/cloudevents/spec/blob/master/http-webhook.md) as a reference for implementing this.
### Load Balancing: 
(note: I have not implemented this yet) Without additional intervention, a device will maintain a connection to the same pod forever. This is unacceptable in a distributed system (and feels like a violation of the 12 factor app principles). As such, it is important to implement mechanisms for closing the connection and getting the device to reconnect to the cloud so it can be routed to a different pod via the L4 load balancer. The most "ungraceful" method of doing this is to have the cloud simply close the connection and rely on the device to detect that and reconnect by itself. Superior methods involve using the RELEASE and ABORT message codes from RFC8323 so that the device knows to reconnect for load balancing purposes rather than thinking that the cloud was being unresponsive/unavailable (which would result in inaccurate errors in the "clec" field).
## Northbound Interface:
It is assumed that at least initially, clients will either be mobile or web apps which are capable of, and have better library support for, HTTP. As such, the "northbound interface" represents the HTTP server which allows you to register users, HTTP clients and mediators, as well as provisioning devices (note: device registration is only supported through the coap-interface at this time). Because user/mediator registration is explicitly out of scope for the OCF cloud spec, I had to decide on my own endpoints and what schemas I want. In the future, I hope to involve the OCF cloud task group in refining these. TODO: list all the HTTP endpoints.
## Registry: 
The registry interface combines both a MySQL and Redis database together, however this feels like a poor design decision and I will likely refactor this so that redis is part of a "cache" interface instead
### MySQL: 
the SQL database stores all users, mediators, devices (including published resources), clients and tokens. It is my hope in the future to support provisioning mediator tokens with different permissions using [OPA](https://www.openpolicyagent.org/). Examples include; a given mediator token only being allowed to provision devices but not clients, provisioned clients only being allowed to send requests during business hours or any clients provisioned with a specific mediator only being allowed to control a specific deviceID). It seems important to eventually support such granular access control policies given the [very real danger that consumer devices pose to infrastructure](https://arxiv.org/pdf/1808.02131.pdf) as well as [their ability to be misused in unethical ways](https://www.nytimes.com/2018/06/23/technology/smart-home-devices-domestic-abuse.html) although more benign examples like temporary house guests (ex: Airbnb and mother-in-laws) are arguably a more compelling justification for most users. 
### Redis: 
the Redis database currently only stores a mapping between device-uuid and the ip address of the pod that's currently maintaining a long-lived connection with that device. Design Note: it is essential that the pod ip be stored in redis rather than MySQL (or utilize pubsub as an alternative approach to message routing) because looking up a single key has O(1) time complexity which is required in order to scale to production sized workloads. In the future, I hope to leverage redis as a cache for some of the data stored in the SQL database (ex: access tokens) in order to help with scaling.
# Getting Started
-In order to follow this guide, you will need: access to a Kubernetes cluster, a MySQL database and a Redis database. I personally used GKE (GCP Kubernetes service) and the free database offerings from db4free.net (mysql) and [Redis](https://redislabs.com/blog/redis-cloud-30mb-ram-30-connections-for-free/) in order to write this code.

-Once you get the credentials/config/endpoints for your databases, you should update the registry-configmap.yaml file with those values (future releases will utilize k8s secrets or hashicorp vault for these credentials). If you decided to build from source rather than use my pre-built images, then you will also need to update coap-deployment.yaml and http-deployment.yaml (TODO improve the names) with links to your image

-When you have completed modifying your yaml files, all you need to do to deploy the code is kubectl apply -f \<your filepath that contains all the k8s yaml\>  and wait a couple minutes for all of the containers and load balancers to spin up. you can use "kubectl get service" to get the external-ip address of the load balancer that you should point your devices and REST client at. Please note that I am using "localhost" rather than a load balancer IP address in this guide.


## Provisioning/Registering users, devices and clients:

Now that the infrastructure is set up, start by registering a user (NOTE: there are several competing options for authenticating users, such as [OAuth](https://oauth.net/2/) and [DID](https://w3c-ccg.github.io/did-spec/), but I decided to not implement either for the time being in order to focus on the overall architecture and spec implementation)

    curl -X POST  -i http://localhost:8080/register/user --data '{
    "uid":"satoshi@btc.com",
    "authprovider":"stub"
    }'

    ------RESPONSE----------
    HTTP/1.1 200 OK
    Date: Wed, 16 Jan 2019 16:17:57 GMT
    Content-Length: 62
    Content-Type: text/plain; charset=utf-8

    {"accesstoken":"atRV9XpdKFjX2EhaqDll8tNkxCE0KwJHC2n3nKB7L54="}
    ---------------------

This request should return an access token that can be used to provision a mediator

to provision a mediator, execute the following command:

    curl -X POST  -i http://localhost:8080/provision/mediator --data '{
    "uid":"satoshi@btc.com",
    "accesstoken":"<your access token>"
    }'

    --------RESPONSE-------------
    HTTP/1.1 200 OK
    Date: Wed, 16 Jan 2019 16:19:33 GMT
    Content-Length: 62
    Content-Type: text/plain; charset=utf-8

    {"accesstoken":"zxQDzRcz0fOERjql5xu_-X1HGu43YV1go5NnQ4O-fzw="}
    ---------------------------------

the response to this request should be a mediator token. Now you can use that mediator token to provision clients and devices:

    curl -X POST -H 'Authorization: Bearer <your mediator token>' -i http://localhost:8080/provision/client --data '{"di":"OCF-cloud-client-test-uuid"}'

    curl -X POST -H 'Authorization: Bearer <your mediator token>' -i http://localhost:8080/provision/device --data '{"di":"OCF-cloud-device-test-uuid"}'

    ---------RESPONSE (equivalent for client/device)------------
    HTTP/1.1 200 OK
    Date: Wed, 16 Jan 2019 16:21:12 GMT
    Content-Length: 62
    Content-Type: text/plain; charset=utf-8

    {"accesstoken":"BjvEd8FFH0OCtQUXTRuqnVEz5FRK3WiAewfA4vq624w="}
    -----------------------------------------------------------

before passing off the device credentials to a device, let's register our client with the one-time access token that we got from the provisioning step

    curl -X POST -i http://localhost:8080/oic/sec/account --data '{
    "uid":"satoshi@btc.com",
    "di":"OCF-cloud-client-test-uuid",
    "accesstoken":"<token from client provisioning response>"  
    }'

    -------------RESPONSE-------------
    HTTP/1.1 200 OK
    Date: Wed, 16 Jan 2019 16:23:42 GMT
    Content-Length: 165
    Content-Type: text/plain; charset=utf-8

    {"accesstoken":"r96uuU9iW2xXWmkAZ9YcCCGXF56rG_fVX7xH7-9gWlY=","refreshtoken":"wTrPL0RNjwICS4MFbjwJX9CUsYkQNiUNTrtn1g9ydTI=","expiresin":6000,"uid":"satoshi@btc.com"}
    ----------------------------------

the response to this request includes: userID, accessToken, refreshToken and the TTL for the access token in seconds

## Connecting a device to the cloud and sending it requests:

Now we can connect our device to our server (or, in my case, run my mock device to test things)

    ~/go/src/github.com/sking2600/coap-gateway/cmd/northbound-interface$ go run *.go 

The mock device application will proceed to ask you for the ip address/URL of the server you want to connect with (if you're running tho coap-interface on your computer then it would be 'localhost') as well as the userID, deviceID and access/refresh/mediated tokens. Then it will attempt to establish a TCP session with the coap-interface and prompt you to say  what actions you want it to take (currently supporting logging in to oic/sec/session, registering with mediatedToken and publishing its resources).

once the device is connected and your HTTP client has an access token, you can start sending commands to your device:

    curl -X POST -H 'Authorization: <your client access token>' -i 'http://localhost:8080/OCF-cloud-device-test-uuid/myhref' -d '{my req payload}'

    --------------RESPONSE-----------
    HTTP/1.1 200 OK
    Date: Wed, 16 Jan 2019 16:45:06 GMT
    Content-Length: 33
    Content-Type: text/plain; charset=utf-8

    test response payload from device
    --------------------------------

you can confirm that all services properly recieved/handled the requests by looking at the logs

## request/response/endpoints specified in the OCF cloud spec:
    UPDATE/oic/sec/account {deviceID, mediated token, authProvider (optional)} returns {access token, userID, refresh token, expires in, redirect URI (optional)}
    DELETE /oic/sec/account {access token, userID OR device/clientID}
    UPDATE/oic/sec/session {deviceID, userID, loginBool, access token} returns {expires in} 
    UPDATE/oic/rd {resources (with links and stuff)} returns a "success response" 
    UPDATE/oic/sec/tokenrefresh {userID, deviceID, refresh token} returns (access token, refresh token, expires in) <- refresh token can be new or old.


## Things currently not implemented: 
* resource discovery
* deleting users/clients/devices
* various parts of the registry implementation aren't verifying tokens