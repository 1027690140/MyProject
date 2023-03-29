package handler

import (
	"service_discovery/errors"
	"service_discovery/model"

	"github.com/gin-gonic/gin"
)

func Set(c *gin.Context) {
	arg := new(model.ArgSet)
	if err := c.Bind(arg); err != nil {
		result(c, nil, errors.ParamsErr)
		return
	}
	// len of color,status,metadata must equal to len of hostname or be zero
	if (len(arg.Hostname) != len(arg.Status) && len(arg.Status) != 0) ||
		(len(arg.Hostname) != len(arg.Metadata) && len(arg.Metadata) != 0) {
		result(c, nil, errors.ParamsErr)
		return
	}
	result(c, nil, rg.Set(c, arg))
}
