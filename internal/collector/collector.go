package collector

import (
	"sync"

	"github.com/ethereum/go-ethereum/core/types"
)

var DefaultBlockCollector = NewBlockCollector()

type BlockCollector struct {
	blocks            sync.Map
	latestBlockNumber uint64
}

func NewBlockCollector() *BlockCollector {
	return &BlockCollector{}
}

func (bc *BlockCollector) Store(block *types.Block) {
	blockNumber := block.NumberU64()
	bc.blocks.Store(blockNumber, block)
	if bc.latestBlockNumber < blockNumber {
		bc.latestBlockNumber = blockNumber
	}
}

func (bc *BlockCollector) Load(blockNumber uint64) (*types.Block, bool) {
	v, ok := bc.blocks.Load(blockNumber)
	if !ok {
		return nil, false
	}
	return v.(*types.Block), true
}

func (bc *BlockCollector) GetLatestBlock() *types.Block {
	di, ok := bc.blocks.Load(bc.latestBlockNumber)
	if !ok {
		return nil
	}
	return di.(*types.Block)
}

func (bc *BlockCollector) LatestBlockNumber() uint64 {
	return bc.latestBlockNumber
}

func (bc *BlockCollector) FindMissingBlocks() []uint64 {
	numSet := make(map[uint64]bool)

	var (
		min, max uint64
	)
	bc.blocks.Range(func(key, value any) bool {
		num := key.(uint64)
		if min > num {
			min = num
		}
		if max < num {
			max = num
		}
		numSet[num] = true
		return true
	})

	missingNum := []uint64{}
	for i := min + 1; i < max; i++ {
		if !numSet[i] {
			missingNum = append(missingNum, i)
		}
	}
	return missingNum
}
