package server

//go:generate go run -modfile=../../../../tools/go.mod github.com/oapi-codegen/oapi-codegen-exp/cmd/oapi-codegen --config=server.gen.yaml ../../../../api/imagebuilder/v1alpha1/openapi.yaml
//go:generate go run -modfile=../../../../tools/api-metadata-extractor/go.mod ../../../../tools/api-metadata-extractor/main.go "../../../../api/imagebuilder/*/openapi.yaml" api_metadata_registry.gen.go server
