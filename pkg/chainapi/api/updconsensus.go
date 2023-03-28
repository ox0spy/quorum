package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// @Tags Management
// @Summary AddProducer
// @Description add a peer to the group producer list
// @Accept json
// @Produce json
// @Param data body handlers.GrpProducerParam true "GrpProducerParam"
// @Success 200 {object} handlers.GrpProducerResult
// @Router /api/v1/group/producer [post]
<<<<<<< HEAD:pkg/chainapi/api/updconsensus.go
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
=======
func (h *Handler) GroupProducer(c echo.Context) (err error) {
	return c.JSON(http.StatusMethodNotAllowed, "API UNDER CONSTRUCTION")
	/*
		cc := c.(*utils.CustomContext)
		params := new(handlers.GrpProducerParam)
		if err := cc.BindAndValidate(params); err != nil {
			return err
		}

		res, err := handlers.GroupProducer(h.ChainAPIdb, params)
		if err != nil {
			return rumerrors.NewBadRequestError(err)
		}
>>>>>>> consensus_2_main:pkg/chainapi/api/producer.go

		return c.JSON(http.StatusOK, res)
	*/
}
