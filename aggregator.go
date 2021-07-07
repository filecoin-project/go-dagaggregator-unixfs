package dagaggregator

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"strings"

	"github.com/ipfs/go-cid"
	chunker "github.com/ipfs/go-ipfs-chunker"
	ipldformat "github.com/ipfs/go-ipld-format"
	"github.com/ipfs/go-merkledag"
	"github.com/ipfs/go-unixfs"
	"github.com/ipfs/go-unixfs/importer/balanced"
	importhelper "github.com/ipfs/go-unixfs/importer/helpers"
	"github.com/multiformats/go-multicodec"
	"golang.org/x/xerrors"
)

type RecordType string

const (
	DagAggregatePreamble RecordType = `DagAggregatePreamble`
	DagAggregateSummary  RecordType = `DagAggregateSummary`
	DagAggregateEntry    RecordType = `DagAggregateEntry`
)

// AggregateManifestFilename must be a name go-unixfs will sort first in the
// final structure. Since ADL-free selectors-over-names are currently difficult
// instead one can simply say "I want the cid of the first link of the root
// structure" and be reasonably confident they will get to this file.
const AggregateManifestFilename = `@AggregateManifest.ndjson`

type ManifestPreamble struct {
	RecordType RecordType
	Version    uint32
}

// CurrentManifestPreamble is always encoded as the very first line within the AggregateManifestFilename
var CurrentManifestPreamble = ManifestPreamble{
	Version:    1,
	RecordType: DagAggregatePreamble,
}

type ManifestSummary struct {
	RecordType      RecordType
	EntryCount      int
	EntriesSortedBy string
	Description     string
}

type ManifestDagEntry struct {
	rootCid      cid.Cid // private
	RecordType   RecordType
	DagCidV1     string
	DagCidV0     string    `json:",omitempty"`
	DagSize      *uint64   `json:",omitempty"`
	NodeCount    *uint64   `json:",omitempty"`
	PathPrefixes [2]string // not repeating the DagCid as segment#3 - too long
	PathIndexes  [3]int
}

type AggregateDagEntry struct {
	RootCid                   cid.Cid
	UniqueBlockCount          uint64 // optional amount of blocks in dag, recorded in manifest
	UniqueBlockCumulativeSize uint64 // optional dag size, used as the TSize in the unixfs link entry and recorded in manifest
}

type nodeMap map[string]*merkledag.ProtoNode

// Aggregate de-duplicates and orders the supplied list of `AggregateDagEntry`-es
// and adds them into a two-level UnixFSv1 directory structure. The intermediate
// blocks comprising the directory tree and the manifest json file are written
// to the supplied DAGService. No "temporary blocks" are produced in the process:
// everything written to the DAGService is part of the final DAG capped by the
// final `aggregateRoot`.
//
// Note: CIDs based on the IDENTITY multihash 0x00 are silently excluded from
// aggregation, and are not reflected in the manifest.
func Aggregate(ctx context.Context, ds ipldformat.DAGService, toAggregate []AggregateDagEntry) (aggregateRoot cid.Cid, aggregateManifest []*ManifestDagEntry, err error) {

	dags := make(map[string]*ManifestDagEntry, len(toAggregate))

	for _, d := range toAggregate {

		cv1 := cid.Undef
		if d.RootCid.Version() == 1 {
			cv1 = d.RootCid
		} else {
			cv1 = cid.NewCidV1(d.RootCid.Type(), d.RootCid.Hash())
		}

		// do not consider identity multihashes for aggregation
		if cv1.Prefix().MhType == uint64(multicodec.Identity) {
			continue
		}

		cv0 := cid.Undef
		if cv1.Prefix().Codec == uint64(multicodec.DagPb) &&
			cv1.Prefix().MhType == uint64(multicodec.Sha2_256) &&
			cv1.Prefix().MhLength == 32 {
			cv0 = cid.NewCidV0(cv1.Hash())
		}

		dagName := cv1.String()

		if _, exists := dags[dagName]; !exists {
			dags[dagName] = &ManifestDagEntry{
				rootCid:    cv1,
				RecordType: DagAggregateEntry,
				DagCidV1:   dagName,
			}
			if cv0 != cid.Undef {
				dags[dagName].DagCidV0 = cv0.String()
			}
			if d.UniqueBlockCumulativeSize != 0 {
				v := d.UniqueBlockCumulativeSize
				dags[dagName].DagSize = &v
			}
			if d.UniqueBlockCount != 0 {
				v := d.UniqueBlockCount
				dags[dagName].NodeCount = &v
			}
		}
	}

	if len(dags) == 0 {
		return cid.Undef, nil, xerrors.New("no valid entries to aggregate")
	}

	// unixfs sorts internally, so we need to pre-sort to match up insertion indices
	aggregateManifest = make([]*ManifestDagEntry, 0, len(dags))
	for _, d := range dags {
		aggregateManifest = append(aggregateManifest, d)
	}
	sort.Slice(aggregateManifest, func(i, j int) bool {
		return strings.Compare(aggregateManifest[i].DagCidV1, aggregateManifest[j].DagCidV1) < 0
	})

	// innermost layer, 4 bytes off end
	innerNodes := make(nodeMap)
	for _, d := range aggregateManifest {

		parentName := d.DagCidV1[:3] + `...` + d.DagCidV1[len(d.DagCidV1)-4:]
		if _, exists := innerNodes[parentName]; !exists {
			innerNodes[parentName] = emptyDir()
		}

		var dagSize uint64
		if d.DagSize != nil {
			dagSize = *d.DagSize
		}

		if err := innerNodes[parentName].AddRawLink(d.DagCidV1, &ipldformat.Link{
			Size: dagSize,
			Cid:  d.rootCid,
		}); err != nil {
			return cid.Undef, nil, err
		}
		d.PathIndexes[2] = len(innerNodes[parentName].Links()) - 1
	}

	newBlocks := make([]ipldformat.Node, 0, len(innerNodes)*3/2)

	// secondary layer, 2 bytes off end ( drop the 2 second-to-last )
	outerNodes := make(nodeMap)
	for _, nodeName := range sortedNodeNames(innerNodes) {
		nd := innerNodes[nodeName]

		newBlocks = append(newBlocks, nd)
		parentName := nodeName[0:len(nodeName)-4] + nodeName[len(nodeName)-2:]
		if _, exists := outerNodes[parentName]; !exists {
			outerNodes[parentName] = emptyDir()
		}

		if err := outerNodes[parentName].AddNodeLink(nodeName, nd); err != nil {
			return cid.Undef, nil, err
		}

		for _, innerDaglink := range nd.Links() {
			d := dags[innerDaglink.Name]
			d.PathPrefixes[1] = nodeName
			d.PathIndexes[1] = len(outerNodes[parentName].Links()) - 1
		}
	}

	// root
	root := emptyDir()
	for topIdx, nodeName := range sortedNodeNames(outerNodes) {

		nd := outerNodes[nodeName]
		newBlocks = append(newBlocks, nd)
		if err := root.AddNodeLink(nodeName, nd); err != nil {
			return cid.Undef, nil, err
		}

		for _, outerDagLink := range nd.Links() {
			for _, innerDaglink := range innerNodes[outerDagLink.Name].Links() {
				d := dags[innerDaglink.Name]
				d.PathPrefixes[0] = nodeName
				d.PathIndexes[0] = topIdx + 1 // +1 because idx#0 is left for the manifest
			}
		}
	}

	// now that we have all the paths correctly, assemble the manifest and add it to the root
	prdr, pwrr := io.Pipe()

	errCh := make(chan error, 1)
	go func() {

		defer func() {
			err := pwrr.Close()
			if err != nil {
				errCh <- err
			}
		}()

		j := json.NewEncoder(pwrr)

		err := j.Encode(CurrentManifestPreamble)
		if err != nil {
			errCh <- err
			return
		}

		err = j.Encode(ManifestSummary{
			RecordType:      DagAggregateSummary,
			EntriesSortedBy: "DagCidV1",
			Description:     "Aggregate of non-related DAGs, produced by github.com/filecoin-project/go-dagaggregator-unixfs",
			EntryCount:      len(aggregateManifest),
		})
		if err != nil {
			errCh <- err
			return
		}

		for _, d := range aggregateManifest {
			err = j.Encode(d)
			if err != nil {
				errCh <- err
				return
			}
		}
	}()

	leaves, err := (&importhelper.DagBuilderParams{
		Dagserv:   ds,
		RawLeaves: true,
		Maxlinks:  8192 / 47, // importhelper.DefaultLinksPerBlock
		CidBuilder: cid.V1Builder{
			Codec:    uint64(multicodec.DagPb),
			MhType:   uint64(multicodec.Sha2_256),
			MhLength: 32,
		},
	}).New(chunker.NewSizeSplitter(prdr, 256<<10))
	if err != nil {
		return cid.Undef, nil, err
	}

	manifest, err := balanced.Layout(leaves)
	if err != nil {
		return cid.Undef, nil, err
	}
	if len(errCh) > 0 {
		return cid.Undef, nil, <-errCh
	}

	if err = root.AddNodeLink(AggregateManifestFilename, manifest); err != nil {
		return cid.Undef, nil, err
	}

	// we are done now, add everything
	newBlocks = append(newBlocks, root)
	err = ds.AddMany(ctx, newBlocks)
	if err != nil {
		return cid.Undef, nil, err
	}

	return root.Cid(), aggregateManifest, nil
}

func sortedNodeNames(nodeMap nodeMap) []string {
	sortedNodeNames := make([]string, 0, len(nodeMap))
	for k := range nodeMap {
		sortedNodeNames = append(sortedNodeNames, k)
	}
	sort.Slice(sortedNodeNames, func(i, j int) bool {
		return sortedNodeNames[i] < sortedNodeNames[j]
	})
	return sortedNodeNames
}

func emptyDir() *merkledag.ProtoNode {
	nd := unixfs.EmptyDirNode()
	nd.SetCidBuilder(cid.V1Builder{
		Codec:    uint64(multicodec.DagPb),
		MhType:   uint64(multicodec.Sha2_256),
		MhLength: 32,
	})
	return nd
}
