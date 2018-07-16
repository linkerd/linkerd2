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
#### `conduit -n emojivoto stat deploy`

### Your results will be something like:
```
NAME       MESHED   SUCCESS      RPS   LATENCY_P50   LATENCY_P95   LATENCY_P99
emoji         1/1   100.00%   2.0rps           1ms           4ms           5ms
vote-bot      1/1         -        -             -             -             -
voting        1/1    89.66%   1.0rps           1ms           5ms           5ms
web           1/1    94.92%   2.0rps           5ms          10ms          18ms
```

We can see that the `voting` service is performing far worse than the others.

How do we figure out what's going on? Our traditional options are: looking at
the logs, attaching a debugger, etc. Conduit gives us a new tool that we can use
- a live view of traffic going through the deployment. Let's use the `tap`
command to take a look at all requests currently flowing to this deployment.

#### `conduit -n emojivoto tap deploy --to deploy/voting`

This gives us a lot of requests:

```
req id=0:1624 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VoteDoughnut
rsp id=0:1624 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :status=200 latency=1603µs
end id=0:1624 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs grpc-status=OK duration=28µs response-length=5B
req id=0:1629 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VoteBeer
rsp id=0:1629 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :status=200 latency=2009µs
end id=0:1629 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs grpc-status=OK duration=24µs response-length=5B
req id=0:1634 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VoteDog
rsp id=0:1634 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :status=200 latency=1730µs
end id=0:1634 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs grpc-status=OK duration=21µs response-length=5B
req id=0:1639 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VoteCrossedSwords
rsp id=0:1639 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :status=200 latency=1599µs
end id=0:1639 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs grpc-status=OK duration=27µs response-length=5B
```

Let's see if we can narrow down what we're looking at. We can see a few
`grpc-status=Unknown`s in these logs. This is GRPCs way of indicating failed
requests.

Let's figure out where those are coming from. Let's run the `tap` command again,
and grep the output for `Unknown`s:

####  ```conduit -n emojivoto tap deploy --to deploy/voting | grep Unknown -B 2```

```
req id=0:2294 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
rsp id=0:2294 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :status=200 latency=2147µs
end id=0:2294 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs grpc-status=Unknown duration=0µs response-length=0B
--
req id=0:2314 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
rsp id=0:2314 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :status=200 latency=2405µs
end id=0:2314 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs grpc-status=Unknown duration=0µs response-length=0B
--
```

We can see that all of the `grpc-status=Unknown`s are coming from the `VotePoop`
endpoint. Let's use the `tap` command's flags to narrow down our output to just
this endpoint:

####  ```conduit -n emojivoto tap deploy/voting --path /emojivoto.v1.VotingService/VotePoop```

```
req id=0:2724 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
rsp id=0:2724 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :status=200 latency=1644µs
end id=0:2724 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs grpc-status=Unknown duration=0µs response-length=0B
req id=0:2729 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
rsp id=0:2729 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :status=200 latency=1736µs
end id=0:2729 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs grpc-status=Unknown duration=0µs response-length=0B
req id=0:2734 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :method=POST :authority=voting-svc.emojivoto:8080 :path=/emojivoto.v1.VotingService/VotePoop
rsp id=0:2734 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs :status=200 latency=1779µs
end id=0:2734 src=10.1.8.150:56224 dst=voting-6795f54474-6vfbs grpc-status=Unknown duration=0µs response-length=0B
```

We can see that none of our `VotePoop` requests are successful. What happens
when we try to vote for 💩 ourselves, in the UI? Follow the instructions in
[Step Five](/getting-started/#step-five) to open the demo app.

Now click on the 💩 emoji to vote on it.

![](images/emojivoto-poop.png "Demo application 💩 page")

Oh! The demo application is intentionally returning errors for all requests to
vote for 💩. We've found where the errors are coming from. At this point, we
can start diving into the logs or code for our failing service. In future
versions of Conduit, we'll even be able to apply routing rules to change what
happens when this endpoint is called.
