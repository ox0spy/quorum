package nodesdkapi

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
	nodesdkctx "github.com/rumsystem/quorum/pkg/nodesdk/nodesdkctx"
)

func (h *NodeSDKHandler) GetAppConfigKey(c echo.Context) (err error) {
	output := make(map[string]string)
	groupid := c.Param("group_id")

	if groupid == "" {
		output[ERROR_INFO] = "group_id can not be empty"
		return c.JSON(http.StatusBadRequest, output)
	}

	dbMgr := nodesdkctx.GetDbMgr()
	nodesdkGroupItem, err := dbMgr.GetGroupInfo(groupid)
	if err != nil {
		output[ERROR_INFO] = err.Error()
		return c.JSON(http.StatusBadRequest, output)
	}

	reqItem := new(AppConfigKeyListItem)
	reqItem.GroupId = groupid
	reqItem.JwtToken = JwtToken

	itemBytes, err := json.Marshal(reqItem)
	if err != nil {
		output[ERROR_INFO] = err.Error()
		return c.JSON(http.StatusBadRequest, output)
	}

	encryptData, err := getEncryptData(itemBytes, nodesdkGroupItem.Group.CipherKey)
	if err != nil {
		output[ERROR_INFO] = err.Error()
		return c.JSON(http.StatusBadRequest, output)
	}

	getItem := new(NodeSDKGetChainDataItem)
	getItem.GroupId = groupid
	getItem.Req = encryptData
	getItem.ReqType = APPCONFIG_KEYLIST

	reqBytes, err := json.Marshal(getItem)
	if err != nil {
		output[ERROR_INFO] = err.Error()
		return c.JSON(http.StatusBadRequest, output)
	}
	//just get the first one
	httpClient, err := nodesdkctx.GetCtx().GetHttpClient(nodesdkGroupItem.Group.GroupId)
	if err != nil {
		output[ERROR_INFO] = err.Error()
		return c.JSON(http.StatusBadRequest, output)
	}

	err = httpClient.UpdApiServer(nodesdkGroupItem.ApiUrl)
	if err != nil {
		output[ERROR_INFO] = err.Error()
		return c.JSON(http.StatusBadRequest, output)
	}

	resultInBytes, err := httpClient.Post(GET_CHAIN_DATA_URI, reqBytes)
	if err != nil {
		output[ERROR_INFO] = err.Error()
		return c.JSON(http.StatusBadRequest, output)
	}

	result := new([]*AppConfigKeyListItem)
	err = json.Unmarshal(resultInBytes, result)
	if err != nil {
		output[ERROR_INFO] = err.Error()
		return c.JSON(http.StatusBadRequest, output)
	}

	return c.JSON(http.StatusOK, result)
}