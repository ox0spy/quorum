package appapi

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	chain "github.com/rumsystem/quorum/internal/pkg/chainsdk/core"
	rumerrors "github.com/rumsystem/quorum/internal/pkg/errors"
	"github.com/rumsystem/quorum/internal/pkg/storage/def"
	localcrypto "github.com/rumsystem/quorum/pkg/crypto"
	quorumpb "github.com/rumsystem/quorum/pkg/pb"
)

type ContentInnerStruct map[string]interface{}

type ContentStruct struct {
	TrxId     string             `json:"TrxId"`
	Publisher string             `json:"Publisher"`
	Content   ContentInnerStruct `json:"Content"`
	TypeUrl   string             `json:"TypeUrl"`
	TimeStamp int64              `json:"TimeStamp"`
}

type GroupContentObjectItem struct {
	TrxId     string `example:"da2aaf30-39a8-4fe4-a0a0-44ceb71ac013"`
	Publisher string `example:"CAISIQOlA37+ghb05D5ZAKExjsto/H7eeCmkagcZ+BY/pjSOKw=="`
	/* Content Example:
		{
	        "type": "Note",
	        "content": "simple note by aa",
	        "name": "A simple Node id1"
	    }
	*/
	Content   map[string]interface{} // json object
	TimeStamp int64                  `example:"1629748212762123400"`
}

type SenderList struct {
	Senders []string
}

// @Tags Apps
// @Summary GetGroupContents
// @Description Get contents in a group
// @Produce json
// @Param group_id path string  true "Group Id"
// @Param num query string false "the count of returns results"
// @Param reverse query boolean false "reverse = true will return results by most recently"
// @Param starttrx query string false "returns results from this trxid, but exclude it"
// @Param includestarttrx query string false "include the start trx"
// @Param data body SenderList true "SenderList"
// @Success 200 {array} GroupContentObjectItem
// @Router /app/api/v1/group/{group_id}/content [get]
func (h *Handler) ContentByPeers(c echo.Context) (err error) {
	groupid := c.Param("group_id")
	num, _ := strconv.Atoi(c.QueryParam("num"))
	starttrx := c.QueryParam("starttrx")
	if num == 0 {
		num = 20
	}
	reverse := false
	if c.QueryParam("reverse") == "true" {
		reverse = true
	}
	includestarttrx := false
	if c.QueryParam("includestarttrx") == "true" {
		includestarttrx = true
	}
	senderlist := &SenderList{}
	if err = c.Bind(&senderlist); err != nil {
		return rumerrors.NewBadRequestError(err)
	}

	trxids, err := h.Appdb.GetGroupContentBySenders(groupid, senderlist.Senders, starttrx, num, reverse, includestarttrx)
	if err != nil {
		return rumerrors.NewBadRequestError(err)
	}

	groupmgr := chain.GetGroupMgr()
	groupitem, err := groupmgr.GetGroupItem(groupid)
	if err != nil {
		return rumerrors.NewBadRequestError(err)
	}

	ctnobjList := []*GroupContentObjectItem{}
	for _, trxid := range trxids {
		trx, err := h.Trxdb.GetTrx(groupid, trxid, def.Chain, h.NodeName)
		if err != nil {
			logger.Errorf("GetTrx groupid: %s trxid: %s failed: %s", groupid, trxid, err)
			continue
		}
		if trx.TrxId == "" && len(trx.Data) == 0 {
			logger.Warnf("GetTrx groupid: %s trxid: %s return empty trx, skip ...", groupid, trxid)
			continue
		}

		//decrypt trx data
		if trx.Type == quorumpb.TrxType_POST && groupitem.EncryptType == quorumpb.GroupEncryptType_PRIVATE {
			//for post, private group, encrypted by age for all announced group user
			ks := localcrypto.GetKeystore()
			decryptData, err := ks.Decrypt(groupid, trx.Data)
			if err != nil {
				//can't decrypt, replace it
				trx.Data = nil
				logger.Warnf("can not decrypt trx.Data for groupid: %s trxid: %s failed: %s", groupid, trxid, err)
			} else {
				//set trx.Data to decrypted []byte
				trx.Data = decryptData
			}
		} else {
			//decode trx data
			ciperKey, err := hex.DecodeString(groupitem.CipherKey)
			if err != nil {
				return err
			}

			decryptData, err := localcrypto.AesDecode(trx.Data, ciperKey)
			if err != nil {
				return err
			}
			trx.Data = decryptData
		}

		var content map[string]interface{} = nil
		if len(trx.Data) > 0 {
			if err := json.Unmarshal(trx.Data, &content); err != nil {
				logger.Errorf("groupid: %s trxid: %s json.Unmarshal trx.Data: %q failed: %s", groupid, trxid, trx.Data, err)
				return err
			}
		} else {
			logger.Errorf("groupid: %s trxid: %s trx.Data is empty", groupid, trxid)
		}

		pk, _ := localcrypto.Libp2pPubkeyToEthBase64(trx.SenderPubkey)
		ctnobjitem := &GroupContentObjectItem{
			TrxId:     trx.TrxId,
			Publisher: pk,
			Content:   content,
			TimeStamp: trx.TimeStamp,
		}
		ctnobjList = append(ctnobjList, ctnobjitem)
	}
	return c.JSON(http.StatusOK, ctnobjList)
}
