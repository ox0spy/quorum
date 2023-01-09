package api

import (
	"net/http"
	"sort"

	"encoding/base64"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/labstack/echo/v4"

	//localcrypto "github.com/rumsystem/quorum/pkg/crypto"
	chain "github.com/rumsystem/quorum/internal/pkg/chainsdk/core"
)

type groupInfo struct {
	GroupId        string        `json:"group_id" validate:"required,uuid4" example:"c0020941-e648-40c9-92dc-682645acd17e"`
	GroupName      string        `json:"group_name" validate:"required" example:"demo-app"`
	OwnerPubKey    string        `json:"owner_pubkey" validate:"required" example:"CAISIQLW2nWw+IhoJbTUmoq2ioT5plvvw/QmSeK2uBy090/3hg=="`
	UserPubkey     string        `json:"user_pubkey" validate:"required" example:"CAISIQO7ury6x7aWpwUVn6mj2dZFqme3BAY5xDkYjqW/EbFFcA=="`
	UserEthaddr    string        `json:"user_eth_addr" validate:"required" example:"0495180230ae0f585ca0b4fc0767e616eaed45e400f470ed50c91668e1ed76c278b7fc5a129ff154c6b200a26cc78b7b4acc5b3915cdf66286c942aa5b65166ff5"`
	ConsensusType  string        `json:"consensus_type" validate:"required" example:"POA"`
	EncryptionType string        `json:"encryption_type" validate:"required" example:"PUBLIC"`
	CipherKey      string        `json:"cipher_key" validate:"required" example:"58044622d48c4d91932583a05db3ff87f29acacb62e701916f7f0bbc6e446e5d"`
	AppKey         string        `json:"app_key" validate:"required" example:"test_app"`
	LastUpdated    int64         `json:"last_updated" validate:"required" example:"1633022375303983600"`
	HighestHeight  int64         `json:"highest_height" validate:"required" example:"0"`
	HighestBlockId string        `json:"highest_block_id" validate:"required,uuid4" example:"989ffea1-083e-46b0-be02-3bad3de7d2e1"`
	GroupStatus    string        `json:"group_status" validate:"required" example:"IDLE"`
	SnapshotInfo   *snapshotInfo `json:"snapshot_info"`
}

type snapshotInfo struct {
	TimeStamp         int64  `example:"1649096392051580700"`
	HighestHeight     int64  `example:"0"`
	HighestBlockId    string `example:"5f3a1cb7-822a-4fb8-ae84-864c496ab8de"`
	Nonce             int64  `example:"19500"`
	SnapshotPackageId string `example:"9d381d97-1cf4-4d13-9f95-b816f01f4df5"`
	SenderPubkey      string `example:"CAISIQMuaL8Y5TxbaO0ult7BTjYYhgrteewJANQGt/CYANjxQA=="`
}

type GroupInfoList struct {
	GroupInfos []*groupInfo `json:"groups"`
}

func (s *GroupInfoList) Len() int { return len(s.GroupInfos) }
func (s *GroupInfoList) Swap(i, j int) {
	s.GroupInfos[i], s.GroupInfos[j] = s.GroupInfos[j], s.GroupInfos[i]
}

func (s *GroupInfoList) Less(i, j int) bool {
	return s.GroupInfos[i].GroupName < s.GroupInfos[j].GroupName
}

// @Tags Groups
// @Summary GetGroups
// @Description Get all joined groups
// @Produce json
// @Success 200 {object} GroupInfoList
// @Router /api/v1/groups [get]
func (h *Handler) GetGroups(c echo.Context) (err error) {
	var groups []*groupInfo
	groupmgr := chain.GetGroupMgr()
	for _, value := range groupmgr.Groups {
		var group *groupInfo
		group = &groupInfo{}

		group.OwnerPubKey = value.Item.OwnerPubKey
		group.GroupId = value.Item.GroupId
		group.GroupName = value.Item.GroupName
		group.OwnerPubKey = value.Item.OwnerPubKey
		group.UserPubkey = value.Item.UserSignPubkey
		group.ConsensusType = value.Item.ConsenseType.String()
		group.EncryptionType = value.Item.EncryptType.String()
		group.CipherKey = value.Item.CipherKey
		group.AppKey = value.Item.AppKey
		group.LastUpdated = value.Item.LastUpdate
		group.HighestHeight = value.Item.HighestHeight
		group.HighestBlockId = value.Item.HighestBlockId

		b, err := base64.RawURLEncoding.DecodeString(group.UserPubkey)
		if err != nil {
			//try libp2pkey
		} else {
			ethpubkey, err := ethcrypto.DecompressPubkey(b)
			//ethpubkey, err := ethcrypto.UnmarshalPubkey(b)
			if err == nil {
				ethaddr := ethcrypto.PubkeyToAddress(*ethpubkey)
				group.UserEthaddr = ethaddr.Hex()
			}
		}

		switch value.GetSyncerStatus() {
		case chain.SYNCING_BACKWARD:
			group.GroupStatus = "SYNCING"
		case chain.SYNCING_FORWARD:
			group.GroupStatus = "SYNCING"
		case chain.SYNC_FAILED:
			group.GroupStatus = "SYNC_FAILED"
		case chain.IDLE:
			group.GroupStatus = "IDLE"
		}

		snapshottag, err := value.GetSnapshotInfo()
		if err != nil {
			group.SnapshotInfo = nil
		} else {
			snapshot := &snapshotInfo{}
			snapshot.TimeStamp = snapshottag.TimeStamp
			snapshot.HighestBlockId = snapshottag.HighestBlockId
			snapshot.HighestHeight = snapshottag.HighestHeight
			snapshot.Nonce = snapshottag.Nonce
			snapshot.SenderPubkey = snapshottag.SenderPubkey
			snapshot.SnapshotPackageId = snapshottag.SnapshotPackageId
			group.SnapshotInfo = snapshot
		}

		groups = append(groups, group)
	}

	ret := GroupInfoList{groups}
	sort.Sort(&ret)
	return c.JSON(http.StatusOK, &ret)
}
