package profiles

// Template provides the base template for the `linkerd profile --template` command.
const Template = `### ServiceProfile for {{.ServiceName}}.{{.ServiceNamespace}} ###
apiVersion: linkerd.io/v1alpha1
kind: ServiceProfile
metadata:
  name: {{.ServiceName}}.{{.ServiceNamespace}}.{{.ClusterZone}}
  namespace: {{.ControlPlaneNamespace}}
spec:
  # A service profile defines a list of routes.  Linkerd can aggregate metrics
  # like request volume, latency, and success rate by route.
  routes:
  - name: '/authors/{id}'

    # Each route must define a condition.  All requests that match the
    # condition will be counted as belonging to that route.  If a request
    # matches more than one route, the first match wins.
    condition:
      # The simplest condition is a path regular expression.
      pathRegex: '/authors/\d+'

      # This is a condition that checks the request method.
      method: POST

      # If more than one condition field is set, all of them must be satisfied.
      # This is equivalent to using the 'all' condition:
      # all:
      # - pathRegex: '/authors/\d+'
      # - method: POST

      # Conditions can be combined using 'all', 'any', and 'not'.
      # any:
      # - all:
      #   - method: POST
      #   - pathRegex: '/authors/\d+'
      # - all:
      #   - not:
      #       method: DELETE
      #   - pathRegex: /info.txt

    # A route may be marked as retryable.  This indicates that requests to this
    # route are always safe to retry and will cause the proxy to retry failed
    # requests on this route whenever possible.
    # isRetryable: true

    # A route may optionally define a list of response classes which describe
    # how responses from this route will be classified.
    responseClasses:

    # Each response class must define a condition.  All responses from this
    # route that match the condition will be classified as this response class.
    - condition:
        # The simplest condition is a HTTP status code range.
        status:
          min: 500
          max: 599

        # Specifying only one of min or max matches just that one status code.
        # status:
        #   min: 404 # This matches 404s only.

        # Conditions can be combined using 'all', 'any', and 'not'.
        # all:
        # - status:
        #     min: 500
        #     max: 599
        # - not:
        #     status:
        #       min: 503

      # The response class defines whether responses should be counted as
      # successes or failures.
      isFailure: true

    # A route can define a request timeout.  Any requests to this route that
    # exceed the timeout will be canceled.  If unspecified, the default timeout
    # is '1s' (one second).
    # timeout: 250ms

  # A service profile can also define a retry budget.  This specifies the
  # maximum total number of retries that should be sent to this service as a
  # ratio of the original request volume.
  # retryBudget:
  #   The retryRatio is the maximum ratio of retries requests to original
  #   requests.  A retryRatio of 0.2 means that retries may add at most an
  #   additional 20% to the request load.
  #   retryRatio: 0.2

  #   This is an allowance of retries per second in addition to those allowed
  #   by the retryRatio.  This allows retries to be performed, when the request
  #   rate is very low.
  #   minRetriesPerSecond: 10

  #   This duration indicates for how long requests should be considered for the
  #   purposes of calculating the retryRatio.  A higher value considers a larger
  #   window and therefore allows burstier retries.
  #   ttl: 10s
`
