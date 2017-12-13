# Conduit Dashboard

This is a React app. It uses webpack to bundle assets, and postcss to transform css.

## First time setup

```
brew install yarn
yarn
```

## Development

After pulling master:

If you just want to run the frontend:
```
cd web/app
yarn webpack
cd ..
go run main.go
```

For reloading assets:
```
cd web/app
yarn webpack-dev-server
cd ..
go run main.go -webpack-dev-server http://localhost:8080/
```

## Testing

### Golang unit tests
To run unit tests:
```
go test ./...
```