package handlers

import (
	"github.com/go-playground/validator/v10"
	chain "github.com/rumsystem/quorum/internal/pkg/chainsdk/core"
)

type ClearGroupDataParam struct {
	GroupId string `from:"group_id" json:"group_id" validate:"required" example:"ac0eea7c-2f3c-4c67-80b3-136e46b924a8"`
}

type ClearGroupDataResult struct {
	GroupId string `json:"group_id" validate:"required" example:"ac0eea7c-2f3c-4c67-80b3-136e46b924a8"`
}

func ClearGroupData(params *ClearGroupDataParam) (*ClearGroupDataResult, error) {
	validate := validator.New()
	err := validate.Struct(params)
	if err != nil {
		return nil, err
	}

	groupmgr := chain.GetGroupMgr()
	group, ok := groupmgr.Groups[params.GroupId]
	if ok {
		// stop syncing first, to avoid starving in browser (indexeddb)
		if err := group.StopSync(); err != nil {
			return nil, err
		}
		// group may not exists or already be left
		if err := group.ClearGroup(); err != nil {
			return nil, err
		}
	}
	return &ClearGroupDataResult{GroupId: params.GroupId}, nil
}
