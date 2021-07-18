package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"

	"github.com/filecoin-project/go-dagaggregator-unixfs"
	"github.com/filecoin-project/go-dagaggregator-unixfs/lib/rambs"
	"github.com/ipfs/go-blockservice"
	"github.com/ipfs/go-cid"
	ipfsapi "github.com/ipfs/go-ipfs-api"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
	exchangeoffline "github.com/ipfs/go-ipfs-exchange-offline"
	ipfsfiles "github.com/ipfs/go-ipfs-files"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipfs/go-merkledag"
	"github.com/mattn/go-isatty"
	"github.com/multiformats/go-multihash"
	"github.com/pborman/getopt/v2"
	"github.com/pborman/options"
	"golang.org/x/sys/unix"
	"golang.org/x/xerrors"
)

var (
	log = logging.Logger("fil-daggregator")
)

func init() {
	logging.SetLogLevel("*", "INFO") // nolint:errcheck
}

type opts struct {
	IpfsAPI            string `getopt:"--ipfs-api             A read/write IPFS API URL"`
	IpfsAPIMaxWorkers  uint   `getopt:"--ipfs-api-max-workers Max amount of parallel API requests"`
	IpfsAPITimeoutSecs uint   `getopt:"--ipfs-api-timeout     Max amount of seconds for a single API operation"`
	ShowProgress       bool   `getopt:"--show-progress        Print progress on STDERR, default when a TTY"`
	AggregateVersion   uint   `getopt:"--aggregate-version    The version of aggregate to produce"`
	SkipDagStat        bool   `getopt:"--skip-dag-stat        Do not query he API for the input dag stats"`
	Help               bool   `getopt:"-h --help              Display help"`
}

func main() {

	opts := &opts{
		ShowProgress:       isatty.IsTerminal(os.Stderr.Fd()),
		IpfsAPIMaxWorkers:  8,
		IpfsAPITimeoutSecs: 300,
		AggregateVersion:   1,
		IpfsAPI:            "http://127.0.0.1:5001",
	}

	o := getopt.New()
	if err := options.RegisterSet("", opts, o); err != nil {
		log.Fatalf("option set registration failed: %s", err)
	}
	o.SetParameters("{ list of CIDs to aggregate | if blank read STDIN }")
	if err := o.Getopt(os.Args, nil); err != nil {
		log.Fatalf("option parsing failed: %s", err)
	}
	if opts.Help {
		o.PrintUsage(os.Stderr)
		os.Exit(0)
	}
	if opts.AggregateVersion != 1 {
		log.Fatalf("requested aggregate version %d not supported by this program", opts.AggregateVersion)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, unix.SIGINT, unix.SIGTERM, unix.SIGHUP)
		<-sigs
		cancel()
	}()

	cidStrs := o.Args()
	if len(cidStrs) == 0 {
		s := bufio.NewScanner(os.Stdin)
		s.Split(bufio.ScanWords)
		for s.Scan() {
			cidStrs = append(cidStrs, s.Text())
		}
	}

	cset := cid.NewSet()
	toAgg := make([]dagaggregator.AggregateDagEntry, 0, len(cidStrs))
	for _, cs := range cidStrs {
		c, err := cid.Parse(cs)
		if err != nil {
			log.Fatalf("unable to parse '%s': %s", cs, err)
		}
		if cset.Visit(c) {
			toAgg = append(toAgg, dagaggregator.AggregateDagEntry{RootCid: c})
		}
	}

	if !opts.SkipDagStat {
		if err := statSources(ctx, opts, toAgg); err != nil {
			log.Fatal(err)
		}
	}

	ramBs := new(rambs.RamBs)
	ramDs := merkledag.NewDAGService(blockservice.New(ramBs, exchangeoffline.Exchange(ramBs)))
	root, entries, err := dagaggregator.Aggregate(ctx, ramDs, toAgg)
	if err != nil {
		log.Fatalf("aggregation failed: %s", err)
	}

	if err := writeoutBlocks(ctx, opts, ramBs); err != nil {
		log.Fatalf("writing newly created dag to IPFS API failed: %s", err)
	}

	log.Infow("aggregation finished", "aggregateRoot", root, "totalEntries", len(entries))
}

func statSources(externalCtx context.Context, opts *opts, toAgg []dagaggregator.AggregateDagEntry) error {

	type dagStat struct {
		Size      uint64
		NumBlocks uint64
	}

	ctx, shutdownWorkers := context.WithCancel(externalCtx)
	defer shutdownWorkers()

	var lastPct uint64
	dagsDone := new(uint64)
	dagsTotal := uint64(len(toAgg))

	maxWorkers := opts.IpfsAPIMaxWorkers
	workCh := make(chan int)                // channel of toAgg indexes to work on
	errCh := make(chan error, 1+maxWorkers) // our own error plus one from each worker

	// work dispenser, watchdog for progress and to bail early if an error appears
	go func() {
		defer close(workCh)

		var progressTick <-chan time.Time
		if opts.ShowProgress {
			fmt.Fprint(os.Stderr, "0% of dags analyzed\r")
			t := time.NewTicker(250 * time.Millisecond)
			progressTick = t.C
			defer t.Stop()
		}

		toAggIdx := 0
		for toAggIdx < len(toAgg) {
			select {
			case workCh <- toAggIdx:
				toAggIdx++
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case err := <-errCh:
				errCh <- err // put error back where we got it from
				if err != nil {
					shutdownWorkers()
				}
				return
			case <-progressTick:
				curPct := 100 * atomic.LoadUint64(dagsDone) / dagsTotal
				if curPct != lastPct {
					lastPct = curPct
					fmt.Fprintf(os.Stderr, "%d%% of dags analyzed\r", lastPct)
				}
			}
		}
	}()

	var wg sync.WaitGroup

	for maxWorkers > 0 {
		maxWorkers--
		wg.Add(1)
		go func() {
			defer wg.Done()
			api := ipfsapi.NewShell(opts.IpfsAPI)
			api.SetTimeout(time.Second * time.Duration(opts.IpfsAPITimeoutSecs))

			for {
				toAggIdx, chanOpen := <-workCh
				if !chanOpen {
					return
				}

				ds := new(dagStat)
				err := api.Request("dag/stat").Arguments(toAgg[toAggIdx].RootCid.String()).Option("progress", "false").Exec(ctx, ds)
				if err != nil {
					errCh <- err
					return
				}

				toAgg[toAggIdx].UniqueBlockCount = ds.NumBlocks
				toAgg[toAggIdx].UniqueBlockCumulativeSize = ds.Size

				if opts.ShowProgress {
					atomic.AddUint64(dagsDone, 1)
				}

			}
		}()
	}

	wg.Wait()
	shutdownWorkers()

	errCh <- externalCtx.Err()
	return <-errCh
}

// pulls cids from an AllKeysChan and sends them concurrently via multiple workers to an API
func writeoutBlocks(externalCtx context.Context, opts *opts, bs blockstore.Blockstore) error {

	ctx, shutdownWorkers := context.WithCancel(externalCtx)
	defer shutdownWorkers()

	akc, err := bs.AllKeysChan(ctx)
	if err != nil {
		return err
	}

	var lastPct uint64
	blocksDone := new(uint64)

	// this works because of how AllKeysChan behaves on rambs
	blocksTotal := uint64(len(akc))

	maxWorkers := opts.IpfsAPIMaxWorkers
	errCh := make(chan error, 1+maxWorkers) // our own error plus one from each worker

	// watchdog for progress and to bail early if an error appears
	go func() {
		var progressTick <-chan time.Time
		if opts.ShowProgress {
			fmt.Fprint(os.Stderr, "0% of blocks written\r")
			t := time.NewTicker(250 * time.Millisecond)
			progressTick = t.C
			defer t.Stop()
		}
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case err := <-errCh:
				errCh <- err // put error back where we got it from
				if err != nil {
					shutdownWorkers()
				}
				return
			case <-progressTick:
				curPct := 100 * atomic.LoadUint64(blocksDone) / blocksTotal
				if curPct != lastPct {
					lastPct = curPct
					fmt.Fprintf(os.Stderr, "%d%% of blocks written\r", lastPct)
				}
			}
		}
	}()

	var wg sync.WaitGroup

	for maxWorkers > 0 {
		maxWorkers--
		wg.Add(1)
		go func() {
			defer wg.Done()
			api := ipfsapi.NewShell(opts.IpfsAPI)
			api.SetTimeout(time.Second * time.Duration(opts.IpfsAPITimeoutSecs))

			for {
				select {

				case <-ctx.Done():
					return

				case c, chanOpen := <-akc:

					if !chanOpen {
						return
					}

					blk, err := bs.Get(c)
					if err != nil {
						errCh <- err
						return
					}

					// copied entirety of ipfsapi.BlockPut() to be able to pass in our own ctx 🤮
					res := new(struct{ Key string })
					err = api.Request("block/put").
						Option("format", cid.CodecToStr[c.Prefix().Codec]).
						Option("mhtype", multihash.Codes[c.Prefix().MhType]).
						Option("mhlen", c.Prefix().MhLength).
						Body(
							ipfsfiles.NewMultiFileReader(
								ipfsfiles.NewSliceDirectory([]ipfsfiles.DirEntry{
									ipfsfiles.FileEntry(
										"",
										ipfsfiles.NewBytesFile(blk.RawData()),
									),
								}),
								true,
							),
						).
						Exec(ctx, res)
					// end of 🤮

					if err != nil {
						errCh <- err
						return
					}

					if res.Key != c.String() {
						errCh <- xerrors.Errorf("unexpected cid mismatch after /block/put: expected %s but got %s", c, res.Key)
						return
					}

					if opts.ShowProgress {
						atomic.AddUint64(blocksDone, 1)
					}
				}
			}
		}()
	}

	wg.Wait()
	shutdownWorkers()

	// feeder drain if anything remains
	for len(akc) > 0 {
		<-akc
	}

	errCh <- externalCtx.Err()
	return <-errCh
}
