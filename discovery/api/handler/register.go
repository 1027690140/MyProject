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

func RegisterHandler(c *gin.Context) {
	log.Println("request api/register...")
	var req model.ArgRequest
	if e := c.ShouldBindJSON(&req); e != nil {
		log.Println("error:", e)
		err := errcode.ParamError
		c.JSON(http.StatusOK, gin.H{
			"code":    err.Code(),
			"message": err.Error(),
		})
		return
	}
	//bind instance
	instance := model.NewInstance(&req)
	if instance.Status != configs.StatusReceive && instance.Status != configs.StatusNotReceive {
		log.Println("register params status invalid")
		err := errcode.ParamError
		c.JSON(http.StatusOK, gin.H{
			"code":    err.Code(),
			"message": err.Error(),
		})
		return
	}
	//dirtytime
	if req.DirtyTimestamp > 0 {
		instance.DirtyTimestamp = req.DirtyTimestamp
	}
	global.Discovery.Registry.Register(instance, req.LatestTimestamp)

	//default do replicate. if request come from other server, req.Replication is true, ignore replicate.
	if !req.Replication {
		global.Discovery.Nodes.Load().(*model.Nodes).Replicate(configs.Register, instance)
	}
	c.JSON(http.StatusOK, gin.H{
		"code":    configs.StatusOK,
		"message": "",
		"data":    "",
	})
}
