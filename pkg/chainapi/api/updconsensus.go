package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	rumerrors "github.com/rumsystem/quorum/internal/pkg/errors"
	"github.com/rumsystem/quorum/internal/pkg/utils"
	handlers "github.com/rumsystem/quorum/pkg/chainapi/handlers"
)

// @Tags Management
// @Summary AddProducer
// @Description add a peer to the group producer list
// @Accept json
// @Produce json
// @Param data body handlers.GrpProducerParam true "GrpProducerParam"
// @Success 200 {object} handlers.GrpProducerResult
// @Router /api/v1/group/producer [post]
func (h *Handler) UpdConsensus(c echo.Context) (err error) {
	cc := c.(*utils.CustomContext)
	params := new(handlers.UpdConsensusParam)
	if err := cc.BindAndValidate(params); err != nil {
		return err
	}

	res, err := handlers.UpdConsensus(h.ChainAPIdb, params)
	if err != nil {
		return rumerrors.NewBadRequestError(err)
	}

	return c.JSON(http.StatusOK, res)

}
