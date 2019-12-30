module github.com/minio/radio

go 1.13

require (
	github.com/djherbis/atime v1.0.0
	github.com/dustin/go-humanize v1.0.0
	github.com/gorilla/mux v1.7.0
	github.com/minio/cli v1.22.0
	github.com/minio/highwayhash v1.0.0
	github.com/minio/lsync v1.0.1
	github.com/minio/mc v0.0.0-20191012041914-735aa139b19c
	github.com/minio/minio v0.0.0-20191208215804-3c30e4503da2
	github.com/minio/minio-go/v6 v6.0.44
	github.com/minio/sha256-simd v0.1.1
	github.com/ncw/directio v1.0.5
	github.com/prometheus/client_golang v0.9.3-0.20190127221311-3c4408c8b829
	github.com/rs/cors v1.6.0
	github.com/secure-io/sio-go v0.3.0
	github.com/skyrings/skyring-common v0.0.0-20160929130248-d1c0bb1cbd5e
	github.com/valyala/tcplisten v0.0.0-20161114210144-ceec8f93295a
	go.uber.org/atomic v1.3.2
	golang.org/x/crypto v0.0.0-20190923035154-9ee001bba392
	gopkg.in/yaml.v2 v2.2.2
)

// Added for go1.13 migration https://github.com/golang/go/issues/32805
replace github.com/gorilla/rpc v1.2.0+incompatible => github.com/gorilla/rpc v1.2.0

// Allow this for offline builds
replace github.com/eapache/go-xerial-snappy => github.com/eapache/go-xerial-snappy v0.0.0-20180814174437-776d5712da21

replace github.com/eapache/queue => github.com/eapache/queue v1.1.0

replace github.com/mattn/go-runewidth => github.com/mattn/go-runewidth v0.0.4

replace github.com/mitchellh/mapstructure => github.com/mitchellh/mapstructure v1.1.2
