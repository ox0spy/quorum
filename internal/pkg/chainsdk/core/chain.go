package chain

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	chaindef "github.com/rumsystem/quorum/internal/pkg/chainsdk/def"
	"github.com/rumsystem/quorum/internal/pkg/conn"
	rumerrors "github.com/rumsystem/quorum/internal/pkg/errors"
	"github.com/rumsystem/quorum/internal/pkg/logging"
	"github.com/rumsystem/quorum/internal/pkg/nodectx"
	"github.com/rumsystem/quorum/internal/pkg/utils"
	"github.com/rumsystem/quorum/pkg/consensus"
	"github.com/rumsystem/quorum/pkg/consensus/def"
	localcrypto "github.com/rumsystem/quorum/pkg/crypto"
	rumchaindata "github.com/rumsystem/quorum/pkg/data"
	quorumpb "github.com/rumsystem/quorum/pkg/pb"
	"google.golang.org/protobuf/proto"
)

var chain_log = logging.Logger("chain")

type GroupProducer struct {
	ProducerPubkey   string
	ProducerPriority int8
}

type Chain struct {
	nodename           string
	groupId            string
	group              *Group
	ProducerPool       map[string]*quorumpb.ProducerItem
	userPool           map[string]*quorumpb.UserItem
	Consensus          def.Consensus
	ProviderPeerIdPool map[string]string
	trxFactory         *rumchaindata.TrxFactory
	syncerrunner       *SyncerRunner
	chaindata          *ChainData
}

func (chain *Chain) NewChain(group *Group) error {
	chain_log.Debugf("<%s> NewChain called", group.Item.GroupId)
	chain.group = group

	chain.nodename = nodectx.GetNodeCtx().Name
	chain.groupId = group.Item.GroupId

	chain.trxFactory = &rumchaindata.TrxFactory{}
	chain.trxFactory.Init(nodectx.GetNodeCtx().Version, group.Item, chain.nodename, chain)

	chain.syncerrunner = NewSyncerRunner(group, chain, chain.nodename)
	chain.chaindata = &ChainData{nodename: chain.nodename, groupId: group.Item.GroupId, groupCipherKey: group.Item.CipherKey, userSignPubkey: group.Item.UserSignPubkey, dbmgr: nodectx.GetDbMgr()}
	return nil
}

func (chain *Chain) GetNodeName() string {
	return chain.nodename
}

func (chain *Chain) SetRumExchangeTestMode() {
	chain_log.Debugf("<%s> SetRumExchangeTestMode called", chain.groupId)
	//chain.syncer.SetRumExchangeTestMode()
}

func (chain *Chain) GetChainSyncIface() chaindef.ChainDataSyncIface {
	chain_log.Debugf("<%s> GetChainSyncIface called", chain.groupId)
	return chain
}

func (chain *Chain) GetTrxFactory() chaindef.TrxFactoryIface {
	chain_log.Debugf("<%s> GetTrxFactory called", chain.groupId)
	return chain.trxFactory
}

func (chain *Chain) GetPubqueueIface() chaindef.PublishQueueIface {
	chain_log.Debugf("<%s> GetPubqueueIface called", chain.groupId)
	return GetPubQueueWatcher()
}

func (chain *Chain) GetCurrentChainEpoch() int64 {
	return chain.group.Item.Epoch
}

func (chain *Chain) GetConsensus() (string, error) {
	return chain.syncerrunner.GetConsensus()
}

func (chain *Chain) UpdChainInfo(Epoch int64) error {
	chain_log.Debugf("<%s> UpdChainInfo called", chain.groupId)
	chain.group.Item.Epoch = Epoch
	chain.group.Item.LastUpdate = time.Now().UnixNano()
	chain_log.Infof("<%s> Chain Info updated, latest Epoch %d", chain.group.Item.GroupId, Epoch)
	return nodectx.GetNodeCtx().GetChainStorage().UpdGroup(chain.group.Item)
}

/*
PSConn handler
*/
func (chain *Chain) HandlePackageMessage(pkg *quorumpb.Package) error {
	chain_log.Debugf("<%s> HandlePackageMessage called", chain.groupId)
	var err error
	if pkg.Type == quorumpb.PackageType_BLOCK {
		chain_log.Info("Handle BLOCK")
		blk := &quorumpb.Block{}
		err = proto.Unmarshal(pkg.Data, blk)
		if err != nil {
			chain_log.Warning(err.Error())
		} else {
			err = chain.HandleBlockPsConn(blk)
		}
	} else if pkg.Type == quorumpb.PackageType_TRX {
		chain_log.Info("Handle TRX")
		trx := &quorumpb.Trx{}
		err = proto.Unmarshal(pkg.Data, trx)
		if err != nil {
			chain_log.Warningf(err.Error())
		} else {
			err = chain.HandleTrxPsConn(trx)
		}
	} else if pkg.Type == quorumpb.PackageType_HBB {
		chain_log.Info("Handle HBB")
		hb := &quorumpb.HBMsgv1{}
		err = proto.Unmarshal(pkg.Data, hb)
		if err != nil {
			chain_log.Warningf(err.Error())
		} else {
			err = chain.HandleHBPsConn(hb)
		}
	} else if pkg.Type == quorumpb.PackageType_CONSENSUS {
		chain_log.Info("Handle CONSENSUS")
		cm := &quorumpb.ConsensusMsg{}
		err = proto.Unmarshal(pkg.Data, cm)
		if err != nil {
			chain_log.Warnf(err.Error())
		} else {
			err = chain.HandleConsesusPsConn(cm)
		}
	}

	return err
}

// Handle Trx from PsConn
func (chain *Chain) HandleTrxPsConn(trx *quorumpb.Trx) error {
	chain_log.Debugf("<%s> HandleTrxPsConn called", chain.groupId)
	if trx.Version != nodectx.GetNodeCtx().Version {
		chain_log.Errorf("HandleTrxPsConn called, Trx Version mismatch %s: %s vs %s", trx.TrxId, trx.Version, nodectx.GetNodeCtx().Version)
		return fmt.Errorf("trx Version mismatch")
	}

	// decompress
	content := new(bytes.Buffer)
	if err := utils.Decompress(bytes.NewReader(trx.Data), content); err != nil {
		chain_log.Errorf("utils.Decompress failed: %s", err)
		return fmt.Errorf("utils.Decompress failed: %s", err)
	}
	trx.Data = content.Bytes()

	verified, err := rumchaindata.VerifyTrx(trx)
	if err != nil {
		chain_log.Warnf("<%s> verify Trx failed with err <%s>", chain.groupId, err.Error())
		return fmt.Errorf("verify Trx failed")
	}

	if !verified {
		chain_log.Warnf("<%s> Invalid Trx, signature verify failed, sender %s", chain.groupId, trx.SenderPubkey)
		return fmt.Errorf("invalid Trx")
	}

	switch trx.Type {
	case quorumpb.TrxType_POST,
		quorumpb.TrxType_ANNOUNCE,
		quorumpb.TrxType_PRODUCER,
		quorumpb.TrxType_USER,
		quorumpb.TrxType_SCHEMA,
		quorumpb.TrxType_APP_CONFIG,
		quorumpb.TrxType_CHAIN_CONFIG:
		chain.producerAddTrx(trx)
	default:
		chain_log.Warningf("<%s> unsupported msg type", chain.group.Item.GroupId)
		err := errors.New("unsupported msg type")
		return err
	}
	return nil
}

// handle BLOCK msg from PSconn
func (chain *Chain) HandleBlockPsConn(block *quorumpb.Block) error {
	bpk := block.BookkeepingPubkey

	//from registed producer
	if _, ok := chain.ProducerPool[bpk]; !ok {
		chain_log.Warningf("<%s> received block <%d> from unregisted producer <%s>, reject it", chain.group.Item.GroupId, block.Epoch, bpk)
		return nil
	} else {
		if nodectx.GetNodeCtx().NodeType == nodectx.PRODUCER_NODE {
			//I am a producer but not in promoted producer list
			if _, ok := chain.ProducerPool[chain.group.Item.UserSignPubkey]; !ok {
				chain_log.Debugf("<%s> unapproved producer add block", chain.groupId)
				err := chain.Consensus.Producer().AddBlock(block)
				if err != nil {
					chain_log.Debugf("<%s> producer add block error <%s>", chain.groupId, err.Error())
					if err.Error() == "PARENT_NOT_EXIST" {
						chain_log.Infof("<%s> TBD parent not exist, Epoch <%d>, should start sync from current epoch <%d>", chain.groupId, block.Epoch, chain.group.Item.Epoch)
					}
				}
			} else {
				chain_log.Debugf("<%s> approved producer ignore broadcast block", chain.groupId)
			}
		} else {
			err := chain.Consensus.User().AddBlock(block)
			if err != nil {
				chain_log.Debugf("<%s> user add block error <%s>", chain.groupId, err.Error())
				if err.Error() == "PARENT_NOT_EXIST" {
					chain_log.Infof("<%s> TBD parent not exist, Epoch <%s>, should start sync from current epoch <%d>", chain.groupId, block.Epoch, chain.group.Item.Epoch)
				}
			}
		}
	}

	return nil
}

func (chain *Chain) HandleHBPsConn(hb *quorumpb.HBMsgv1) error {
	if !chain.isProducer() {
		//not approved producer node should not handle hb msg
		return nil
	}

	if hb.PayloadType == quorumpb.HBMsgPayloadType_HB_TRX {
		if chain.Consensus.Producer() == nil {
			return nil
		}
		return chain.Consensus.Producer().HandleHBMsg(hb)
	} else if hb.PayloadType == quorumpb.HBMsgPayloadType_HB_PSYNC {
		if chain.Consensus.PSync() == nil {
			return nil
		}
		return chain.Consensus.PSync().HandleHBMsg(hb)
	}

	return fmt.Errorf("unknown hbmsg type %s", hb.PayloadType.String())
}

func (chain *Chain) HandleConsesusPsConn(c *quorumpb.ConsensusMsg) error {
	chain_log.Debugf("<%s> HandleConsesusPsConn called", chain.groupId)

	//non producer should not handle consensus msg
	if _, ok := chain.ProducerPool[chain.group.Item.UserSignPubkey]; !ok {
		return nil
	}

	if chain.Consensus.PSync() == nil {
		return nil
	}
	d := &quorumpb.ConsensusMsg{
		GroupId:      c.GroupId,
		SessionId:    c.SessionId,
		MsgType:      c.MsgType,
		Payload:      c.Payload,
		SenderPubkey: c.SenderPubkey,
		TimeStamp:    c.TimeStamp,
	}

	db, err := proto.Marshal(d)
	if err != nil {
		return err
	}

	dhash := localcrypto.Hash(db)
	if res := bytes.Compare(c.MsgHash, dhash); res != 0 {
		return fmt.Errorf("msg hash mismatch")
	}

	bytespubkey, err := base64.RawURLEncoding.DecodeString(c.SenderPubkey)
	if err != nil {
		return err
	}

	ethpbukey, err := ethcrypto.DecompressPubkey(bytespubkey)
	if err == nil {
		ks := localcrypto.GetKeystore()
		r := ks.EthVerifySign(c.MsgHash, c.SenderSign, ethpbukey)
		if !r {
			return fmt.Errorf("verify signature failed")
		} else {
			chain_log.Debugf("<%s> MsgSignature is good", chain.groupId)
		}
	} else {
		return err
	}

	if c.MsgType == quorumpb.ConsensusType_REQ {
		if _, ok := chain.ProducerPool[c.SenderPubkey]; !ok {
			chain_log.Debugf("consensusReq from non producer node, ignore")
			return nil
		}
		//let psync handle the req
		return chain.Consensus.PSync().AddConsensusReq(c)
	} else if c.MsgType == quorumpb.ConsensusType_RESP {
		//check if the resp is from myself
		if len(chain.ProducerPool) != 1 && chain.group.Item.UserSignPubkey == c.SenderPubkey {
			chain_log.Debugf("multiple producer exist, session <%s> consensusResp from myself, ignore", c.SessionId)
			return nil
		}

		//check if psync result with same session_id exist
		isExist, err := nodectx.GetNodeCtx().GetChainStorage().IsPSyncSessionExist(chain.groupId, c.SessionId)
		if err != nil {
			return err
		}

		if isExist {
			chain_log.Debugf("Session <%s> is handled, ignore", c.SessionId)
			return nil
		}

		//verify response
		resp := &quorumpb.ConsensusResp{}
		err = proto.Unmarshal(c.Payload, resp)
		if err != nil {
			return err
		}

		ok, err := chain.verifyProducer(c.SenderPubkey, resp)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("invalid consensusResp from producer %s", c.SenderPubkey)
		}

		return chain.handlePSyncResp(c.SessionId, resp)
	} else {
		return fmt.Errorf("unknown msgType %s", c.MsgType)
	}
}

/*
all sync related rules go here
*/
func (chain *Chain) handlePSyncResp(sessionId string, resp *quorumpb.ConsensusResp) error {
	chain_log.Debugf("<%s> handlePSyncResp called, SessionId <%s>", chain.groupId, sessionId)

	//check if the resp is from myself
	//check if the resp is what gsyncer expected
	taskId, taskType, _, err := chain.syncerrunner.GetCurrentSyncTask()
	if err == rumerrors.ErrNoTaskWait || taskType != ConsensusSync || taskId != sessionId {
		//not the expected consensus resp
		return rumerrors.ErrConsusMismatch
	}

	savedResp, err := nodectx.GetNodeCtx().GetChainStorage().GetCurrentPSyncSession(chain.groupId)
	if err != nil {
		return err
	}

	//just in case
	if len(savedResp) > 1 {
		return fmt.Errorf("get more than 1 saved psync resp msg, something goes wrong")
	}

	if len(savedResp) == 1 {
		respItem := savedResp[0]
		if respItem.CurChainEpoch > resp.CurChainEpoch {
			chain_log.Debugf("resp from old epoch, do nothing, ignore")
			return fmt.Errorf("resp from old epoch, ignore")
		}
	}

	//save ConsensusResp
	nodectx.GetNodeCtx().GetChainStorage().UpdPSyncResp(chain.groupId, sessionId, resp)

	//check and update producer
	/*
		trx, _, err := nodectx.GetNodeCtx().GetChainStorage().GetTrx(chain.groupId, resp.ProducerProof.TrxId, sdef.Chain, chain.nodename)
		if err != nil && trx != nil {
			chain_log.Debugf("No need to upgrade producer list")
		} else {
			//TBD update producers list and regerate all consensus
			// user
			// producer
			// psync
		}
	*/
	if resp.CurChainEpoch == chain.group.Item.Epoch {
		chain_log.Debugf("node local epoch = current chain epoch, No need to sync")
		chain.syncerrunner.UpdateConsensusResult(sessionId, uint(SyncDone))
	} else {
		chain.syncerrunner.UpdateConsensusResult(sessionId, uint(ContinueGetEpoch))
	}

	return nil
}

func (chain *Chain) verifyProducer(senderPubkey string, resp *quorumpb.ConsensusResp) (bool, error) {
	//TBD verify signature

	if len(resp.CurProducer.Producers) == 1 && resp.CurProducer.Producers[0] == chain.group.Item.OwnerPubKey {
		return true, nil
	} else {

		//verify producer trx
		trxOK, err := rumchaindata.VerifyTrx(resp.ProducerProof)
		if err != nil {
			return false, err
		}

		if !trxOK {
			chain_log.Debugf("Invalid proof producer trx")
			return false, err
		}

		//need decode trx data by using ciperKey
		ciperKey, err := hex.DecodeString(chain.group.Item.CipherKey)
		if err != nil {
			return false, err
		}

		encryptdData, err := localcrypto.AesDecode(resp.ProducerProof.Data, ciperKey)
		if err != nil {
			return false, err
		}

		bftProducerBundleItem := &quorumpb.BFTProducerBundleItem{}
		err = proto.Unmarshal(encryptdData, bftProducerBundleItem)
		if err != nil {
			return false, err
		}

		/*
			update producer trx does NOT include owner itself
		*/
		if senderPubkey == chain.group.Item.OwnerPubKey {
			chain_log.Debugf(("it's my dear load..."))
			return true, nil
		}

		//sender(producer) Id should in the update producer trx list
		for _, producer := range bftProducerBundleItem.Producers {
			if producer.ProducerPubkey == senderPubkey {
				chain_log.Debugf("consensus sender <%s> is valid producer", senderPubkey)
				return true, nil
			}
		}

		//no, not a producer
		return false, nil
	}
}

func (chain *Chain) HandleConsesusRex(c *quorumpb.ConsensusMsg) error {
	return nil
}

/*
Rex Handler
*/
func (chain *Chain) HandleTrxRex(trx *quorumpb.Trx, s network.Stream) error {
	chain_log.Debugf("<%s> HandleTrxRex called", chain.groupId)
	if trx.Version != nodectx.GetNodeCtx().Version {
		chain_log.Errorf("HandleTrxRex called, Trx Version mismatch %s: %s vs %s", trx.TrxId, trx.Version, nodectx.GetNodeCtx().Version)
		return fmt.Errorf("trx Version mismatch")
	}

	// decompress
	content := new(bytes.Buffer)
	if err := utils.Decompress(bytes.NewReader(trx.Data), content); err != nil {
		e := fmt.Errorf("utils.Decompress failed: %s", err)
		chain_log.Error(e)
		return e
	}
	trx.Data = content.Bytes()

	verified, err := rumchaindata.VerifyTrx(trx)
	if err != nil {
		chain_log.Warnf("<%s> verify Trx failed with err <%s>", chain.groupId, err.Error())
		return fmt.Errorf("verify Trx failed")
	}

	if !verified {
		chain_log.Warnf("<%s> Invalid Trx, signature verify failed, sender %s", chain.groupId, trx.SenderPubkey)
		return fmt.Errorf("invalid Trx")
	}

	//Rex Channel only support the following trx type
	switch trx.Type {
	case quorumpb.TrxType_REQ_BLOCK_FORWARD:
		if trx.SenderPubkey == chain.group.Item.UserSignPubkey {
			return nil
		}
		chain.handleReqBlocks(trx, s)
	case quorumpb.TrxType_REQ_BLOCK_RESP:
		if trx.SenderPubkey == chain.group.Item.UserSignPubkey {
			return nil
		}
		chain.HandleReqBlockResp(trx)
	default:
		//do nothing
	}

	return nil
}

func (chain *Chain) HandleBlockRex(block *quorumpb.Block, s network.Stream) error {
	chain_log.Debugf("<%s> HandleBlockRex called", chain.groupId)
	return nil
}

func (chain *Chain) HandleHBRex(hb *quorumpb.HBMsgv1) error {
	chain_log.Debugf("<%s> HandleBlockRex called", chain.groupId)
	return nil
}

func (chain *Chain) producerAddTrx(trx *quorumpb.Trx) error {
	if chain.Consensus != nil && chain.Consensus.Producer() == nil || !chain.isProducer() {
		return nil
	}

	chain_log.Debugf("<%s> producerAddTrx called", chain.groupId)
	chain.Consensus.Producer().AddTrx(trx)
	return nil
}

func (chain *Chain) handleReqBlocks(trx *quorumpb.Trx, s network.Stream) error {
	chain_log.Debugf("<%s> handleReqBlockForward called", chain.groupId)

	chain_log.Debugf("<%s> producer handleReqBlockForward called", chain.groupId)
	requester, fromEpoch, blkReqs, blocks, result, err := chain.chaindata.GetReqBlocks(trx)
	if err != nil {
		return err
	}

	chain_log.Debugf("<%s> send REQ_NEXT_BLOCK_RESP (BLOCK_NOT_FOUND)", chain.groupId)
	if chain.Consensus == nil || chain.Consensus.Producer() == nil {
		chain_log.Debugf("<%s> I'm not a producer, skip BLOCK_NOT_FOUND", chain.groupId)
		return nil
	}

	chain_log.Debugf("<%s> send REQ_BLOCKS_RESP", chain.groupId)
	chain_log.Debugf("-- requester <%s>, fromEpoch <%d>, request <%d>", requester, fromEpoch, blkReqs)
	chain_log.Debugf("-- send fromEpoch <%d>, total <%d> blocks, status <%s>", fromEpoch, len(blocks), result.String())

	trx, err = chain.trxFactory.GetReqBlocksRespTrx("", chain.groupId, requester, blkReqs, fromEpoch, blocks, result)
	if err != nil {
		return err
	}

	if cmgr, err := conn.GetConn().GetConnMgr(chain.groupId); err != nil {
		return err
	} else {
		return cmgr.SendRespTrxRex(trx, s)
	}
}

func (chain *Chain) HandleReqBlockResp(trx *quorumpb.Trx) { //taskId,error
	chain_log.Debugf("<%s> HandleReqBlockResp called", chain.groupId)

	var err error
	ciperKey, err := hex.DecodeString(chain.group.Item.CipherKey)
	if err != nil {
		chain_log.Warningf("<%s> HandleReqBlockResp error <%s>", chain.groupId, err.Error())
		return
	}

	decryptData, err := localcrypto.AesDecode(trx.Data, ciperKey)
	if err != nil {
		chain_log.Warningf("<%s> HandleReqBlockResp error <%s>", chain.groupId, err.Error())
		return
	}

	reqBlockResp := &quorumpb.ReqBlockResp{}
	if err := proto.Unmarshal(decryptData, reqBlockResp); err != nil {
		chain_log.Warningf("<%s> HandleReqBlockResp error <%s>", chain.groupId, err.Error())
		return
	}

	//check trx sender
	if trx.SenderPubkey != reqBlockResp.ProviderPubkey {
		chain_log.Debugf("<%s> HandleReqBlockResp - Trx Sender/blocks providers mismatch <%s>", chain.groupId)
		return
	}

	//if not asked by me, ignore it
	if reqBlockResp.RequesterPubkey != chain.group.Item.UserSignPubkey {
		chain_log.Debugf("<%s> HandleReqBlockResp error <%s>", chain.groupId, rumerrors.ErrSenderMismatch.Error())
		return
	}

	gsyncerTaskId, gsyncerTaskType, _, err := chain.syncerrunner.GetCurrentSyncTask()
	if err == rumerrors.ErrNoTaskWait {
		chain_log.Debugf("<%s> HandleReqBlockResp - no task waiting", chain.groupId)
		return
	}

	if gsyncerTaskType != GetEpoch {
		chain_log.Debugf("<%s> HandleReqBlockResp error <%s>", chain.groupId, rumerrors.ErrSyncerStatus.Error())
		return
	}

	//get epoch by using taskId
	epochWaiting, err := strconv.ParseInt(gsyncerTaskId, 10, 64)
	if err != nil {
		chain_log.Warningf("<%s> HandleReqBlockResp error <%s>", chain.groupId, err.Error())
		return
	}

	//check if the epoch is what we are waiting for
	if reqBlockResp.FromEpoch != epochWaiting {
		chain_log.Warningf("<%s> HandleReqBlockResp error <%s>", chain.groupId, rumerrors.ErrEpochMismatch)
		return
	}

	// check where the response is from
	// * if from non producer node and response is BLOCK_NOT_FOUND, ignore
	if !chain.isProducerByPubkey(reqBlockResp.ProviderPubkey) && reqBlockResp.Result == quorumpb.ReqBlkResult_BLOCK_NOT_FOUND {
		chain_log.Warningf("<%s> HandleReqBlockResp - receive BLOCK_NOT_FOUND from non producer node <%s>, ignore", chain.groupId, reqBlockResp.ProviderPubkey)
		return
	}

	chain_log.Debugf("- Receive valid reqBlockResp, provider <%s> result <%s> from epoch <%d> total blocks provided <%d>",
		reqBlockResp.ProviderPubkey,
		reqBlockResp.Result.String(),
		reqBlockResp.FromEpoch,
		len(reqBlockResp.Blocks.Blocks))

	isFromProducer := chain.isProducerByPubkey(reqBlockResp.ProviderPubkey)

	switch reqBlockResp.Result {
	case quorumpb.ReqBlkResult_BLOCK_NOT_FOUND:
		//user node say BLOCK_NOT_FOUND, ignore
		if !isFromProducer {
			chain_log.Debugf("<%s> HandleReqBlockResp - receive BLOCK_NOT_FOUND from user node <%s>, ignore", chain.groupId, reqBlockResp.ProviderPubkey)
			return
		}

		//TBD, stop only when received BLOCK_NOT_FOUND from F + 1 producers, otherwise continue sync
		chain_log.Debugf("<%s> HandleReqBlockResp - receive BLOCK_NOT_FOUND from producer node <%s>, process it", chain.groupId, reqBlockResp.ProviderPubkey)
		taskId := strconv.Itoa(int(reqBlockResp.FromEpoch))
		chain.syncerrunner.UpdateGetEpochResult(taskId, uint(SyncDone))
		return

	case quorumpb.ReqBlkResult_BLOCK_IN_RESP_ON_TOP:
		chain.applyBlocks(reqBlockResp.Blocks.Blocks)
		if !isFromProducer {
			chain_log.Debugf("<%s> HandleReqBlockResp - receive BLOCK_IN_RESP_ON_TOP from user node <%s>, apply all blocks and  ignore ON_TOP", chain.groupId, reqBlockResp.ProviderPubkey)
			return
		}

		chain_log.Debugf("<%s> HandleReqBlockResp - receive BLOCK_IN_RESP_ON_TOP from producer node <%s>, process it", chain.groupId, reqBlockResp.ProviderPubkey)
		//ignore on_top msg, run another round of sync, till get F + 1 BLOCK_NOT_FOUND from producers
		chain.syncerrunner.UpdateGetEpochResult(gsyncerTaskId, uint(ContinueGetEpoch))
		return
	case quorumpb.ReqBlkResult_BLOCK_IN_RESP:
		chain_log.Debugf("<%s> HandleReqBlockResp - receive BLOCK_IN_RESP from node <%s>, apply all blocks", chain.groupId, reqBlockResp.ProviderPubkey)
		chain.applyBlocks(reqBlockResp.Blocks.Blocks)
		chain.syncerrunner.UpdateGetEpochResult(gsyncerTaskId, uint(ContinueGetEpoch))
		break
	}
}

func (chain *Chain) applyBlocks(blocks []*quorumpb.Block) error {
	//handle synced block
	var err error
	for _, block := range blocks {
		if nodectx.GetNodeCtx().NodeType == nodectx.PRODUCER_NODE {
			err = chain.Consensus.Producer().AddBlock(block)
			if err != nil {
				chain_log.Warningf("<%s> HandleReqBlockResp error <%s>", chain.groupId, err.Error())
				return err
			}
		} else {
			err = chain.Consensus.User().AddBlock(block)
			if err != nil {
				chain_log.Warningf("<%s> HandleReqBlockResp error <%s>", chain.groupId, err.Error())
				return err
			}
		}
	}
	return nil
}

func (chain *Chain) UpdProducerList() {
	chain_log.Debugf("<%s> UpdProducerList called", chain.groupId)
	//create and load group producer pool
	chain.ProducerPool = make(map[string]*quorumpb.ProducerItem)
	producers, err := nodectx.GetNodeCtx().GetChainStorage().GetProducers(chain.group.Item.GroupId, chain.nodename)

	if err != nil {
		chain_log.Infof("Get producer failed with err %s", err.Error())
	}

	for _, item := range producers {
		base64ethpkey, err := localcrypto.Libp2pPubkeyToEthBase64(item.ProducerPubkey)
		if err == nil {
			chain.ProducerPool[base64ethpkey] = item
		} else {
			chain.ProducerPool[item.ProducerPubkey] = item
		}
		ownerPrefix := "(producer)"
		if item.ProducerPubkey == chain.group.Item.OwnerPubKey {
			ownerPrefix = "(owner)"
		}
		chain_log.Infof("<%s> Load producer <%s%s>", chain.groupId, item.ProducerPubkey, ownerPrefix)
	}
}

func (chain *Chain) UpdConnMgrProducer() {
	connMgr, _ := conn.GetConn().GetConnMgr(chain.groupId)

	var producerspubkey []string
	for key, _ := range chain.ProducerPool {
		producerspubkey = append(producerspubkey, key)
	}

	connMgr.UpdProducers(producerspubkey)
}

func (chain *Chain) UpdAnnouncedProducerStatus() {
	chain_log.Debugf("<%s> UpdAnnouncedProducerStatus called", chain.groupId)
	//update announced producer result
	announcedProducers, _ := nodectx.GetNodeCtx().GetChainStorage().GetAnnounceProducersByGroup(chain.group.Item.GroupId, chain.nodename)
	for _, item := range announcedProducers {
		_, ok := chain.ProducerPool[item.SignPubkey]
		err := nodectx.GetNodeCtx().GetChainStorage().UpdateAnnounceResult(quorumpb.AnnounceType_AS_PRODUCER, chain.group.Item.GroupId, item.SignPubkey, ok, chain.nodename)
		if err != nil {
			chain_log.Warningf("<%s> UpdAnnounceResult failed with error <%s>", chain.groupId, err.Error())
		}
	}
}

func (chain *Chain) UpdProducerConfig() {
	chain_log.Debugf("<%s> UpdProducerConfig called", chain.groupId)
	if chain.Consensus == nil || chain.Consensus.Producer() == nil {
		return
	}

	//recreate producer BFT config
	chain.Consensus.Producer().RecreateBft()
}

func (chain *Chain) GetUserPool() map[string]*quorumpb.UserItem {
	return chain.userPool
}

func (chain *Chain) GetUsesEncryptPubKeys() ([]string, error) {
	keys := []string{}
	ks := nodectx.GetNodeCtx().Keystore
	mypubkey, err := ks.GetEncodedPubkey(chain.group.Item.GroupId, localcrypto.Encrypt)
	if err != nil {
		return nil, err
	}
	keys = append(keys, mypubkey)
	for _, usr := range chain.userPool {
		if usr.EncryptPubkey != mypubkey {
			keys = append(keys, usr.EncryptPubkey)
		}
	}

	return keys, nil
}

func (chain *Chain) UpdUserList() {
	chain_log.Debugf("<%s> UpdUserList called", chain.groupId)
	//create and load group user pool
	chain.userPool = make(map[string]*quorumpb.UserItem)
	users, _ := nodectx.GetNodeCtx().GetChainStorage().GetUsers(chain.group.Item.GroupId, chain.nodename)
	for _, item := range users {
		chain.userPool[item.UserPubkey] = item
		ownerPrefix := "(user)"
		if item.UserPubkey == chain.group.Item.OwnerPubKey {
			ownerPrefix = "(owner)"
		}
		chain_log.Infof("<%s> Load Users <%s_%s>", chain.groupId, item.UserPubkey, ownerPrefix)
	}

	//update announced User result
	announcedUsers, _ := nodectx.GetNodeCtx().GetChainStorage().GetAnnounceUsersByGroup(chain.group.Item.GroupId, chain.nodename)
	for _, item := range announcedUsers {
		_, ok := chain.userPool[item.SignPubkey]
		err := nodectx.GetNodeCtx().GetChainStorage().UpdateAnnounceResult(quorumpb.AnnounceType_AS_USER, chain.group.Item.GroupId, item.SignPubkey, ok, chain.nodename)
		if err != nil {
			chain_log.Warningf("<%s> UpdAnnounceResult failed with error <%s>", chain.groupId, err.Error())
		}
	}
}

func (chain *Chain) CreateConsensus() error {
	chain_log.Debugf("<%s> CreateConsensus called", chain.groupId)

	var user def.User
	var producer def.Producer
	var psync def.PSync

	var shouldCreateUser, shouldCreateProducer, shouldCreatePSyncer bool

	if nodectx.GetNodeCtx().NodeType == nodectx.PRODUCER_NODE {
		shouldCreateProducer = true
		shouldCreateUser = false
		shouldCreatePSyncer = true
	} else if nodectx.GetNodeCtx().NodeType == nodectx.FULL_NODE {
		//check if I am owner of the group
		if chain.group.Item.UserSignPubkey == chain.group.Item.OwnerPubKey {
			shouldCreateProducer = true
			shouldCreatePSyncer = true
		} else {
			shouldCreateProducer = false
			shouldCreatePSyncer = false
		}
		shouldCreateUser = true
	} else {
		return fmt.Errorf("unknow nodetype")
	}

	if shouldCreateProducer {
		chain_log.Infof("<%s> Create and initial molasses producer", chain.groupId)
		producer = &consensus.MolassesProducer{}
		producer.NewProducer(chain.group.Item, chain.group.ChainCtx.nodename, chain)
	}

	if shouldCreateUser {
		chain_log.Infof("<%s> Create and initial molasses user", chain.groupId)
		user = &consensus.MolassesUser{}
		user.NewUser(chain.group.Item, chain.group.ChainCtx.nodename, chain)
	}

	if shouldCreatePSyncer {
		chain_log.Infof("<%s> Create and initial molasses psyncer", chain.groupId)
		psync = &consensus.MolassesPSync{}
		psync.NewPSync(chain.group.Item, chain.nodename, chain)
	}

	chain_log.Infof("<%s> create new consensus", chain.groupId)
	chain.Consensus = consensus.NewMolasses(producer, user, psync)

	return nil
}

func (chain *Chain) TrxEnqueue(groupId string, trx *quorumpb.Trx) error {
	return TrxEnqueue(groupId, trx)
}

func (chain *Chain) StartSync() error {
	chain_log.Debugf("<%s> StartSync called.", chain.groupId)
	//TODO
	//chain.SyncLocalBlock()
	chain.syncerrunner.Start()
	return nil
}

func (chain *Chain) isProducer() bool {
	_, ok := chain.group.ChainCtx.ProducerPool[chain.group.Item.UserSignPubkey]
	return ok
}

func (chain *Chain) isProducerByPubkey(pubkey string) bool {
	_, ok := chain.group.ChainCtx.ProducerPool[pubkey]
	return ok
}

//TODO
//func (chain *Chain) SyncLocalBlock() error {
//	startFrom := chain.group.Item.HighestBlockId
//	for {
//		subblocks, err := nodectx.GetNodeCtx().GetChainStorage().GetSubBlock(chain.group.Item.HighestBlockId, chain.nodename)
//		if err != nil {
//			chain_log.Debugf("<%s> GetSubBlock failed <%s>", chain.groupId, err.Error())
//			return err
//		}
//		if len(subblocks) > 0 {
//			for _, block := range subblocks {
//				err := chain.AddLocalBlock(block)
//				if err != nil {
//					chain_log.Debugf("<%s> AddLocalBlock failed <%s>", chain.groupId, err.Error())
//					break // for range subblocks
//				}
//			}
//		} else {
//			chain_log.Debugf("<%s> No more local blocks", chain.groupId)
//			return nil
//		}
//		topBlock, err := nodectx.GetNodeCtx().GetChainStorage().GetBlock(chain.group.Item.HighestBlockId, false, chain.nodename)
//		if err != nil {
//			chain_log.Debugf("<%s> Get Top Block failed <%s>", chain.groupId, err.Error())
//			return err
//		} else {
//			if topBlock.BlockId == startFrom {
//				return nil
//			} else {
//				startFrom = topBlock.BlockId
//			}
//		}
//	}
//
//}

//TODO
//func (chain *Chain) AddLocalBlock(block *quorumpb.Block) error {
//	chain_log.Debugf("<%s> AddLocalBlock called", chain.groupId)
//	signpkey, err := localcrypto.Libp2pPubkeyToEthBase64(chain.group.Item.UserSignPubkey)
//	if err != nil && signpkey == "" {
//		chain_log.Warnf("<%s> Pubkey err <%s>", chain.groupId, err)
//	}
//
//	_, producer := chain.ProducerPool[signpkey]
//
//	if producer {
//		chain_log.Debugf("<%s> PRODUCER ADD LOCAL BLOCK <%d>", chain.groupId, block.Epoch)
//		err := chain.AddBlock(block)
//		if err != nil {
//			chain_log.Infof(err.Error())
//		}
//	} else {
//		chain_log.Debugf("<%s> USER ADD LOCAL BLOCK <%d>", chain.groupId, block.Epoch)
//		err := chain.Consensus.User().AddBlock(block)
//		if err != nil {
//			chain_log.Infof(err.Error())
//		}
//	}
//	return nil
//}

func (chain *Chain) StopSync() {
	chain_log.Debugf("<%s> StopSync called", chain.groupId)
	if chain.syncerrunner != nil {
		chain.syncerrunner.Stop()
	}
}

func (chain *Chain) GetSyncerStatus() int8 {
	return chain.syncerrunner.gsyncer.Status
}

func (chain *Chain) IsSyncerIdle() bool {
	chain_log.Debugf("IsSyncerIdle called, groupId <%s>", chain.groupId)
	if chain.syncerrunner.gsyncer.Status == SYNCING_FORWARD ||
		chain.syncerrunner.gsyncer.Status == LOCAL_SYNCING ||
		chain.syncerrunner.gsyncer.Status == CONSENSUS_SYNC ||
		chain.syncerrunner.gsyncer.Status == SYNC_FAILED {
		chain_log.Debugf("<%s> gsyncer is busy, status: <%d>", chain.groupId, chain.syncerrunner.gsyncer.Status)
		return true
	}
	chain_log.Debugf("<%s> syncer is IDLE", chain.groupId)
	return false
}

func (chain *Chain) GetNextNonce(groupId string, prefix ...string) (nonce uint64, err error) {
	nodeprefix := utils.GetPrefix(prefix...)
	n, err := nodectx.GetDbMgr().GetNextNonce(groupId, nodeprefix)
	return n, err
}

func (chain *Chain) ApplyTrxsFullNode(trxs []*quorumpb.Trx, nodename string) error {
	chain_log.Debugf("<%s> ApplyTrxsFullNode called", chain.groupId)
	for _, trx := range trxs {
		//check if trx already applied
		isExist, err := nodectx.GetNodeCtx().GetChainStorage().IsTrxExist(trx.GroupId, trx.TrxId, trx.Nonce, nodename)
		if err != nil {
			chain_log.Debugf("<%s> %s", chain.groupId, err.Error())
			continue
		}

		if isExist {
			chain_log.Debugf("<%s> trx <%s> existed, update trx only", chain.groupId, trx.TrxId)
			nodectx.GetNodeCtx().GetChainStorage().AddTrx(trx, nodename)
			continue
		}

		originalData := trx.Data

		//new trx, apply it
		if trx.Type == quorumpb.TrxType_POST && chain.group.Item.EncryptType == quorumpb.GroupEncryptType_PRIVATE {
			//for post, private group, encrypted by pgp for all announced group user
			ks := localcrypto.GetKeystore()
			decryptData, err := ks.Decrypt(chain.groupId, trx.Data)
			if err != nil {
				trx.Data = []byte("")
				//return err
			} else {
				//set trx.Data to decrypted []byte
				trx.Data = decryptData
			}

		} else {
			//decode trx data
			ciperKey, err := hex.DecodeString(chain.group.Item.CipherKey)
			if err != nil {
				return err
			}

			decryptData, err := localcrypto.AesDecode(trx.Data, ciperKey)
			if err != nil {
				return err
			}

			//set trx.Data to decrypted []byte
			trx.Data = decryptData
		}

		//apply trx
		chain_log.Debugf("<%s> try apply trx <%s>", chain.groupId, trx.TrxId)

		switch trx.Type {
		case quorumpb.TrxType_POST:
			chain_log.Debugf("<%s> apply POST trx", chain.groupId)
			nodectx.GetNodeCtx().GetChainStorage().AddPost(trx, nodename)
		case quorumpb.TrxType_PRODUCER:
			chain_log.Debugf("<%s> apply PRODUCER trx", chain.groupId)
			nodectx.GetNodeCtx().GetChainStorage().UpdateProducerTrx(trx, nodename)
			chain.UpdProducerList()
			chain.UpdAnnouncedProducerStatus()
			chain.UpdProducerConfig()
			//chain.UpdConnMgrProducer()
		case quorumpb.TrxType_USER:
			chain_log.Debugf("<%s> apply USER trx", chain.groupId)
			nodectx.GetNodeCtx().GetChainStorage().UpdateUserTrx(trx, nodename)
			chain.UpdUserList()
		case quorumpb.TrxType_ANNOUNCE:
			chain_log.Debugf("<%s> apply ANNOUNCE trx", chain.groupId)
			nodectx.GetNodeCtx().GetChainStorage().UpdateAnnounce(trx.Data, nodename)
		case quorumpb.TrxType_APP_CONFIG:
			chain_log.Debugf("<%s> apply APP_CONFIG trx", chain.groupId)
			nodectx.GetNodeCtx().GetChainStorage().UpdateAppConfigTrx(trx, nodename)
		case quorumpb.TrxType_CHAIN_CONFIG:
			chain_log.Debugf("<%s> apply CHAIN_CONFIG trx", chain.groupId)
			err := nodectx.GetNodeCtx().GetChainStorage().UpdateChainConfigTrx(trx, nodename)
			if err != nil {
				chain_log.Errorf("<%s> handle CHAIN_CONFIG trx", chain.groupId)
			}
		default:
			chain_log.Warningf("<%s> unsupported msgType <%s>", chain.groupId, trx.Type)
		}

		//set trx data to original(encrypted)
		trx.Data = originalData

		//save trx to db
		nodectx.GetNodeCtx().GetChainStorage().AddTrx(trx, nodename)
	}
	return nil
}

func (chain *Chain) ApplyTrxsProducerNode(trxs []*quorumpb.Trx, nodename string) error {
	chain_log.Debugf("<%s> ApplyTrxsProducerNode called", chain.groupId)
	for _, trx := range trxs {
		if trx.Type == quorumpb.TrxType_APP_CONFIG || trx.Type == quorumpb.TrxType_POST {
			//producer node does not handle APP_CONFIG and POST
			chain_log.Infof("Skip TRX %s with type %s", trx.TrxId, trx.Type.String())
			continue
		}

		//check if trx already applied
		isExist, err := nodectx.GetNodeCtx().GetChainStorage().IsTrxExist(trx.GroupId, trx.TrxId, trx.Nonce, nodename)
		if err != nil {
			chain_log.Debugf("<%s> %s", chain.groupId, err.Error())
			continue
		}

		if isExist {
			chain_log.Debugf("<%s> trx <%s> existed, update trx", chain.groupId, trx.TrxId)
			nodectx.GetNodeCtx().GetChainStorage().AddTrx(trx, nodename)
			continue
		}

		originalData := trx.Data
		//decode trx data
		ciperKey, err := hex.DecodeString(chain.group.Item.CipherKey)
		if err != nil {
			return err
		}

		decryptData, err := localcrypto.AesDecode(trx.Data, ciperKey)
		if err != nil {
			return err
		}

		//set trx.Data to decrypted []byte
		trx.Data = decryptData

		chain_log.Debugf("<%s> apply trx <%s>", chain.groupId, trx.TrxId)
		//apply trx content
		switch trx.Type {
		case quorumpb.TrxType_PRODUCER:
			chain_log.Debugf("<%s> apply PRODUCER trx", chain.groupId)
			nodectx.GetNodeCtx().GetChainStorage().UpdateProducerTrx(trx, nodename)
			chain.UpdProducerList()
			chain.UpdAnnouncedProducerStatus()
			chain.UpdProducerConfig()
			chain.UpdConnMgrProducer()
		case quorumpb.TrxType_USER:
			chain_log.Debugf("<%s> apply USER trx", chain.groupId)
			nodectx.GetNodeCtx().GetChainStorage().UpdateUserTrx(trx, nodename)
			chain.UpdUserList()
		case quorumpb.TrxType_ANNOUNCE:
			chain_log.Debugf("<%s> apply ANNOUNCE trx", chain.groupId)
			nodectx.GetNodeCtx().GetChainStorage().UpdateAnnounce(trx.Data, nodename)
		case quorumpb.TrxType_CHAIN_CONFIG:
			chain_log.Debugf("<%s> apply CHAIN_CONFIG trx", chain.groupId)
			err := nodectx.GetNodeCtx().GetChainStorage().UpdateChainConfigTrx(trx, nodename)
			if err != nil {
				chain_log.Errorf("<%s> handle CHAIN_CONFIG trx", chain.groupId)
			}
		default:
			chain_log.Warningf("<%s> unsupported msgType <%s>", chain.groupId, trx.Type)
		}

		trx.Data = originalData

		//save trx to db
		nodectx.GetNodeCtx().GetChainStorage().AddTrx(trx, nodename)
	}

	return nil
}
