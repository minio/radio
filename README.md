> This project is not yet ready for production use
# Radio Quickstart Guide [![Slack](https://slack.min.io/slack?type=svg)](https://slack.min.io)
Redundant Array of Distributed Independent Objectstores in short `RADIO` performs synchronous mirroring, erasure coding across multiple object stores licensed under AGPLv3.0

## Features
- Mirroring
- Erasure Coding

## Sample Config `mirror`
```yml
mirror:
- local:
    bucket: radiobucket1
    access_key: Q3AM3UQ867SPQQA43P2F
    secret_key: zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG
  remote:
  - bucket: bucket1
    endpoint: http://domain1.com:9001
    access_key: TX8mIIOGC12QBMJ45F0Z
    secret_key: 9ule1ga5JMfMmQXCoEPNcM2jij
  - bucket: bucket2
    endpoint: http://domain2.com:9002
    access_key: GX82IIOGC12QBMJ45F0Z
    secret_key: 9ux11ga5JMfMmQXCoEPNcM2jij
  - bucket: bucket3
    endpoint: http://domain3.com:9003
    access_key: HX8KIIOGC12QBMJ45F0Z
    secret_key: 9ux41ga5JMfMmQXCoEPNcM2jij
```

## Sample Config `erasure`
```yml
erasure:
- parity: 1
  local:
    bucket: radiobucket1
    access_key: Q3AM3UQ867SPQQA43P2F
    secret_key: zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG
  remote:
  - bucket: bucket1
    endpoint: http://domain1.com:9001
    access_key: TX8mIIOGC12QBMJ45F0Z
    secret_key: 9ule1ga5JMfMmQXCoEPNcM2jij
  - bucket: bucket2
    endpoint: http://domain2.com:9002
    access_key: GX82IIOGC12QBMJ45F0Z
    secret_key: 9ux11ga5JMfMmQXCoEPNcM2jij
  - bucket: bucket3
    endpoint: http://domain3.com:9003
    access_key: HX8KIIOGC12QBMJ45F0Z
    secret_key: 9ux41ga5JMfMmQXCoEPNcM2jij
```

## Starting `radio`
```
radio server -C config.yml
```

Prints the following banner
```
Endpoint:  http://192.168.1.172:9000  http://172.17.0.1:9000  http://127.0.0.1:9000

Command-line Access: https://docs.min.io/docs/minio-client-quickstart-guide
   $ mc config host add myradio1 http://192.168.1.172:9000 Q3AM3UQ867SPQQA43P2F zuf+tfteSlswRu7BJ86wekitnifILbZam1KYY3TG
...
```

## License
This project is licensed under AGPLv3.0
```
// This file is part of Radio
// Copyright (c) 2019 MinIO, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.
 ```
