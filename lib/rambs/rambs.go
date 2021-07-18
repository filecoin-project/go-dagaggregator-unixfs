/*
Package rambs is an implementation of a blockstore, keeping records indexed by full
CIDs instead of just multihashes. It is the moral equivalent of

    blockstore.NewBlockstore(dssync.MutexWrap(ds.NewMapDatastore()))
*/
package rambs

import (
	"context"
	"io"
	"sort"
	"sync"

	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	blockstore "github.com/ipfs/go-ipfs-blockstore"
)

type RamBs struct {
	store map[cid.Cid][]byte
	mu    sync.RWMutex
}

// make sure we are Blockstore compliant
var _ blockstore.Blockstore = &RamBs{}
var _ blockstore.Viewer = &RamBs{}
var _ io.Closer = &RamBs{}

var ErrNotFound = blockstore.ErrNotFound

func (rbs *RamBs) Has(c cid.Cid) (bool, error) {
	rbs.mu.RLock()
	defer rbs.mu.RUnlock()
	if rbs.store == nil {
		return false, nil
	}

	_, canHaz := rbs.store[cidv1(c)]
	return canHaz, nil
}
func (rbs *RamBs) GetSize(c cid.Cid) (int, error) {
	rbs.mu.RLock()
	defer rbs.mu.RUnlock()
	if rbs.store == nil {
		return -1, ErrNotFound
	}

	b, canHaz := rbs.store[cidv1(c)]
	if !canHaz {
		return -1, ErrNotFound
	}

	return len(b), nil
}
func (rbs *RamBs) Get(c cid.Cid) (blocks.Block, error) {
	rbs.mu.RLock()
	defer rbs.mu.RUnlock()
	if rbs.store == nil {
		return nil, ErrNotFound
	}

	b, canHaz := rbs.store[cidv1(c)]
	if !canHaz {
		return nil, ErrNotFound
	}

	return blocks.NewBlockWithCid(b, c)
}
func (rbs *RamBs) View(c cid.Cid, cb func([]byte) error) error {
	rbs.mu.RLock()
	defer rbs.mu.RUnlock()
	if rbs.store == nil {
		return ErrNotFound
	}

	b, canHaz := rbs.store[cidv1(c)]
	if !canHaz {
		return ErrNotFound
	}

	return cb(b)
}
func (rbs *RamBs) AllKeysChan(ctx context.Context) (<-chan cid.Cid, error) {

	rbs.mu.RLock()

	if rbs.store == nil {
		rbs.mu.RUnlock()
		ch := make(chan cid.Cid)
		close(ch)
		return ch, nil
	}

	allCids := make([]cid.Cid, 0, len(rbs.store))
	for k := range rbs.store {
		allCids = append(allCids, k)
	}
	rbs.mu.RUnlock()

	sort.Slice(allCids, func(i, j int) bool { return allCids[i].KeyString() < allCids[j].KeyString() })
	ch := make(chan cid.Cid, len(allCids))
	for _, c := range allCids {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func (rbs *RamBs) DeleteBlock(c cid.Cid) error {
	rbs.mu.Lock()
	defer rbs.mu.Unlock()
	if rbs.store == nil {
		return nil
	}
	delete(rbs.store, c)
	return nil
}

func (rbs *RamBs) PutMany(blks []blocks.Block) error {
	rbs.mu.Lock()
	defer rbs.mu.Unlock()
	if rbs.store == nil {
		rbs.store = make(map[cid.Cid][]byte, 256<<10)
	}

	for _, b := range blks {
		rbs.store[cidv1(b.Cid())] = b.RawData()
	}
	return nil
}
func (rbs *RamBs) Put(b blocks.Block) error {
	return rbs.PutMany([]blocks.Block{b})
}
func (rbs *RamBs) HashOnRead(bool) {}
func (rbs *RamBs) Close() error    { return nil }

func cidv1(c cid.Cid) cid.Cid {
	if c == cid.Undef || c.Version() == 1 {
		return c
	}
	return cid.NewCidV1(c.Type(), c.Hash())
}
