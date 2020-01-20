# Radio Quickstart Guide [![Slack](https://slack.min.io/slack?type=svg)](https://slack.min.io)
Redundant Array of Distributed Independent Objectstores in short `RADIO` performs synchronous mirroring, erasure coding across multiple object stores licensed under *AGPLv3.0*

## Features
- Mirroring
- Erasure Coding (In-progress)

## Architecture
[![RADIO](https://raw.githubusercontent.com/minio/radio/master/.github/arch.svg?sanitize=true)](https://min.io)

## Sample Config
```yml
---
## Distributed mode for distributed locking
## across many radio instances
distribute:
  peers: https://server{1...32}:9000/
  token: 32bytestring
  certs:
    cert_file: /etc/certs/public.crt
    key_file: /etc/certs/private.key
    ca_path: /etc/certs/CAs

## Local caching based on MinIO
## caching implementation
cache:
  drives:
    - /mnt/cache1
    - /mnt/cache2
    - /mnt/cache3
  exclude:
    - bucket1/*
    - "*.db"
  quota: 90
  expiry: 30

## Radio buckets configuration with all its remotes
## Supports two protection schema's
## - replicate
## - erasure (with parity)
buckets:
  radiobucket1:
    access_key: Q3AM3UQ867SPQQA43P2F
    secret_key: zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG
    protection:
      scheme: replicate
    remote:
      - access_key: TX8mIIOGC12QBMJ45F0Z
        bucket: bucket1
        endpoint: http://replica1:9000
        secret_key: 9ule1ga5JMfMmQXCoEPNcM2jij
      - access_key: GX82IIOGC12QBMJ45F0Z
        bucket: bucket2
        endpoint: http://replica2:9000
        secret_key: 9ux11ga5JMfMmQXCoEPNcM2jij
      - access_key: HX8KIIOGC12QBMJ45F0Z
        bucket: bucket3
        endpoint: http://replica3:9000
        secret_key: 9ux41ga5JMfMmQXCoEPNcM2jij
  radiobucket2:
    access_key: Q3AM3UQ867SPQQA43P2F
    secret_key: zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG
    protection:
      scheme: erasure
      parity: 1
    remote:
      - access_key: TX8mIIOGC12QBMJ45F0Z
        bucket: bucket1
        endpoint: http://erasure1.com:9000
        secret_key: 9ule1ga5JMfMmQXCoEPNcM2jij
      - access_key: GX82IIOGC12QBMJ45F0Z
        bucket: bucket2
        endpoint: http://erasure2.com:9000
        secret_key: 9ux11ga5JMfMmQXCoEPNcM2jij
      - access_key: HX8KIIOGC12QBMJ45F0Z
        bucket: bucket3
        endpoint: http://erasure3.com:9000
        secret_key: 9ux41ga5JMfMmQXCoEPNcM2jij
```

## Starting `radio`
```
radio server -c config.yml
```

Prints the following banner
```
Endpoint:  http://192.168.1.172:9000  http://172.17.0.1:9000  http://127.0.0.1:9000

Command-line Access: https://docs.min.io/docs/minio-client-quickstart-guide
   $ mc config host add myradio1 http://192.168.1.172:9000 Q3AM3UQ867SPQQA43P2F zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG
...
```

# License
RADIO is an free software project released under the [AGPLv3.0](https://github.com/minio/radio/blob/master/LICENSE) (Affero General Public License).

# Contributing
Please see our [Code of conduct](https://github.com/minio/radio/blob/master/code_of_conducts.md). We welcome your contributions. Please feel free to fork the code, play with it, make some patches and send us pull requests via [issues](https://github.com/minio/radio/issues).
