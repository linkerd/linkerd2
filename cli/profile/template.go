package profile

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
      path: '^/authors/\d+$'

      # This is a condition that checks the request method.
      method: POST

      # If more than one condition field is set, all of them must be satisfied.
      # This is equivalent to using the 'all' condition:
      # all:
      # - path: '^/authors/\d+$'
      # - method: POST

      # Conditions can be combined using 'all', 'any', and 'not'.
      # any:
      # - all:
      #   - method: POST
      #   - path: '^/authors/\d+$'
      # - all:
      #   - not:
      #       method: DELETE
      #   - path: ^/info.txt$

    # A route may optionally define a list of response classes which describe
    # how responses from this route will be classified.
    response_classes:

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
      is_failure: true
`
