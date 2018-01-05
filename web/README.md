# Conduit Dashboard

This is a React app. It uses webpack to bundle assets, and postcss to transform css.

The commands below assume that you're starting from the root of the repo.

## First time setup

Install [yarn](https://github.com/yarnpkg/yarn) and use it to install dependencies.

```
brew install yarn
cd web/app
yarn
```

## Development

After pulling master:

If you just want to run the frontend:

```
cd web/app
yarn
yarn webpack
cd ..
go run main.go
```

The web server will be running on localhost:8084.

Note the web process depends on a public-api server, for which you have three options:

1. Boot public-api locally

    ```
    go run ../controller/cmd/public-api/main.go
    ```

2. Connect to public-api in docker-compose (see below)
3. Connect to public-api in Kubernetes (see below)

To develop with a webpack dev server, start the server in a separate window:

```
cd web/app
yarn webpack-dev-server
```

And then set the `-webpack-dev-server` flag when running the web server:

```
go run main.go -webpack-dev-server=http://localhost:8080
```

To add a JS dependency:

```
cd web/app
yarn add [dep]
```

## Run docker-compose

You can also run all of the go apps in a docker-compose environment.

From the root of the repo, run:

```
docker-compose build
docker-compose up -d
```

If you want to develop on the web service locally, stop it first, then run it
locally and set the `-api-addr` flag to the address of the public API server
that's running in your docker environment:

```
docker-compose stop web
cd web
go run main.go -api-addr=localhost:8085 # where localhost is the Docker address
```

## Run Web against a public API server in Kubernetes

If you are running the public API server in Kubernetes, forward localhost:8085
to the conduit controller pod:

```
POD_NAME=$(kubectl --namespace=conduit get po --selector=app=controller -o jsonpath='{.items[*].metadata.name}')
kubectl -n conduit port-forward $POD_NAME 8085:8085
```

Then connect the local web process to the forwarded port:

```
cd web
go run main.go -api-addr=localhost:8085
```

## Testing

### Golang unit tests

To run:

```
go test ./...
```

### JS unit tests

To run:

```
cd web/app
yarn karma start
```
