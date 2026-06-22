module multi-tiered-serverless-simulator

go 1.25.0

require github.com/docker/docker v24.0.9+incompatible

replace github.com/distribution/reference => ./third_party/distribution_reference
replace github.com/docker/go-connections => ./third_party/docker_go_connections
replace github.com/docker/go-connections => ./third_party/docker_go_connections
replace github.com/docker/go-connections => ./third_party/docker_go_connections

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/distribution v2.8.3+incompatible // indirect
	github.com/docker/go-connections v0.7.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/morikuni/aec v1.1.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	gotest.tools/v3 v3.5.2 // indirect
)
