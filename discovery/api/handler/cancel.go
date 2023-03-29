package handler

import (
	"log"
	"net/http"
	"service_discovery/configs"
	"service_discovery/global"
	"service_discovery/model"
	"service_discovery/pkg/errcode"

	"github.com/gin-gonic/gin"
)

func CancelHandler(c *gin.Context) {
	log.Println("request api/cancel...")
	var req model.ArgCancel
	if e := c.ShouldBindJSON(&req); e != nil {
		err := errcode.ParamError
		c.JSON(http.StatusOK, gin.H{
			"code":    err.Code(),
			"message": err.Error(),
		})
		return
	}
	instance, err := global.Discovery.Registry.Cancel(req.Env, req.AppID, req.Hostname, req.LatestTimestamp)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    err.Code(),
			"message": err.Error(),
		})
		return
	}
	//replication to other server
	if !req.Replication {
		global.Discovery.Nodes.Load().(*model.Nodes).Replicate(configs.Cancel, instance)
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    configs.StatusOK,
		"message": "",
	})
}
