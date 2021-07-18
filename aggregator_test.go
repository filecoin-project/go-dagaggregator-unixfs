package dagaggregator

import (
	"context"
	"testing"

	"github.com/filecoin-project/go-dagaggregator-unixfs/lib/rambs"
	"github.com/ipfs/go-blockservice"
	"github.com/ipfs/go-cid"
	exchangeoffline "github.com/ipfs/go-ipfs-exchange-offline"
	"github.com/ipfs/go-merkledag"
)

var ramBs = new(rambs.RamBs) // we want codec-awareness
var ramDs = merkledag.NewDAGService(blockservice.New(ramBs, exchangeoffline.Exchange(ramBs)))

func TestAggregate(t *testing.T) {

	aggRoot, aggManifest, err := Aggregate(context.Background(), ramDs, []AggregateDagEntry{
		{
			RootCid:                   cidFromStr("QmQy6xmJhrcC5QLboAcGFcAE1tC8CrwDVkrHdEYJkLscrQ"),
			UniqueBlockCount:          1,
			UniqueBlockCumulativeSize: 42, // ignored by the gateway renderer, leaf size taken instead
		},

		// IDENTITY: should be skipped entirely
		{RootCid: cidFromStr("bafkqaddgnfwc6nbpon4xg5dfnu")},

		{
			RootCid:                   cidFromStr("bafybeibhbx3y6tnn7q4gpsous6apnobft5jybvroiepdsmvps2lmycjjxu"),
			UniqueBlockCount:          666, // should have no effect - first seen, first taken
			UniqueBlockCumulativeSize: 666, // ^^
		},

		// The following 3 test "bucket collision" working correctly, defaults for node/size should be acceptable
		{RootCid: cidFromStr("bafkreihjji2ny4zwyh7ubc3bmdb5tj455vi5fhsbwf2uvcw6l75z446qea")},
		{RootCid: cidFromStr("bafkreialad2qaplrgjs2x2rs4fycwmwmpmocantoho3doxulmbmlrg6qea")},
		{RootCid: cidFromStr("bafkreiarcpog7fgb3cvs4iznh6jcqtxgyyk5rbsmk4dvxuty5tylof6qea")},
	})
	if err != nil {
		t.Fatal(err)
	}

	exp := cidFromStr("bafybeib62b4ukyzjcj7d2h4mbzjgg7l6qiz3ma4vb4b2bawmcauf5afvua")
	if !aggRoot.Equals(exp) {
		t.Errorf("Unexpected mismatch of aggregate root: expected %s, got %s", exp, aggRoot)
	}

	expectedIndices := [][3]int{
		{1, 0, 0},
		{1, 0, 1},
		{1, 0, 2},
		{2, 0, 0},
	}

	for i, me := range aggManifest {
		if me.PathIndexes != expectedIndices[i] {
			t.Errorf("Unexpected mismatch of aggregate path for entry %d:\nexpected %v\ngot %v\n", i, expectedIndices[i], me.PathIndexes)
		}
	}
}

func cidFromStr(cs string) cid.Cid {
	c, err := cid.Parse(cs)
	if err != nil {
		panic(err)
	}
	return c
}
