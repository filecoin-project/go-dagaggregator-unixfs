fil-daggregator
=======================

> A basic utility for generating [DAG-aggregates](https://pkg.go.dev/github.com/filecoin-project/go-dagaggregator-unixfs#readme-typical-use-case) from a list of CIDs

## Installation

```
go install github.com/filecoin-project/go-dagaggregator-unixfs/cmd/fil-daggregator@latest
```

## Usage Example

```
curl -sL https://dweb.link/ipfs/bafybeibg7faulfv5tjxfd5o4mk2sdpxm2d4n37enzz6nte46lkz46uqhk4/@AggregateManifest.ndjson \
| jq -r '.DagCidV1, .DagCidV0 | select( . != null )' \
| GOLOG_LOG_FMT=json fil-daggregator
```

## Output Example

```
{"msg":"aggregation finished","aggregateRoot":"bafybeibg7faulfv5tjxfd5o4mk2sdpxm2d4n37enzz6nte46lkz46uqhk4","totalEntries":4}
```

## License
[SPDX-License-Identifier: Apache-2.0 OR MIT](../../LICENSE.md)
