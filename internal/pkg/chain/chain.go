package chain

import (
	"encoding/hex"
	"errors"
	"sync"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/rumsystem/quorum/internal/pkg/nodectx"
	quorumpb "github.com/rumsystem/quorum/internal/pkg/pb"
	pubsubconn "github.com/rumsystem/quorum/internal/pkg/pubsubconn"
	"google.golang.org/protobuf/proto"

	localcrypto "github.com/rumsystem/quorum/internal/pkg/crypto"
)

var chain_log = logging.Logger("chain")

type GroupProducer struct {
	ProducerPubkey   string
	ProducerPriority int8
}

type Chain struct {
	nodename          string
	group             *Group
	userChannelId     string
	producerChannelId string
	syncChannelId     string
	trxMgrs           map[string]*TrxMgr
	ProducerPool      map[string]*quorumpb.ProducerItem

	Syncer    *Syncer
	Consensus Consensus
	statusmu  sync.RWMutex

	producerChannTimer *time.Timer
	groupId            string
}

func (chain *Chain) CustomInit(nodename string, group *Group, producerPubsubconn pubsubconn.PubSubConn, userPubsubconn pubsubconn.PubSubConn) {

	/*
		chain.group = group
		chain.trxMgrs = make(map[string]*TrxMgr)
		chain.nodename = nodename

		chain.producerChannelId = PRODUCER_CHANNEL_PREFIX + group.Item.GroupId
		producerTrxMgr := &TrxMgr{}
		producerTrxMgr.Init(chain.group.Item, producerPubsubconn)
		producerTrxMgr.SetNodeName(nodename)
		chain.trxMgrs[chain.producerChannelId] = producerTrxMgr

		chain.Consensus = NewMolasses(&MolassesProducer{}, &MolassesUser{})
		chain.Consensus.Producer().Init(chain.group.Item, chain.group.ChainCtx.nodename, chain)
		chain.Consensus.User().Init(group.Item, group.ChainCtx.nodename, chain)

		chain.userChannelId = USER_CHANNEL_PREFIX + group.Item.GroupId
		userTrxMgr := &TrxMgr{}
		userTrxMgr.Init(chain.group.Item, userPubsubconn)
		userTrxMgr.SetNodeName(nodename)
		chain.trxMgrs[chain.userChannelId] = userTrxMgr

		chain.syncChannelId = SYNC_CHANNEL_PREFIX + group.Item.GroupId + "_" + group.Item.UserSignPubkey
		syncTrxMgr := &TrxMgr{}
		syncTrxMgr.Init(chain.group.Item, userPubsubconn)
		syncTrxMgr.SetNodeName(nodename)
		chain.trxMgrs[chain.userChannelId] = userTrxMgr

		chain.Syncer = &Syncer{nodeName: nodename}
		chain.Syncer.Init(chain.group, producerTrxMgr, userTrxMgr, syncTrxMgr)

		chain.groupId = group.Item.GroupId
	*/
}

func (chain *Chain) Init(group *Group) error {
	chain_log.Debugf("<%s> Init called", group.Item.GroupId)
	chain.group = group
	chain.trxMgrs = make(map[string]*TrxMgr)
	chain.nodename = nodectx.GetNodeCtx().Name
	chain.groupId = group.Item.GroupId

	chain.producerChannelId = PRODUCER_CHANNEL_PREFIX + chain.groupId
	chain.userChannelId = USER_CHANNEL_PREFIX + chain.groupId
	chain.syncChannelId = SYNC_CHANNEL_PREFIX + chain.groupId + "_" + chain.group.Item.UserSignPubkey

	chain_log.Infof("<%s> chainctx initialed", chain.groupId)
	return nil
}

func (chain *Chain) LeaveChannel() error {
	chain_log.Debugf("<%s> LeaveChannel called", chain.groupId)
	if userTrxMgr, ok := chain.trxMgrs[chain.userChannelId]; ok {
		userTrxMgr.LeaveChannel(chain.userChannelId)
		delete(chain.trxMgrs, chain.userChannelId)

	}
	if producerTrxMgr, ok := chain.trxMgrs[chain.producerChannelId]; ok {
		producerTrxMgr.LeaveChannel(chain.producerChannelId)
		delete(chain.trxMgrs, chain.producerChannelId)
	}
	if syncTrxMgr, ok := chain.trxMgrs[chain.syncChannelId]; ok {
		syncTrxMgr.LeaveChannel(chain.syncChannelId)
		delete(chain.trxMgrs, chain.syncChannelId)
	}

	return nil
}

func (chain *Chain) StartInitialSync(block *quorumpb.Block) error {
	chain_log.Debugf("<%s> StartInitialSync called", chain.groupId)
	if chain.Syncer != nil {
		return chain.Syncer.SyncForward(block)
	}
	return nil
}

func (chain *Chain) StopSync() error {
	chain_log.Debugf("<%s> StopSync called", chain.groupId)
	if chain.Syncer != nil {
		return chain.Syncer.StopSync()
	}
	return nil
}

func (chain *Chain) GetChainCtx() *Chain {
	return chain
}

func (chain *Chain) GetProducerTrxMgr() *TrxMgr {
	chain_log.Debugf("<%s> GetProducerTrxMgr called", chain.groupId)

	if _, ok := chain.ProducerPool[chain.group.Item.UserSignPubkey]; ok {
		return chain.trxMgrs[chain.producerChannelId]
	}

	var producerTrxMgr *TrxMgr

	if _, ok := chain.trxMgrs[chain.producerChannelId]; ok {
		//reset timer
		producerTrxMgr = chain.trxMgrs[chain.producerChannelId]
		chain_log.Debugf("<%s> reset connection timer for producertrxMgr <%s>", chain.groupId, chain.producerChannelId)
		chain.producerChannTimer.Stop()
		chain.producerChannTimer.Reset(CLOSE_CONN_TIMER * time.Second)
	} else {
		chain.createProducerTrxMgr()
		producerTrxMgr = chain.trxMgrs[chain.producerChannelId]
		chain_log.Debugf("<%s> create close_conn timer for producer channel <%s>", chain.groupId, chain.producerChannelId)
		chain.producerChannTimer = time.AfterFunc(CLOSE_CONN_TIMER*time.Second, func() {
			if producerTrxMgr, ok := chain.trxMgrs[chain.producerChannelId]; ok {
				chain_log.Debugf("<%s> time up, close sync channel <%s>", chain.groupId, chain.producerChannelId)
				producerTrxMgr.LeaveChannel(chain.producerChannelId)
				delete(chain.trxMgrs, chain.producerChannelId)
			}
		})
	}

	return producerTrxMgr
}

func (chain *Chain) GetUserTrxMgr() *TrxMgr {
	chain_log.Debugf("<%s> GetUserTrxMgr called", chain.groupId)
	return chain.trxMgrs[chain.userChannelId]
}

func (chain *Chain) UpdChainInfo(height int64, blockId string) error {
	chain_log.Debugf("<%s> UpdChainInfo called", chain.groupId)
	chain.group.Item.HighestHeight = height
	chain.group.Item.HighestBlockId = blockId
	chain.group.Item.LastUpdate = time.Now().UnixNano()
	chain_log.Infof("<%s> Chain Info updated %d, %v", chain.group.Item.GroupId, height, blockId)
	return nodectx.GetDbMgr().UpdGroup(chain.group.Item)
}

func (chain *Chain) HandleTrx(trx *quorumpb.Trx) error {
	//chain_log.Debugf("<%s> HandleTrx called", chain.groupId)
	if trx.Version != nodectx.GetNodeCtx().Version {
		chain_log.Errorf("HandleTrx called, Trx Version mismatch %s", trx.TrxId)
		return errors.New("Trx Version mismatch")
	}
	switch trx.Type {
	case quorumpb.TrxType_AUTH:
		chain.producerAddTrx(trx)
	case quorumpb.TrxType_POST:
		chain.producerAddTrx(trx)
	case quorumpb.TrxType_ANNOUNCE:
		chain.producerAddTrx(trx)
	case quorumpb.TrxType_PRODUCER:
		chain.producerAddTrx(trx)
	case quorumpb.TrxType_SCHEMA:
		chain.producerAddTrx(trx)
	case quorumpb.TrxType_REQ_BLOCK_FORWARD:
		if trx.SenderPubkey == chain.group.Item.UserSignPubkey {
			return nil
		}
		chain.handleReqBlockForward(trx)
	case quorumpb.TrxType_REQ_BLOCK_BACKWARD:
		if trx.SenderPubkey == chain.group.Item.UserSignPubkey {
			return nil
		}
		chain.handleReqBlockBackward(trx)
	case quorumpb.TrxType_REQ_BLOCK_RESP:
		if trx.SenderPubkey == chain.group.Item.UserSignPubkey {
			return nil
		}
		chain.handleReqBlockResp(trx)
	case quorumpb.TrxType_BLOCK_PRODUCED:
		chain.handleBlockProduced(trx)
		return nil
	default:
		chain_log.Warningf("<%s> unsupported msg type", chain.group.Item.GroupId)
		err := errors.New("unsupported msg type")
		return err
	}
	return nil
}

func (chain *Chain) HandleBlock(block *quorumpb.Block) error {
	chain_log.Debugf("<%s> HandleBlock called", chain.groupId)

	var shouldAccept bool

	if chain.Consensus.Producer() != nil {
		//if I am a producer, no need to addBlock since block just produced is already saved
		shouldAccept = false
	} else if _, ok := chain.ProducerPool[block.ProducerPubKey]; ok {
		//from registed producer
		shouldAccept = true
	} else {
		//from someone else
		shouldAccept = false
		chain_log.Warningf("<%s> received block <%s> from unregisted producer <%s>, reject it", chain.group.Item.GroupId, block.BlockId, block.ProducerPubKey)
	}

	if shouldAccept {
		err := chain.Consensus.User().AddBlock(block)
		if err != nil {
			chain_log.Debugf("<%s> user add block error <%s>", chain.groupId, err.Error())
			if err.Error() == "PARENT_NOT_EXIST" {
				chain_log.Infof("<%s>, parent not exist, sync backward from block <%s>", chain.groupId, block.BlockId)
				chain.Syncer.SyncBackward(block)
			}
		}
	}

	return nil
}

func (chain *Chain) producerAddTrx(trx *quorumpb.Trx) error {
	if chain.Consensus.Producer() == nil {
		return nil
	}
	chain_log.Debugf("<%s> producerAddTrx called", chain.groupId)
	chain.Consensus.Producer().AddTrx(trx)
	return nil
}

func (chain *Chain) handleReqBlockForward(trx *quorumpb.Trx) error {
	if chain.Consensus.Producer() == nil {
		return nil
	}
	chain_log.Debugf("<%s> producer handleReqBlockForward called", chain.groupId)
	return chain.Consensus.Producer().GetBlockForward(trx)
}

func (chain *Chain) handleReqBlockBackward(trx *quorumpb.Trx) error {
	if chain.Consensus.Producer() == nil {
		return nil
	}
	chain_log.Debugf("<%s> producer handleReqBlockBackward called", chain.groupId)
	return chain.Consensus.Producer().GetBlockBackward(trx)
}

func (chain *Chain) handleReqBlockResp(trx *quorumpb.Trx) error {
	ciperKey, err := hex.DecodeString(chain.group.Item.CipherKey)
	if err != nil {
		return err
	}

	decryptData, err := localcrypto.AesDecode(trx.Data, ciperKey)
	if err != nil {
		return err
	}

	var reqBlockResp quorumpb.ReqBlockResp
	if err := proto.Unmarshal(decryptData, &reqBlockResp); err != nil {
		return err
	}

	//if not asked by myself, ignore it
	if reqBlockResp.RequesterPubkey != chain.group.Item.UserSignPubkey {
		return nil
	}

	chain_log.Debugf("<%s> handleReqBlockResp called", chain.groupId)

	var newBlock quorumpb.Block
	if err := proto.Unmarshal(reqBlockResp.Block, &newBlock); err != nil {
		return err
	}

	var shouldAccept bool

	chain_log.Debugf("<%s> REQ_BLOCK_RESP, block_id <%s>, block_producer <%s>", chain.groupId, newBlock.BlockId, newBlock.ProducerPubKey)

	if _, ok := chain.ProducerPool[newBlock.ProducerPubKey]; ok {
		shouldAccept = true
	} else {
		shouldAccept = false
	}

	if !shouldAccept {
		chain_log.Warnf(" <%s> Block producer <%s> not registed, reject", chain.groupId, newBlock.ProducerPubKey)
		return nil
	}

	return chain.Syncer.AddBlockSynced(&reqBlockResp, &newBlock)
}

func (chain *Chain) handleBlockProduced(trx *quorumpb.Trx) error {
	if chain.Consensus.Producer() == nil {
		return nil
	}
	chain_log.Debugf("<%s> handleBlockProduced called", chain.groupId)
	return chain.Consensus.Producer().AddProducedBlock(trx)
}

func (chain *Chain) UpdProducerList() {
	chain_log.Debugf("<%s> UpdProducerList called", chain.groupId)
	//create and load group producer pool
	chain.ProducerPool = make(map[string]*quorumpb.ProducerItem)
	producers, _ := nodectx.GetDbMgr().GetProducers(chain.group.Item.GroupId, chain.nodename)
	for _, item := range producers {
		chain.ProducerPool[item.ProducerPubkey] = item
		ownerPrefix := "(producer)"
		if item.ProducerPubkey == chain.group.Item.OwnerPubKey {
			ownerPrefix = "(owner)"
		}
		chain_log.Infof("<%s> Load producer <%s%s>", chain.groupId, item.ProducerPubkey, ownerPrefix)
	}

	//update announced producer result
	announcedProducers, _ := nodectx.GetDbMgr().GetAnnounceProducersByGroup(chain.group.Item.GroupId, chain.nodename)
	for _, item := range announcedProducers {
		if _, ok := chain.ProducerPool[item.SignPubkey]; ok {
			err := nodectx.GetDbMgr().UpdateProducerAnnounceResult(chain.group.Item.GroupId, item.SignPubkey, ok, chain.nodename)
			if err != nil {
				chain_log.Warningf("<%s> UpdAnnounceResult failed with error <%s>", chain.groupId, err.Error())
			}
		}
	}
}

func (chain *Chain) CreateConsensus() {
	chain_log.Debugf("<%s> CreateConsensus called", chain.groupId)
	if _, ok := chain.ProducerPool[chain.group.Item.UserSignPubkey]; ok {
		//producer, create group producer
		chain_log.Infof("<%s> Create and initial molasses producer", chain.groupId)
		chain.Consensus = NewMolasses(&MolassesProducer{}, &MolassesUser{})
		chain.Consensus.Producer().Init(chain.group.Item, chain.group.ChainCtx.nodename, chain)
		chain.createProducerTrxMgr()
	} else {
		chain_log.Infof("<%s> Create and initial molasses user", chain.groupId)
		chain.Consensus = NewMolasses(nil, &MolassesUser{})
	}

	chain.Consensus.User().Init(chain.group.Item, chain.group.ChainCtx.nodename, chain)

	chain.createUserTrxMgr()
	chain.createSyncTrxMgr()

	chain_log.Infof("<%s> Create and init group syncer", chain.groupId)
	chain.Syncer = &Syncer{nodeName: chain.nodename}
	chain.Syncer.Init(chain.group, chain)
}

func (chain *Chain) createUserTrxMgr() {
	chain_log.Infof("<%s> Create and join group user channel", chain.groupId)

	if _, ok := chain.trxMgrs[chain.userChannelId]; ok {
		chain_log.Infof("<%s> reuse user channel", chain.groupId)
		return
	}

	userPsconn := pubsubconn.InitP2pPubSubConn(nodectx.GetNodeCtx().Ctx, nodectx.GetNodeCtx().Node.Pubsub, nodectx.GetNodeCtx().Name)
	userPsconn.JoinChannel(chain.userChannelId, chain)

	chain_log.Infof("<%s> Create and init group userTrxMgr", chain.groupId)
	var userTrxMgr *TrxMgr
	userTrxMgr = &TrxMgr{}
	userTrxMgr.Init(chain.group.Item, userPsconn)
	chain.trxMgrs[chain.userChannelId] = userTrxMgr
}

func (chain *Chain) createSyncTrxMgr() {
	chain_log.Infof("<%s> Create and join group syncer channel", chain.groupId)

	if _, ok := chain.trxMgrs[chain.syncChannelId]; ok {
		chain_log.Infof("<%s> reuse syncer channel", chain.groupId)
		return
	}

	syncPsconn := pubsubconn.InitP2pPubSubConn(nodectx.GetNodeCtx().Ctx, nodectx.GetNodeCtx().Node.Pubsub, nodectx.GetNodeCtx().Name)
	syncPsconn.JoinChannel(chain.syncChannelId, chain)

	chain_log.Infof("<%s> Create and init group syncTrxMgr", chain.groupId)
	var syncTrxMgr *TrxMgr
	syncTrxMgr = &TrxMgr{}
	syncTrxMgr.Init(chain.group.Item, syncPsconn)
	chain.trxMgrs[chain.syncChannelId] = syncTrxMgr

}
func (chain *Chain) createProducerTrxMgr() {
	chain_log.Infof("<%s> Create and join group producer channel", chain.groupId)
	if _, ok := chain.trxMgrs[chain.producerChannelId]; ok {
		chain_log.Infof("<%s> reuse producer channel", chain.groupId)
		return
	}

	producerPsconn := pubsubconn.InitP2pPubSubConn(nodectx.GetNodeCtx().Ctx, nodectx.GetNodeCtx().Node.Pubsub, nodectx.GetNodeCtx().Name)
	producerPsconn.JoinChannel(chain.producerChannelId, chain)

	chain_log.Infof("<%s> Create and init group producerTrxMgr", chain.groupId)
	var producerTrxMgr *TrxMgr
	producerTrxMgr = &TrxMgr{}
	producerTrxMgr.Init(chain.group.Item, producerPsconn)
	chain.trxMgrs[chain.producerChannelId] = producerTrxMgr
}

func (chain *Chain) IsSyncerReady() bool {
	chain_log.Debugf("<%s> IsSyncerReady called", chain.groupId)
	if chain.Syncer.Status == SYNCING_BACKWARD ||
		chain.Syncer.Status == SYNCING_FORWARD ||
		chain.Syncer.Status == SYNC_FAILED {
		chain_log.Debugf("<%s> syncer is busy, status: <%d>", chain.groupId, chain.Syncer.Status)
		return true
	}
	chain_log.Debugf("<%s> syncer is IDLE", chain.groupId)
	return false
}

func (chain *Chain) SyncBackward(block *quorumpb.Block) error {
	return chain.Syncer.SyncBackward(block)
}
