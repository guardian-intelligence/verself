package projectsinternalclient

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.6.0 -generate types,client -response-type-suffix HTTPResponse -package projectsinternalclient -o client.gen.go ../openapi/internal-openapi-3.0.yaml
