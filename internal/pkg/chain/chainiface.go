package chain

import quorumpb "github.com/rumsystem/quorum/internal/pkg/pb"

type ChainMolassesIface interface {
	GetChainCtx() *Chain
	GetUserTrxMgr() *TrxMgr
	GetProducerTrxMgr() *TrxMgr
	UpdChainInfo(height int64, blockId string) error
	UpdProducerList()
	CreateConsensus()
	IsSyncerReady() bool
	SyncBackward(block *quorumpb.Block) error
}
