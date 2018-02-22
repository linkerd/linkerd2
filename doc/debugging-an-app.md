+++
title = "Example: debugging an app"
docpage = true
[menu.docs]
  parent = "debugging-an-app"
+++

This section assumes you've followed the steps in the [Getting
Started](/getting-started) guide and have Conduit and the demo application
running in some flavor of Kubernetes cluster.

## Using Conduit to debug a failing service 💻🔥
Now that we have Conduit and the demo application [up and
running](/getting-started), let's use Conduit to diagnose issues.

First, let's use the `conduit stat` command to get an overview of deployment
health:
#### `conduit stat deployments`

### Your results will be something like:
```
NAME                   REQUEST_RATE   SUCCESS_RATE   P50_LATENCY   P99_LATENCY
emojivoto/emoji              2.0rps        100.00%           0ms           0ms
emojivoto/voting             0.6rps         66.67%           0ms           0ms
emojivoto/web                2.0rps         95.00%           0ms           0ms
```

We can see that the `voting` service is performing far worse than the others.

How do we figure out what's going on? Our traditional options are: looking at
the logs, attaching a debugger, etc. Conduit gives us a new tool that we can use
- a live view of traffic going through the deployment. Let's use the `tap`
command to take a look at requests currently flowing through this deployment.

#### `conduit tap deploy emojivoto/voting`

This gives us a lot of requests:

```
req id=0:458 src=172.17.0.9:45244 dst=172.17.0.8:8080 :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VoteGhost
rsp id=0:458 src=172.17.0.9:45244 dst=172.17.0.8:8080 :status=200 latency=758µs
end id=0:458 src=172.17.0.9:45244 dst=172.17.0.8:8080 grpc-status=OK duration=9µs response-length=5B
req id=0:459 src=172.17.0.9:45244 dst=172.17.0.8:8080 :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VoteDoughnut
rsp id=0:459 src=172.17.0.9:45244 dst=172.17.0.8:8080 :status=200 latency=987µs
end id=0:459 src=172.17.0.9:45244 dst=172.17.0.8:8080 grpc-status=OK duration=9µs response-length=5B
req id=0:460 src=172.17.0.9:45244 dst=172.17.0.8:8080 :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VoteBurrito
rsp id=0:460 src=172.17.0.9:45244 dst=172.17.0.8:8080 :status=200 latency=767µs
end id=0:460 src=172.17.0.9:45244 dst=172.17.0.8:8080 grpc-status=OK duration=18µs response-length=5B
req id=0:461 src=172.17.0.9:45244 dst=172.17.0.8:8080 :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VoteDog
rsp id=0:461 src=172.17.0.9:45244 dst=172.17.0.8:8080 :status=200 latency=693µs
end id=0:461 src=172.17.0.9:45244 dst=172.17.0.8:8080 grpc-status=OK duration=10µs response-length=5B
req id=0:462 src=172.17.0.9:45244 dst=172.17.0.8:8080 :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
```

Let's see if we can narrow down what we're looking at. We can see a few
`grpc-status=Unknown`s in these logs. This is GRPCs way of indicating failed
requests.

Let's figure out where those are coming from. Let's run the `tap` command again,
and grep the output for `Unknown`s:

####  ```conduit tap deploy emojivoto/voting | grep Unknown -B 2```

```
req id=0:212 src=172.17.0.8:58326 dst=172.17.0.10:8080 :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
rsp id=0:212 src=172.17.0.8:58326 dst=172.17.0.10:8080 :status=200 latency=360µs
end id=0:212 src=172.17.0.8:58326 dst=172.17.0.10:8080 grpc-status=Unknown duration=0µs response-length=0B
--
req id=0:215 src=172.17.0.8:58326 dst=172.17.0.10:8080 :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
rsp id=0:215 src=172.17.0.8:58326 dst=172.17.0.10:8080 :status=200 latency=414µs
end id=0:215 src=172.17.0.8:58326 dst=172.17.0.10:8080 grpc-status=Unknown duration=0µs response-length=0B
--
```

We can see that all of the `grpc-status=Unknown`s are coming from the `VotePoop`
endpoint. Let's use the `tap` command's flags to narrow down our output to just
this endpoint:

####  ```conduit tap deploy emojivoto/voting --path /emojivoto.v1.VotingService/VotePoop```

```
req id=0:264 src=172.17.0.8:58326 dst=172.17.0.10:8080 :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
rsp id=0:264 src=172.17.0.8:58326 dst=172.17.0.10:8080 :status=200 latency=696µs
end id=0:264 src=172.17.0.8:58326 dst=172.17.0.10:8080 grpc-status=Unknown duration=0µs response-length=0B
req id=0:266 src=172.17.0.8:58326 dst=172.17.0.10:8080 :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
rsp id=0:266 src=172.17.0.8:58326 dst=172.17.0.10:8080 :status=200 latency=667µs
end id=0:266 src=172.17.0.8:58326 dst=172.17.0.10:8080 grpc-status=Unknown duration=0µs response-length=0B
req id=0:270 src=172.17.0.8:58326 dst=172.17.0.10:8080 :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
rsp id=0:270 src=172.17.0.8:58326 dst=172.17.0.10:8080 :status=200 latency=346µs
end id=0:270 src=172.17.0.8:58326 dst=172.17.0.10:8080 grpc-status=Unknown duration=0µs response-length=0B
```

We can see that none of our `VotePoop` requests are successful. What happens
when we try to vote for 💩 ourselves, in the UI? Follow the instructions in
[Step Five](/getting-started/#step-five) to open the demo app.

Now click on the 💩 emoji to vote on it.

{{< figure src="/images/emojivoto-poop.png" title="Demo application 💩 page" >}}

Oh! The demo application is intentionally returning errors for all requests to
vote for 💩. We've found where the errors are coming from. At this point, we
can start diving into the logs or code for our failing service. In future
versions of Conduit, we'll even be able to apply routing rules to change what
happens when this endpoint is called.
