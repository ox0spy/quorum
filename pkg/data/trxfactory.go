package data

import (
	"encoding/binary"
	"errors"

	quorumpb "github.com/rumsystem/quorum/pkg/pb"
	"google.golang.org/protobuf/proto"
)

type TrxFactory struct {
	nodename   string
	groupId    string
	groupItem  *quorumpb.GroupItem
	chainNonce ChainNonce
	version    string
}

type ChainNonce interface {
	GetNextNonce(groupId string, prefix ...string) (nonce uint64, err error)
}

func (factory *TrxFactory) Init(version string, groupItem *quorumpb.GroupItem, nodename string, chainnonce ChainNonce) {
	factory.groupItem = groupItem
	factory.groupId = groupItem.GroupId
	factory.nodename = nodename
	factory.chainNonce = chainnonce
	factory.version = version
}

func (factory *TrxFactory) CreateTrxByEthKey(msgType quorumpb.TrxType, data []byte, keyalias string, encryptto ...[]string) (*quorumpb.Trx, error) {
	nonce, err := factory.chainNonce.GetNextNonce(factory.groupItem.GroupId, factory.nodename)
	if err != nil {
		return nil, err
	}
	return CreateTrxByEthKey(factory.nodename, factory.version, factory.groupItem, msgType, int64(nonce), data, keyalias, encryptto...)
}

func (factory *TrxFactory) GetUpdAppConfigTrx(keyalias string, item *quorumpb.AppConfigItem) (*quorumpb.Trx, error) {
	encodedcontent, err := proto.Marshal(item)
	if err != nil {
		return nil, err
	}

	return factory.CreateTrxByEthKey(quorumpb.TrxType_APP_CONFIG, encodedcontent, keyalias)
}

func (factory *TrxFactory) GetChainConfigTrx(keyalias string, item *quorumpb.ChainConfigItem) (*quorumpb.Trx, error) {
	encodedcontent, err := proto.Marshal(item)
	if err != nil {
		return nil, err
	}

	return factory.CreateTrxByEthKey(quorumpb.TrxType_CHAIN_CONFIG, encodedcontent, keyalias)
}

/*
func (factory *TrxFactory) GetRegProducerBundleTrx(keyalias string, item *quorumpb.BFTProducerBundleItem) (*quorumpb.Trx, error) {
	encodedcontent, err := proto.Marshal(item)
	if err != nil {
		return nil, err
	}
	return factory.CreateTrxByEthKey(quorumpb.TrxType_PRODUCER, encodedcontent, keyalias)
}
*/

func (factory *TrxFactory) GetRegUserTrx(keyalias string, item *quorumpb.UserItem) (*quorumpb.Trx, error) {
	encodedcontent, err := proto.Marshal(item)
	if err != nil {
		return nil, err
	}
	return factory.CreateTrxByEthKey(quorumpb.TrxType_USER, encodedcontent, keyalias)
}

func (factory *TrxFactory) GetAnnounceTrx(keyalias string, item *quorumpb.AnnounceItem) (*quorumpb.Trx, error) {
	encodedcontent, err := proto.Marshal(item)
	if err != nil {
		return nil, err
	}

	return factory.CreateTrxByEthKey(quorumpb.TrxType_ANNOUNCE, encodedcontent, keyalias)
}

func (factory *TrxFactory) GetReqBlocksTrx(keyalias string, groupId string, fromBlock uint64, blkReq int32) (*quorumpb.Trx, error) {
	var reqBlockItem quorumpb.ReqBlock
	reqBlockItem.GroupId = groupId
	reqBlockItem.FromBlock = fromBlock
	reqBlockItem.BlksRequested = blkReq
	reqBlockItem.ReqPubkey = factory.groupItem.UserSignPubkey

	bItemBytes, err := proto.Marshal(&reqBlockItem)
	if err != nil {
		return nil, err
	}

	return CreateTrxByEthKey(factory.nodename, factory.version, factory.groupItem, quorumpb.TrxType_REQ_BLOCK, int64(0), bItemBytes, keyalias)
}

func (factory *TrxFactory) GetReqBlocksRespTrx(keyalias string, groupId string, requester string, fromBlock uint64, blkReq int32, blocks []*quorumpb.Block, result quorumpb.ReqBlkResult) (*quorumpb.Trx, error) {
	var reqBlockRespItem quorumpb.ReqBlockResp
	reqBlockRespItem.GroupId = groupId
	reqBlockRespItem.RequesterPubkey = requester
	reqBlockRespItem.ProviderPubkey = factory.groupItem.UserSignPubkey
	reqBlockRespItem.Result = result
	reqBlockRespItem.FromBlock = fromBlock
	reqBlockRespItem.BlksRequested = blkReq
	reqBlockRespItem.BlksProvided = int32(len(blocks))
	blockBundles := &quorumpb.BlocksBundle{}
	blockBundles.Blocks = blocks
	reqBlockRespItem.Blocks = blockBundles

	bItemBytes, err := proto.Marshal(&reqBlockRespItem)
	if err != nil {
		return nil, err
	}

	//send ask next block trx out
	return CreateTrxByEthKey(factory.nodename, factory.version, factory.groupItem, quorumpb.TrxType_REQ_BLOCK_RESP, int64(0), bItemBytes, keyalias)
}

func (factory *TrxFactory) GetPostAnyTrx(keyalias string, content []byte, encryptto ...[]string) (*quorumpb.Trx, error) {
	if binary.Size(content) > OBJECT_SIZE_LIMIT {
		err := errors.New("content size over 200Kb")
		return nil, err
	}

	return factory.CreateTrxByEthKey(quorumpb.TrxType_POST, content, keyalias, encryptto...)
}
