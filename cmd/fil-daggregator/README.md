fil-daggregator
=======================

> A basic utility for generating [DAG-aggregates](https://pkg.go.dev/github.com/filecoin-project/go-dagaggregator-unixfs#readme-typical-use-case) from a list of CIDs

## Installation

```
go install github.com/filecoin-project/go-dagaggregator-unixfs/cmd/fil-daggregator@latest
```

## Usage Example #1
Creating a DAG-aggregate and exporting it to a .car file. Assumes [go-ipfs](https://github.com/ipfs/go-ipfs) is installed.

```
ipfs daemon --offline
ipfs add file1  # output: QmNpiuBaHgQJP5KatAN2sqoW5p2eUp35nYzPXieNQmmHja
ipfs add file2  # output: QmZpapjGaKSqe91hw2bynRzSdMfTbVWm42QgxDRKfgLNou
fil-daggregator QmNpiuBaHgQJP5KatAN2sqoW5p2eUp35nYzPXieNQmmHja QmZpapjGaKSqe91hw2bynRzSdMfTbVWm42QgxDRKfgLNou  # output: bafybeicwi3cclqetgy4aldwcii4jxc7efaxau5zegcgbhumngtaufrysmm
ipfs dag export bafybeihbwnogqzu3q4drqgvr7yenpm3tdfvqu2xqpkdylkxren5yf7c7qu > daggregate.car
```

## Usage Example #2

```
curl -sL https://dweb.link/ipfs/bafybeibg7faulfv5tjxfd5o4mk2sdpxm2d4n37enzz6nte46lkz46uqhk4/@AggregateManifest.ndjson \
| jq -r '.DagCidV1, .DagCidV0 | select( . != null )' \
| GOLOG_LOG_FMT=json fil-daggregator
```

### Output

```
{"msg":"aggregation finished","aggregateRoot":"bafybeibg7faulfv5tjxfd5o4mk2sdpxm2d4n37enzz6nte46lkz46uqhk4","totalManifestEntries":4,"newIntermediateBlocks":6}
```

## License
[SPDX-License-Identifier: Apache-2.0 OR MIT](../../LICENSE.md)
