go-dagaggregator-unixfs
=======================

> A steteless aggregator for organizing arbitrary IPLD dags within a UnixFS hierarchy

[![GoDoc](https://godoc.org/github.com/filecoin-project/go-dagaggregator-unixfs?status.svg)](https://pkg.go.dev/github.com/filecoin-project/go-dagaggregator-unixfs)
[![GoReport](https://goreportcard.com/badge/github.com/filecoin-project/go-dagaggregator-unixfs)](https://goreportcard.com/report/github.com/filecoin-project/go-dagaggregator-unixfs)

This library provides functions and convention for agregating multiple
arbitrary DAGs into a single superstructure, while preserving sufficient
metadata for basic navigation with pathing [IPLD selectors][1].

## Lead Maintainer

[Peter Rabbitson](https://github.com/ribasushi)

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)

[1]: https://pkg.go.dev/github.com/ipld/go-ipld-prime/traversal/selector
