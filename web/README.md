# Boron Web UI

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
cd ~/workspace/conduit/web/app
yarn
yarn webpack
yarn webpack-dev-server
```

For reloading assets:
```
cd web/app
yarn
yarn webpack
yarn webpack-dev-server
```

## Run docker-compose
```
cd ~/workspace/conduit
docker-compose build
docker-compose up -d
docker-compose stop web
./bin/go-run web -static-dir=web/app/dist -template-dir=web/templates -webpack-dev-server=http://localhost:8080
```

## Testing

### Golang unit tests
To run unit tests:
```
go test ./...
```
