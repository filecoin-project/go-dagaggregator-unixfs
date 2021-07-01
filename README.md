go-dagaggregator-unixfs
=======================

> A stateless aggregator for organizing arbitrary IPLD dags within a UnixFS hierarchy

[![GoDoc](https://godoc.org/github.com/filecoin-project/go-dagaggregator-unixfs?status.svg)](https://pkg.go.dev/github.com/filecoin-project/go-dagaggregator-unixfs)
[![GoReport](https://goreportcard.com/badge/github.com/filecoin-project/go-dagaggregator-unixfs)](https://goreportcard.com/report/github.com/filecoin-project/go-dagaggregator-unixfs)

This library provides functions and convention for aggregating multiple
arbitrary DAGs into a single superstructure, while preserving sufficient
metadata for basic navigation with pathing [IPLD selectors][1].

# Typical use case

Users who want to store relatively small ( below about 8GiB ) DAGs on Filecoin
often find it difficult to have their deal accepted, even in the presence of
Fil+. A solution to this is grouping the root CIDs of multiple non-related DAGs
into an "aggregate structure" which is then used to make a deal with a specific
miner. Naturally the resulting structure will have a new root CID, which
complicates both discovery and retrieval. Additionally certain limits need to be
respected otherwise such a structure can become unwieldy in other IPLD contexts
like IPFS.

This library provides basic solutions to the above problems.

# Spec

The test-fixture demonstrating everything below can be found at https://dweb.link/ipfs/bafybeib62b4ukyzjcj7d2h4mbzjgg7l6qiz3ma4vb4b2bawmcauf5afvua

## Grouping UnixFS structure

A "dag aggregation" UnixFS directory has the following structure:
- The first entry is a manifest file in [ND-JSON](http://ndjson.org/) format (detailed in next section)
- Every CID is represented as base32 CIDv1. Any `Qm...` CIDv0, is upgraded to CIDv1 as this operation is lossless.
- For browseability/recognizeability purposes, and to stay within bitswap limits, the **leading** 3 characters of a CIDv1 are combined with the 2 and 4 trailing characters for each directory sublevel, based on the calculations in the `Limits` section below.

This means a directory looking roughly like:
```
- bafyAggregateRootCid
  - @AggregateManifest.ndjson
  - baf...aa
    - baf...aaaa
    - baf...abaa
    …
    - baf...77aa
  - baf...ab
    - baf...aaab
    …
    - baf...77ab
  …
  - baf...77
    - baf...7777
        - bafymbzacid777777777777777777777777777777777777777777777777777777777777777777777777777777777777777777777777777777
```

## Manifest format

The Aggregate Manifest is an [ND-JSON](http://ndjson.org/) file comprised of 3 types of records.

### Preamble

This is always the first record of the manifest, it signals how to parse the
rest of the manifest.
```
{
  "RecordType": "DagAggregatePreamble",
  "Version": 1
}
```

### Summary

This is the second record within the manifest. Has entry count and various other
metadata.
```
{
  "RecordType": "DagAggregateSummary",
  "EntryCount": 4,
  "EntriesSortedBy": "DagCidV1",
  "Description": "Aggregate of non-related DAGs, produced by github.com/filecoin-project/go-dagaggregator-unixfs"
}
```

### Individual DAG Entries

The rest of the manifest records contain information about the included DAGs, one
per record.

```
{
  "RecordType": "DagAggregateEntry",
  "DagCidV1": "bafybeibhbx3y6tnn7q4gpsous6apnobft5jybvroiepdsmvps2lmycjjxu",
  "DagCidV0": "QmQy6xmJhrcC5QLboAcGFcAE1tC8CrwDVkrHdEYJkLscrQ",
  "DagSize": 42,
  "NodeCount": 1,
  "PathPrefixes": [ "baf...xu", "baf...jjxu" ],
  "PathIndexes": [ 2, 0, 0 ]
}
```

`PathPrefixes` contains the parent directories for this particular DAG, based on the chosen parts of the target CID. They currently can be derived from `DagCIDV1`, but are included for ease of navigation.

`PathIndexes` contains the 0-based position of each entry within its
corresponding parent directory. It is provided to make partial retrievals possible
with simple pathing selectors [as described in this lotus changeset](https://github.com/filecoin-project/lotus/pull/6393#issue-661783290)

## Limits in consideration
- A car file accepted by the Filecoin network currently can have a maximum payload size of about 65,024MiB ( 64<<30 / 128 * 127 ). For the sake of argument let's assume a future with 128GiB sectors, which gives an exact maximum payload size of 136,365,211,648 bytes. Assuming ~60 bytes for a car header, **shortest safe CID** representation of a CIDv0 sha2-256 at 34 bytes, and payload of 1024 bytes per block ( ridiculously small NFTs ), gives us an upper bound of (136365211648 - 60) / ( 2 + 34 + 1024) ~~ 128,646,426 ~~ an upper bound of 2^27 individual CIDs that could be in a deal and all be individually addressable.

- The **longest textual representation** of a "tenuously common" CID would be a `b`-multibase base32 representation of a [blake2b-512](https://cid.ipfs.io/#bafymbzacid777777777777777777777777777777777777777777777777777777777777777777777777777777777777777777777777777777), which clocks at 113 bytes. This in turn means that a typical UnixFS directory can contain:
1048576 = ( 4b dir hdr ) + N * ( 2b pbframe hdr + 2b CID prefix + 70b CID + 2b name prefix + 113b text-CID + ~5 bytes for prefixed size ) ~~ 5405 such names without sharding, before going over the 1MiB libp2p limit. In order to be super-conservative assume a target of 2^12 entries per "aggregate shard". If we go with the more reasonable 256-bit hashes, we arrive at ~9363 names which translates to 2^13 entries per shard.

- The common textual representation of CIDs is base32, each character of which represents exactly 5 bits. This means that sharding on 2 base32 characters gives a rough distribution of 2^10 per shard, fitting comfortably within the above considerations.

The limits described above, combined with the perfect distribution of hashes
within CIDs, means that one can safely store a "super-sector" full of
addressable CIDs by having 2 layers of directories, the first layer "sharded" by
2 base32 characters, the second layer by 4 base32 characters, and the final
container having the full CIDs pointing to the content: 2^(10+10+10) > 2^(27).
This does not even take into account the vast overestimation of sector and CID
sizes.

## Lead Maintainer

[Peter Rabbitson](https://github.com/ribasushi)

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)

[1]: https://pkg.go.dev/github.com/ipld/go-ipld-prime/traversal/selector
