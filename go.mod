module github.com/minio/radio

go 1.13

require (
	github.com/djherbis/atime v1.0.0
	github.com/dustin/go-humanize v1.0.0
	github.com/gorilla/mux v1.7.0
	github.com/minio/cli v1.22.0
	github.com/minio/highwayhash v1.0.0
	github.com/minio/lsync v1.0.1
	github.com/minio/minio v0.0.0-20191231040613-0b7bd024fb30
	github.com/minio/minio-go/v6 v6.0.45-0.20191213193129-a5786a9c2a5b
	github.com/minio/sha256-simd v0.1.1
	github.com/ncw/directio v1.0.5
	github.com/prometheus/client_golang v0.9.3-0.20190127221311-3c4408c8b829
	github.com/rs/cors v1.6.0
	github.com/secure-io/sio-go v0.3.0
	github.com/skyrings/skyring-common v0.0.0-20160929130248-d1c0bb1cbd5e
	github.com/stretchr/testify v1.4.0 // indirect
	github.com/valyala/tcplisten v0.0.0-20161114210144-ceec8f93295a
	go.uber.org/atomic v1.4.0
	golang.org/x/crypto v0.0.0-20191117063200-497ca9f6d64f
	gopkg.in/yaml.v2 v2.2.4
)

// Added for go1.13 migration https://github.com/golang/go/issues/32805
replace github.com/gorilla/rpc v1.2.0+incompatible => github.com/gorilla/rpc v1.2.0
